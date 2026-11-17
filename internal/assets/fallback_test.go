package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestScriptOf(t *testing.T) {
	cases := map[rune]Script{
		'a': "", 'é': "", 'Ж': "", 'λ': "",
		'中': ScriptCJK, 'の': ScriptCJK, 'カ': ScriptCJK, '，': ScriptCJK, '。': ScriptCJK,
		'한': ScriptHangul, 'ㄱ': ScriptHangul,
	}
	for r, want := range cases {
		if got := ScriptOf(r); got != want {
			t.Errorf("ScriptOf(%q) = %q, want %q", r, got, want)
		}
	}
}

// The embedded Ubuntu Mono is a known quantity: Latin and Cyrillic yes, CJK no.
func TestCmapCoverage(t *testing.T) {
	data, err := fs.ReadFile(embeddedFonts, "fonts/UbuntuMono-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "u.otf")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	faces, err := readFaces(p)
	if err != nil || len(faces) != 1 {
		t.Fatalf("readFaces: %v (%d faces)", err, len(faces))
	}
	f := faces[0]
	if f.family != "Ubuntu Mono" {
		t.Errorf("family = %q", f.family)
	}
	if !f.covers([]rune("Hello, Привет")) {
		t.Error("Ubuntu Mono should cover Latin and Cyrillic")
	}
	if f.covers([]rune("中")) || f.covers([]rune("한")) {
		t.Error("Ubuntu Mono should not cover CJK")
	}
}

func TestFindFallbackFontRejectsNonCovering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FOLIO_FONTS_DIR", "")
	// A rune no real font maps (unassigned plane-3 code point).
	if _, ok := FindFallbackFont([]rune{0x3FFFD}, ""); ok {
		t.Error("found a font for an unassigned code point")
	}
}
