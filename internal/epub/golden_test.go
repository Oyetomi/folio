package epub

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oyetomi/folio/internal/latex"
	"github.com/Oyetomi/folio/internal/testutil"
)

// zipFixture packs testdata/<name>/ into an .epub (mimetype stored first and
// uncompressed, as the spec requires) and returns its path.
func zipFixture(t *testing.T, name string) string {
	t.Helper()
	srcDir := filepath.Join("testdata", name)
	out := filepath.Join(t.TempDir(), name+".epub")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	add := func(rel string, method uint16) {
		data, err := os.ReadFile(filepath.Join(srcDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(rel), Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", zip.Store)
	err = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, p)
		if rel != "mimetype" {
			add(rel, zip.Deflate)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExtractEPUBGolden(t *testing.T) {
	workDir := t.TempDir()
	cfg, docs, err := ExtractEPUB(zipFixture(t, "minimal"), workDir)
	if err != nil {
		t.Fatalf("ExtractEPUB: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "title: %s\nauthor: %s\ndescription: %s\nlicense: %s\ncover: %v\n",
		cfg.Title, cfg.Author, cfg.Description, cfg.LicenseText, cfg.CoverImage != "")
	for _, d := range docs {
		fmt.Fprintf(&b, "\n===== [%d.%d.%d] part=%d %q (%s)\n%s\n",
			d.IDTuple[0], d.IDTuple[1], d.IDTuple[2], d.PartIdx, d.Title, d.FilePath, d.RawText)
	}
	testutil.Golden(t, filepath.Join("testdata", "minimal.golden.md"), b.String())
}

// TestEPUBImagesResolve checks that images extracted from the archive are
// picked up when chapters are compiled the way `folio build book.epub` does
// it (rootDir == workDir, no source tree on disk).
func TestEPUBImagesResolve(t *testing.T) {
	workDir := t.TempDir()
	cfg, docs, err := ExtractEPUB(zipFixture(t, "minimal"), workDir)
	if err != nil {
		t.Fatal(err)
	}
	tex, _, err := latex.BuildTexDocument(workDir, workDir, cfg, docs, latex.FontsOpen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tex, `\includegraphics{figure.png}`) {
		t.Errorf("EPUB figure was not included in generated LaTeX")
	}
}
