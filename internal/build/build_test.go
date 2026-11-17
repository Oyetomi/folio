package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oyetomi/folio/internal/latex"
)

func writeBook(t *testing.T, yml string) string {
	t.Helper()
	dir := t.TempDir()
	if yml != "" {
		if err := os.WriteFile(filepath.Join(dir, "folio.yml"), []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "01-intro.md"), []byte("# Intro\n\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// isolate hides the developer's real ~/.folio/fonts from the test.
func isolate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FOLIO_FONTS_DIR", "")
	t.Setenv("FOLIO_FONTS", "")
}

func TestProFontsRequiredWhenRequested(t *testing.T) {
	isolate(t)
	for _, tc := range []struct{ name, yml, flag string }{
		{"flag", "", "pro"},
		{"yml", "title: X\nfonts: pro\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Source: writeBook(t, tc.yml), Out: filepath.Join(t.TempDir(), "x.pdf"), Fonts: tc.flag})
			if err == nil || !strings.Contains(err.Error(), "MinionPro-Regular.otf") {
				t.Fatalf("want a clear missing-pro-fonts error, got %v", err)
			}
		})
	}
}

func TestUnknownFontMode(t *testing.T) {
	isolate(t)
	_, err := Build(context.Background(), Options{Source: writeBook(t, ""), Out: filepath.Join(t.TempDir(), "x.pdf"), Fonts: "comic"})
	if err == nil || !strings.Contains(err.Error(), `unknown fonts setting "comic"`) {
		t.Fatalf("got %v", err)
	}
}

func TestFontModePrecedence(t *testing.T) {
	cases := []struct{ flag, yml, env, want string }{
		{"", "", "", "open"},
		{"", "mono", "", "mono"},
		{"", "", "mono", "mono"},
		{"open", "mono", "pro", "open"}, // flag beats yml beats env
		{"", "mono", "pro", "mono"},
	}
	for _, c := range cases {
		got, err := latex.ParseFontMode(firstSet(c.flag, c.yml, c.env))
		if err != nil || string(got) != c.want {
			t.Errorf("flag=%q yml=%q env=%q: got %q, %v; want %q", c.flag, c.yml, c.env, got, err, c.want)
		}
	}
}

// Regression: `folio build` with no argument (source ".") found no chapters
// because the root "." matched the hidden-directory rule.
func TestLoadCurrentDirectory(t *testing.T) {
	dir := writeBook(t, "")
	t.Chdir(dir)
	ms, err := Load(".", t.TempDir())
	if err != nil {
		t.Fatalf("Load(\".\"): %v", err)
	}
	if len(ms.Docs) != 1 {
		t.Fatalf("found %d chapters, want 1", len(ms.Docs))
	}
	if ms.Config.Title == "." || ms.Config.Title == "" {
		t.Errorf("title = %q, want the folder name", ms.Config.Title)
	}
}
