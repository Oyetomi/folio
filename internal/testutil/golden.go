// Package testutil holds shared helpers for Folio's golden-file tests.
//
// Golden tests compare generated output (LaTeX, Markdown, TikZ) against
// checked-in expected files. When output changes intentionally, regenerate
// the expected files with:
//
//	go test ./... -update
//
// and review the resulting diff with `git diff` before committing.
package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Update rewrites golden files instead of comparing against them.
var Update = flag.Bool("update", false, "rewrite golden files with current output")

// Golden compares got against the contents of goldenPath. With -update it
// writes got to goldenPath instead.
func Golden(t *testing.T, goldenPath string, got string) {
	t.Helper()
	if *Update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir for golden %s: %v", goldenPath, err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
		return
	}
	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test ./... -update` to create it)", goldenPath, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	t.Errorf("output differs from %s (run `go test ./... -update` if the change is intended)\n%s",
		goldenPath, firstDiff(want, got))
}

// firstDiff reports the first differing line with a little context, which is
// far more readable than dumping two multi-kilobyte LaTeX documents.
func firstDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) > n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w == g {
			continue
		}
		var b strings.Builder
		start := i - 3
		if start < 0 {
			start = 0
		}
		for j := start; j < i && j < len(wl); j++ {
			b.WriteString("    " + wl[j] + "\n")
		}
		b.WriteString("  - " + w + "\n")
		b.WriteString("  + " + g + "\n")
		b.WriteString("  (first difference at line " + strconv.Itoa(i+1) + ")")
		return b.String()
	}
	return "  (outputs differ only in trailing content)"
}
