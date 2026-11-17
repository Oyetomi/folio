package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingGlyphs(t *testing.T) {
	log := `Missing character: There is no ^^ea^^b5^^ad (U+AD6D) in font [fonts/MinionPro-Regular.otf]/OT:script=latn;!
Missing character: There is no 국 (U+AD6D) in font [x]!
Missing character: There is no 🚀 (U+1F680) in font [x]!
Overfull \hbox (3.2pt too wide) in paragraph`
	p := filepath.Join(t.TempDir(), "book.log")
	if err := os.WriteFile(p, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	got := MissingGlyphs(p)
	if string(got) != "국🚀" {
		t.Errorf("MissingGlyphs = %q, want distinct 국🚀", string(got))
	}
}
