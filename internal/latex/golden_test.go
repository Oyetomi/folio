package latex

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Oyetomi/folio/internal/assets"
	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/markdown"
	"github.com/Oyetomi/folio/internal/testutil"
	"github.com/Oyetomi/folio/internal/verify"
)

var bookFixtures = []string{"structured", "summary"}

// buildFixture runs the same discovery + LaTeX generation steps as `folio build`.
func buildFixture(t *testing.T, name string, fonts FontMode) (tex, workDir string) {
	t.Helper()
	rootDir := filepath.Join("testdata", "books", name)
	cfg, err := config.LoadOrDiscover(rootDir)
	if err != nil {
		t.Fatalf("LoadOrDiscover: %v", err)
	}
	docs, err := markdown.DiscoverChapters(rootDir, cfg)
	if err != nil {
		t.Fatalf("DiscoverChapters: %v", err)
	}
	if len(docs) == 0 {
		t.Fatalf("no chapters discovered in %s", rootDir)
	}
	workDir = t.TempDir()
	tex, _, err = BuildTexDocument(rootDir, workDir, cfg, docs, fonts)
	if err != nil {
		t.Fatalf("BuildTexDocument: %v", err)
	}
	return tex, workDir
}

func TestBuildTexDocumentGolden(t *testing.T) {
	for _, name := range bookFixtures {
		t.Run(name, func(t *testing.T) {
			tex, _ := buildFixture(t, name, FontsOpen)
			testutil.Golden(t, filepath.Join("testdata", "books", name+".tex"), tex)
		})
	}
}

var reIncludeGraphics = regexp.MustCompile(`\\includegraphics(?:\[[^\]]*\])?\{([^}]+)\}`)

// TestSameNamedImagesInDifferentDirs guards against images being staged by
// basename only: ch01/img/diagram.png and ch02/img/diagram.png are different
// pictures and each chapter must get its own.
func TestSameNamedImagesInDifferentDirs(t *testing.T) {
	tex, workDir := buildFixture(t, "structured", FontsOpen)
	var staged []string
	for _, m := range reIncludeGraphics.FindAllStringSubmatch(tex, -1) {
		staged = append(staged, m[1])
	}
	if len(staged) != 2 {
		t.Fatalf("expected 2 \\includegraphics, got %d: %v", len(staged), staged)
	}
	if staged[0] == staged[1] {
		t.Fatalf("both chapters reference the same staged image %q", staged[0])
	}
	src := filepath.Join("testdata", "books", "structured", "chapters")
	for i, want := range []string{"ch01/img/diagram.png", "ch02/img/diagram.png"} {
		wantData, err := os.ReadFile(filepath.Join(src, want))
		if err != nil {
			t.Fatal(err)
		}
		gotData, err := os.ReadFile(filepath.Join(workDir, staged[i]))
		if err != nil {
			t.Fatalf("staged image %q missing: %v", staged[i], err)
		}
		if !bytes.Equal(gotData, wantData) {
			t.Errorf("figure %d (%s) staged as %q has the wrong contents", i+1, want, staged[i])
		}
	}
}

// TestCompileWithTectonic is an end-to-end check that the generated LaTeX
// compiles in every font mode and passes the verification gate. Skipped with
// -short or when tectonic is not installed; the pro mode runs only when
// Minion Pro / Myriad Pro are installed locally.
func TestCompileWithTectonic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tectonic compile in -short mode")
	}
	tectonic, err := exec.LookPath("tectonic")
	if err != nil {
		t.Skip("tectonic not installed")
	}
	hasPro, err := assets.StageFonts(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	modes := []FontMode{FontsOpen, FontsMono}
	if hasPro {
		modes = append(modes, FontsPro)
	}
	for _, name := range bookFixtures {
		for _, mode := range modes {
			t.Run(name+"/"+string(mode), func(t *testing.T) {
				compileFixture(t, tectonic, name, mode)
			})
		}
	}
}

func compileFixture(t *testing.T, tectonic, name string, mode FontMode) {
	t.Helper()
	tex, workDir := buildFixture(t, name, mode)
	if _, err := assets.StageFonts(workDir, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "book.tex"), []byte(tex), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tectonic, "--keep-logs", "book.tex")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tectonic failed: %v\n%s", err, out)
	}
	pdf := filepath.Join(workDir, "book.pdf")
	if _, err := os.Stat(pdf); err != nil {
		t.Fatalf("no PDF produced: %v", err)
	}
	logPath := filepath.Join(workDir, "book.log")
	if missing := verify.MissingGlyphs(logPath); len(missing) > 0 {
		t.Errorf("%d glyphs missing from the fonts: %q", len(missing), string(missing))
	}
	if logData, err := os.ReadFile(logPath); err == nil && bytes.Contains(logData, []byte("Font shape `TU/")) && bytes.Contains(logData, []byte("undefined")) {
		t.Errorf("a requested font shape is undefined; check the font file names for mode %s", mode)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return
	}
	rep, err := verify.AuditPDF(pdf, logPath)
	if err != nil {
		t.Fatalf("AuditPDF: %v", err)
	}
	if len(rep.Overlaps) > 0 {
		t.Errorf("%d text overlaps in figures: %+v", len(rep.Overlaps), rep.Overlaps)
	}
	t.Logf("%d pages, %d figures, %d overfull hboxes", rep.Pages, rep.FiguresChecked, rep.OverfullHbox)
}
