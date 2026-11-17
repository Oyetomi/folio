package markdown

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/testutil"
)

// TestCompileChapterGolden compiles every testdata/chapters/*.md fixture and
// compares the generated LaTeX body against the matching .tex golden file.
func TestCompileChapterGolden(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "chapters", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no chapter fixtures found")
	}
	sort.Strings(fixtures)
	for _, fx := range fixtures {
		fx := fx
		name := strings.TrimSuffix(filepath.Base(fx), ".md")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(fx)
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.BookConfig{WorkDir: t.TempDir()}
			tex, figs := CompileChapterFile(string(raw), 1, cfg, fx)
			got := "% figures: " + strconv.Itoa(figs) + "\n" + tex
			testutil.Golden(t, strings.TrimSuffix(fx, ".md")+".tex", got)
		})
	}
}

// TestCompileChapterDeterministic guards against map-iteration or other
// nondeterminism leaking into the output, which would make goldens flaky.
func TestCompileChapterDeterministic(t *testing.T) {
	fixtures, _ := filepath.Glob(filepath.Join("testdata", "chapters", "*.md"))
	for _, fx := range fixtures {
		raw, err := os.ReadFile(fx)
		if err != nil {
			t.Fatal(err)
		}
		cfg := &config.BookConfig{WorkDir: t.TempDir()}
		first, _ := CompileChapterFile(string(raw), 1, cfg, fx)
		for i := 0; i < 5; i++ {
			again, _ := CompileChapterFile(string(raw), 1, cfg, fx)
			if again != first {
				t.Fatalf("%s: output changed between runs", fx)
			}
		}
	}
}
