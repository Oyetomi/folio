// Package server implements `folio serve`: a local web UI ("the desk") for
// loading manuscripts, building PDFs with live progress, and previewing them.
//
// It is designed to run on the author's own machine only. Requests must use a
// loopback Host header (defeats DNS rebinding) and state-changing requests
// must be same-origin (defeats cross-site request forgery from other tabs).
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"crypto/rand"
	"encoding/hex"

	"github.com/Oyetomi/folio/internal/assets"
	"github.com/Oyetomi/folio/internal/build"
)

// The UI is a React app in /web. `npm run build` there writes dist/ here; it
// is committed so `go install` works without Node.
//
//go:generate sh -c "cd ../../web && npm ci && npm run build"
//go:embed all:dist
var distFS embed.FS

const (
	maxUploadBytes = 1 << 30 // 1 GiB per manuscript upload
	maxLogEvents   = 4000
	keepBuilds     = 24
)

// Config configures Run.
type Config struct {
	Addr        string
	OpenBrowser bool
	Version     string
}

// Run serves the desk until ctx is cancelled or the process is interrupted.
func Run(ctx context.Context, cfg Config) error {
	dataDir, err := os.MkdirTemp("", "folio-desk-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dataDir)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := New(ctx, dataDir, cfg.Version)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		// Default port busy (another desk open?) — fall back to any free port.
		host, _, _ := net.SplitHostPort(cfg.Addr)
		if host == "" {
			host = "127.0.0.1"
		}
		if ln, err = net.Listen("tcp", net.JoinHostPort(host, "0")); err != nil {
			return err
		}
	}
	addr := ln.Addr().(*net.TCPAddr)
	host := "localhost"
	if !addr.IP.IsLoopback() {
		host = addr.IP.String()
		fmt.Fprintln(os.Stderr, "warning: the desk is meant for localhost; requests with non-loopback Host headers are refused")
	}
	link := fmt.Sprintf("http://%s:%d/", host, addr.Port)
	fmt.Printf("Folio desk open at %s  (Ctrl+C to close)\n", link)
	if cfg.OpenBrowser {
		openBrowser(link)
	}

	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	fmt.Println("\nDesk closed.")
	return nil
}

// Server holds the desk's in-memory state. Everything lives under dataDir and
// disappears when the process exits.
type Server struct {
	ctx     context.Context
	dataDir string
	version string

	mu      sync.Mutex
	sources map[string]*source
	jobs    map[string]*job
	order   []string // job ids, oldest first

	press chan struct{} // one Tectonic run at a time
}

// New creates a Server storing uploads and builds under dataDir.
func New(ctx context.Context, dataDir, version string) *Server {
	return &Server{
		ctx:     ctx,
		dataDir: dataDir,
		version: version,
		sources: map[string]*source{},
		jobs:    map[string]*job{},
		press:   make(chan struct{}, 1),
	}
}

// Handler returns the HTTP handler with all routes and guards installed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	dist, _ := fs.Sub(distFS, "dist")
	mux.Handle("GET /{$}", serveFile(dist, "index.html"))
	mux.Handle("GET /assets/", cacheForever(http.FileServerFS(dist))) // content-hashed by Vite
	mux.Handle("GET /favicon.svg", http.FileServerFS(dist))
	mux.Handle("GET /fonts/", cacheForever(http.StripPrefix("/fonts/", http.FileServerFS(assets.Fonts()))))

	mux.HandleFunc("GET /api/env", s.handleEnv)
	mux.HandleFunc("POST /api/sources", s.handleCreateSource)
	mux.HandleFunc("GET /api/builds", s.handleListBuilds)
	mux.HandleFunc("POST /api/builds", s.handleCreateBuild)
	mux.HandleFunc("GET /api/builds/{id}", s.handleGetBuild)
	mux.HandleFunc("DELETE /api/builds/{id}", s.handleCancelBuild)
	mux.HandleFunc("GET /api/builds/{id}/events", s.handleEvents)
	mux.HandleFunc("GET /api/builds/{id}/pdf", s.handleArtifact("pdf"))
	mux.HandleFunc("GET /api/builds/{id}/tex", s.handleArtifact("tex"))
	return guard(mux)
}

// ---- guards ---------------------------------------------------------------

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "the Folio desk only answers on localhost", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-src 'self'; object-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func serveFile(fsys fs.FS, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, name)
	})
}

func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}

// ---- environment ------------------------------------------------------------

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	_, tecErr := build.FindTectonic()
	_, pdfErr := exec.LookPath("pdftotext")
	_, gitErr := exec.LookPath("git")
	_, hasPro := assets.FindProFonts("")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   s.version,
		"proFonts":  hasPro,
		"tectonic":  tecErr == nil,
		"pdftotext": pdfErr == nil,
		"git":       gitErr == nil,
	})
}

// ---- sources ------------------------------------------------------------------

type source struct {
	ID    string
	Kind  string // folder | epub | github | path
	Label string
	Root  string // directory or .epub handed to build.Build
	Dir   string // our storage for this source ("" for local paths)
	Info  sourceInfo
}

type chapterInfo struct {
	Title string `json:"title"`
	Part  int    `json:"part"`
}

type partInfo struct {
	Index int    `json:"index"`
	Title string `json:"title"`
}

type sourceInfo struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	Label    string        `json:"label"`
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle"`
	Author   string        `json:"author"`
	NoIndex  bool          `json:"noIndex"`
	Fonts    string        `json:"fonts"`
	Chapters []chapterInfo `json:"chapters"`
	Parts    []partInfo    `json:"parts"`
}

// Files worth uploading from a manuscript folder; everything else is skipped.
var manuscriptExt = map[string]bool{
	".md": true, ".mdx": true, ".markdown": true, ".ipynb": true, ".txt": true,
	".yml": true, ".yaml": true, ".toml": true, ".json": true,
	".png": true, ".jpg": true, ".jpeg": true, ".pdf": true, ".svg": true, ".gif": true,
	".rs": true, ".go": true, ".py": true, ".js": true, ".ts": true, ".c": true, ".h": true,
	".java": true, ".sh": true, ".sql": true, ".html": true, ".css": true,
}

var reGitHubRepo = regexp.MustCompile(`^https?://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?(?:/.*)?$`)

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "That manuscript is larger than 1 GB.")
			return
		}
		writeError(w, http.StatusBadRequest, "Could not read the upload: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()

	id := newID()
	src := &source{ID: id, Kind: r.FormValue("kind"), Dir: filepath.Join(s.dataDir, "sources", id)}
	if err := os.MkdirAll(src.Dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fail := func(code int, msg string) {
		os.RemoveAll(src.Dir)
		writeError(w, code, msg)
	}

	switch src.Kind {
	case "folder":
		root, label, err := saveFolder(src.Dir, r.MultipartForm.Value["paths"], r.MultipartForm.File["files"])
		if err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		src.Root, src.Label = root, label

	case "epub":
		fhs := r.MultipartForm.File["file"]
		if len(fhs) != 1 || !build.IsEPUB(fhs[0].Filename) {
			fail(http.StatusBadRequest, "Choose a single .epub file.")
			return
		}
		dst := filepath.Join(src.Dir, safeName(fhs[0].Filename, "book.epub"))
		if err := saveUpload(fhs[0], dst); err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		src.Root, src.Label = dst, fhs[0].Filename

	case "github":
		raw := strings.TrimSpace(r.FormValue("url"))
		if !strings.Contains(raw, "://") {
			raw = "https://" + strings.TrimPrefix(raw, "//")
		}
		repo, ok := build.RemoteURL(raw)
		m := reGitHubRepo.FindStringSubmatch(repo)
		if !ok || m == nil {
			fail(http.StatusBadRequest, "That doesn't look like a GitHub repository URL (github.com/owner/repo).")
			return
		}
		repo = fmt.Sprintf("https://github.com/%s/%s", m[1], m[2])
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		dst := filepath.Join(src.Dir, "repo")
		if err := build.Clone(ctx, repo, dst, nil); err != nil {
			fail(http.StatusBadGateway, "Could not clone "+repo+". Is it public?\n"+err.Error())
			return
		}
		src.Root, src.Label = dst, m[1]+"/"+m[2]

	case "path":
		p := strings.TrimSpace(r.FormValue("path"))
		if strings.HasPrefix(p, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, strings.TrimPrefix(p, "~"))
			}
		}
		if p == "" || !filepath.IsAbs(p) {
			fail(http.StatusBadRequest, "Enter an absolute path, like ~/books/my-novel or /Users/you/book.epub.")
			return
		}
		st, err := os.Stat(p)
		if err != nil || (!st.IsDir() && !build.IsEPUB(p)) {
			fail(http.StatusBadRequest, "Nothing usable at that path: it must be a folder or an .epub file.")
			return
		}
		src.Root, src.Label = filepath.Clean(p), p

	default:
		fail(http.StatusBadRequest, "Unknown source kind.")
		return
	}

	// Inspect: discover metadata and the table of contents without typesetting.
	inspectDir := filepath.Join(src.Dir, "_inspect")
	_ = os.MkdirAll(inspectDir, 0o755)
	ms, err := build.Load(src.Root, inspectDir)
	os.RemoveAll(inspectDir)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "No book found there: "+err.Error())
		return
	}
	info := sourceInfo{
		ID: id, Kind: src.Kind, Label: src.Label,
		Title: ms.Config.Title, Subtitle: ms.Config.Subtitle, Author: ms.Config.Author,
		NoIndex: ms.Config.NoIndex, Fonts: ms.Config.Fonts,
	}
	for _, d := range ms.Docs {
		info.Chapters = append(info.Chapters, chapterInfo{Title: d.Title, Part: d.PartIdx})
	}
	for _, p := range ms.Config.Parts {
		info.Parts = append(info.Parts, partInfo{Index: p.Index, Title: p.Title})
	}
	src.Info = info

	s.mu.Lock()
	s.sources[id] = src
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, info)
}

// saveFolder writes an uploaded directory tree. paths[i] is the browser's
// relative path for files[i]. Returns the manuscript root and a label.
func saveFolder(dir string, paths []string, files []*multipart.FileHeader) (string, string, error) {
	if len(files) == 0 || len(paths) != len(files) {
		return "", "", errors.New("The folder upload was empty or incomplete.")
	}
	var rels []string
	for i, fh := range files {
		raw := strings.ReplaceAll(paths[i], `\`, "/")
		if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, ":") || hasDotDot(raw) {
			continue // browsers never send these; drop rather than reinterpret
		}
		rel := path.Clean(raw)
		if !manuscriptExt[strings.ToLower(path.Ext(rel))] {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(dst, dir+string(filepath.Separator)) {
			continue
		}
		if err := saveUpload(fh, dst); err != nil {
			return "", "", err
		}
		rels = append(rels, rel)
	}
	if len(rels) == 0 {
		return "", "", errors.New("No Markdown, notebook, or image files were found in that folder.")
	}
	// Browsers prefix every path with the chosen folder's name; use that
	// folder as the root when everything lives under it.
	first := strings.SplitN(rels[0], "/", 2)[0]
	shared := strings.Contains(rels[0], "/")
	for _, r := range rels {
		if !strings.HasPrefix(r, first+"/") {
			shared = false
			break
		}
	}
	if shared {
		return filepath.Join(dir, first), first, nil
	}
	return dir, fmt.Sprintf("%d files", len(rels)), nil
}

func saveUpload(fh *multipart.FileHeader, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := fh.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var reUnsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func safeName(name, fallback string) string {
	n := strings.TrimSpace(reUnsafeName.ReplaceAllString(filepath.Base(name), "_"))
	n = strings.TrimLeft(n, ".")
	if n == "" {
		return fallback
	}
	return n
}

// ---- builds -------------------------------------------------------------------

type event struct {
	Type string
	Data any
}

type job struct {
	ID       string
	SourceID string
	Label    string
	Title    string
	Author   string
	Created  time.Time
	Dir      string

	mu        sync.Mutex
	state     string // queued | running | done | failed | cancelled
	stage     string
	events    []event
	truncated bool
	notify    chan struct{}
	result    *build.Result
	errMsg    string
	cancel    context.CancelFunc
}

func (j *job) emit(typ string, data any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if typ == "log" {
		if len(j.events) >= maxLogEvents {
			if !j.truncated {
				j.truncated = true
				j.events = append(j.events, event{"log", map[string]string{"line": "… (log truncated)"}})
			} else {
				return
			}
		}
	}
	j.events = append(j.events, event{typ, data})
	close(j.notify)
	j.notify = make(chan struct{})
}

func (j *job) setState(state string) {
	j.mu.Lock()
	j.state = state
	j.mu.Unlock()
}

func (j *job) finished() bool {
	switch j.state {
	case "done", "failed", "cancelled":
		return true
	}
	return false
}

type buildRequest struct {
	SourceID string `json:"sourceId"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Author   string `json:"author"`
	NoIndex  bool   `json:"noIndex"`
	Verify   bool   `json:"verify"`
	Fonts    string `json:"fonts"`
}

func (s *Server) handleCreateBuild(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Malformed build request.")
		return
	}
	s.mu.Lock()
	src, ok := s.sources[req.SourceID]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "This source is no longer loaded. Load it again.")
		return
	}

	ctx, cancel := context.WithCancel(s.ctx)
	j := &job{
		ID:       newID(),
		SourceID: src.ID,
		Label:    src.Label,
		Title:    firstNonEmpty(req.Title, src.Info.Title),
		Author:   firstNonEmpty(req.Author, src.Info.Author),
		Created:  time.Now(),
		state:    "queued",
		notify:   make(chan struct{}),
		cancel:   cancel,
	}
	j.Dir = filepath.Join(s.dataDir, "builds", j.ID)
	if err := os.MkdirAll(j.Dir, 0o755); err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.order = append(s.order, j.ID)
	s.pruneLocked()
	s.mu.Unlock()

	go s.run(ctx, j, src, req)
	writeJSON(w, http.StatusAccepted, s.summary(j))
}

func (s *Server) run(ctx context.Context, j *job, src *source, req buildRequest) {
	defer j.cancel()
	j.emit("stage", map[string]string{"stage": "queued"})
	select {
	case s.press <- struct{}{}:
	case <-ctx.Done():
		j.setState("cancelled")
		j.emit("cancelled", map[string]string{"message": "Build cancelled."})
		return
	}
	defer func() { <-s.press }()
	j.setState("running")

	logw := &lineWriter{emit: func(line string) { j.emit("log", map[string]string{"line": line}) }}
	res, err := build.Build(ctx, build.Options{
		Source:      src.Root,
		Out:         filepath.Join(j.Dir, "book.pdf"),
		Title:       req.Title,
		Subtitle:    req.Subtitle,
		Author:      req.Author,
		NoIndex:     req.NoIndex,
		Fonts:       req.Fonts,
		KeepTex:     true,
		Verify:      req.Verify,
		Log:         logw,
		FetchLog:    logw,
		CompilerLog: logw,
		OnStage: func(st build.Stage) {
			j.mu.Lock()
			j.stage = string(st)
			j.mu.Unlock()
			j.emit("stage", map[string]string{"stage": string(st)})
		},
	})
	logw.Flush()

	j.mu.Lock()
	switch {
	case ctx.Err() != nil:
		j.state = "cancelled"
	case err != nil:
		j.state = "failed"
		j.errMsg = err.Error()
	default:
		j.state = "done"
		j.result = res
		j.Title = firstNonEmpty(res.Title, j.Title)
		j.Author = firstNonEmpty(res.Author, j.Author)
	}
	state := j.state
	j.mu.Unlock()

	switch state {
	case "cancelled":
		j.emit("cancelled", map[string]string{"message": "Build cancelled."})
	case "failed":
		j.emit("failed", map[string]string{"message": err.Error()})
	default:
		j.emit("done", s.summary(j))
	}
}

// pruneLocked drops the oldest finished builds beyond keepBuilds.
func (s *Server) pruneLocked() {
	for len(s.order) > keepBuilds {
		old := s.jobs[s.order[0]]
		old.mu.Lock()
		done := old.finished()
		old.mu.Unlock()
		if !done {
			return
		}
		os.RemoveAll(old.Dir)
		delete(s.jobs, old.ID)
		s.order = s.order[1:]
	}
}

type resultJSON struct {
	Pages          int     `json:"pages"`
	Chapters       int     `json:"chapters"`
	Figures        int     `json:"figures"`
	FiguresAudited int     `json:"figuresAudited"`
	Overlaps       int     `json:"overlaps"`
	OverfullHbox   int     `json:"overfull"`
	Verified       bool    `json:"verified"`
	SizeBytes      int64   `json:"sizeBytes"`
	MissingGlyphs  int     `json:"missingGlyphs"`
	MissingSample  string  `json:"missingSample"`
	Fonts          string  `json:"fonts"`
	Seconds        float64 `json:"seconds"`
	Subtitle       string  `json:"subtitle"`
}

type jobJSON struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Title   string      `json:"title"`
	Author  string      `json:"author"`
	State   string      `json:"state"`
	Stage   string      `json:"stage"`
	Created time.Time   `json:"created"`
	Error   string      `json:"error,omitempty"`
	Result  *resultJSON `json:"result,omitempty"`
}

func (s *Server) summary(j *job) jobJSON {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := jobJSON{ID: j.ID, Label: j.Label, Title: j.Title, Author: j.Author, State: j.state, Stage: j.stage, Created: j.Created, Error: j.errMsg}
	if r := j.result; r != nil {
		pages := r.Pages
		out.Result = &resultJSON{
			Pages: pages, Chapters: r.Chapters, Figures: r.Figures, FiguresAudited: r.FiguresAudited,
			Overlaps: len(r.Overlaps), OverfullHbox: r.OverfullHbox, Verified: r.Verified,
			SizeBytes: r.Size, Seconds: r.Duration.Seconds(), Subtitle: r.Subtitle,
			MissingGlyphs: len(r.MissingGlyphs), MissingSample: sampleRunes(r.MissingGlyphs, 40),
			Fonts: r.Fonts,
		}
	}
	return out
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*job, bool) {
	s.mu.Lock()
	j, ok := s.jobs[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "No such build.")
	}
	return j, ok
}

func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	jobs := make([]*job, 0, len(s.order))
	for _, id := range s.order {
		jobs = append(jobs, s.jobs[id])
	}
	s.mu.Unlock()
	out := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, s.summary(j))
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.lookup(w, r); ok {
		writeJSON(w, http.StatusOK, s.summary(j))
	}
}

func (s *Server) handleCancelBuild(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.lookup(w, r); ok {
		j.cancel()
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleEvents streams a build's history and live progress as Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	j, ok := s.lookup(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	next := 0
	for {
		j.mu.Lock()
		pending := j.events[next:]
		wait := j.notify
		finished := j.finished()
		j.mu.Unlock()

		for _, ev := range pending {
			b, _ := json.Marshal(ev.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
			next++
		}
		flusher.Flush()
		if finished {
			return
		}
		select {
		case <-wait:
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, ": keep-alive\n\n")
		}
	}
}

var reSlug = regexp.MustCompile(`[^A-Za-z0-9]+`)

func (s *Server) handleArtifact(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, ok := s.lookup(w, r)
		if !ok {
			return
		}
		j.mu.Lock()
		done := j.state == "done"
		title := j.Title
		j.mu.Unlock()
		if !done {
			writeError(w, http.StatusConflict, "This build hasn't finished.")
			return
		}
		file := filepath.Join(j.Dir, "book."+kind)
		ctype := "application/pdf"
		if kind == "tex" {
			ctype = "text/x-tex; charset=utf-8"
		}
		name := strings.Trim(reSlug.ReplaceAllString(title, "-"), "-")
		if name == "" {
			name = "book"
		}
		disp := "inline"
		if r.URL.Query().Get("download") == "1" {
			disp = "attachment"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s.%s"`, disp, name, kind))
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, file)
	}
}

// ---- helpers --------------------------------------------------------------------

// lineWriter turns a byte stream (including git's \r progress updates) into
// complete log lines.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	for _, c := range p {
		if c == '\n' || c == '\r' {
			lw.flushLocked()
			continue
		}
		lw.buf = append(lw.buf, c)
	}
	return len(p), nil
}

func (lw *lineWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.flushLocked()
}

func (lw *lineWriter) flushLocked() {
	line := strings.TrimRight(string(lw.buf), " \t")
	lw.buf = lw.buf[:0]
	if strings.TrimSpace(line) != "" {
		lw.emit(line)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func openBrowser(link string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	_ = cmd.Start()
}

func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// sampleRunes joins up to n runes with spaces, for display.
func sampleRunes(rs []rune, n int) string {
	if len(rs) > n {
		rs = rs[:n]
	}
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}
