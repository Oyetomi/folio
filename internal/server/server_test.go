package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Oyetomi/folio/internal/build"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := New(context.Background(), t.TempDir(), "test")
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestGuardRejectsForeignHost(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/env", nil)
	req.Host = "evil.example:7350" // DNS-rebinding attempt
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host: got %d, want 403", res.StatusCode)
	}
}

func TestGuardRejectsCrossOriginPost(t *testing.T) {
	ts := newTestServer(t)
	for _, hdr := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/builds", strings.NewReader(`{}`))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%v: got %d, want 403", hdr, res.StatusCode)
		}
	}
}

func TestServesUI(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(res.Body)
	res.Body.Close()
	assets := regexp.MustCompile(`/assets/[^"]+\.(?:js|css)`).FindAllString(string(html), -1)
	if len(assets) < 2 {
		t.Fatalf("index.html references %d built assets; run `npm run build` in web/", len(assets))
	}
	for _, p := range append([]string{"/", "/favicon.svg", "/fonts/UbuntuMono-Regular.otf"}, assets...) {
		res, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %d", p, res.StatusCode)
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s: missing CSP", p)
		}
	}
}

func TestSaveFolderSanitizesPaths(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	paths := []string{"book/ch1.md", "book/../../escape.md", "/abs/evil.md", "book/img/a.png", "book/run.exe"}
	for _, p := range paths {
		_ = mw.WriteField("paths", p)
		fw, _ := mw.CreateFormFile("files", filepath.Base(p))
		fw.Write([]byte("# hi\n"))
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	root, label, err := saveFolder(dir, req.MultipartForm.Value["paths"], req.MultipartForm.File["files"])
	if err != nil {
		t.Fatal(err)
	}
	if label != "book" || root != filepath.Join(dir, "book") {
		t.Errorf("root=%s label=%s", root, label)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.md")); err == nil {
		t.Error("path traversal escaped the upload dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "book", "run.exe")); err == nil {
		t.Error("non-manuscript file was saved")
	}
	// "../../escape.md" and "/abs/evil.md" are confined under dir.
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !strings.HasPrefix(p, dir) {
			t.Errorf("file written outside upload dir: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestUploadAndBuild drives the whole desk: upload a folder, inspect it,
// build it, stream events to completion, and fetch the PDF.
func TestUploadAndBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles with tectonic")
	}
	if _, err := build.FindTectonic(); err != nil {
		t.Skip("tectonic not installed")
	}
	ts := newTestServer(t)

	fixture := filepath.Join("..", "latex", "testdata", "books", "structured")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("kind", "folder")
	err := filepath.Walk(fixture, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filepath.Dir(fixture), p)
		_ = mw.WriteField("paths", filepath.ToSlash(rel))
		fw, _ := mw.CreateFormFile("files", info.Name())
		data, _ := os.ReadFile(p)
		fw.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mw.Close()
	res, err := http.Post(ts.URL+"/api/sources", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	var info sourceInfo
	json.NewDecoder(res.Body).Decode(&info)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create source: %d", res.StatusCode)
	}
	if info.Title != "Fixture Book" || len(info.Chapters) != 4 {
		t.Fatalf("inspect: title=%q chapters=%d", info.Title, len(info.Chapters))
	}

	body, _ := json.Marshal(buildRequest{SourceID: info.ID, Title: "Desk Edition", Verify: true})
	res, err = http.Post(ts.URL+"/api/builds", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var job jobJSON
	json.NewDecoder(res.Body).Decode(&job)
	res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/builds/"+job.ID+"/events", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	stages := map[string]bool{}
	var final string
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var evType string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			evType = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			var d map[string]any
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d)
			switch evType {
			case "stage":
				stages[d["stage"].(string)] = true
			case "done", "failed", "cancelled":
				final = evType
				if evType == "failed" {
					t.Fatalf("build failed: %v", d["message"])
				}
			}
		}
	}
	if final != "done" {
		t.Fatalf("stream ended without done (final=%q)", final)
	}
	for _, st := range []string{"queued", "discover", "typeset", "compile", "verify"} {
		if !stages[st] {
			t.Errorf("missing stage %q", st)
		}
	}

	res, err = http.Get(ts.URL + "/api/builds/" + job.ID + "/pdf?download=1")
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("not a PDF (%d bytes)", len(pdf))
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, `Desk-Edition.pdf`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
}
