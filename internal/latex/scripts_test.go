package latex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oyetomi/folio/internal/assets"
)

func TestWrapScriptRuns(t *testing.T) {
	active := map[assets.Script]string{assets.ScriptCJK: `\folioCJK`, assets.ScriptHangul: `\folioHangul`}
	cases := map[string]string{
		"plain text":                     "plain text",
		"中文 and 한국어":                     `{\folioCJK 中文} and {\folioHangul 한국어}`,
		"日本語の本":                          `{\folioCJK 日本語の本}`,
		`\textbf{粗体}`:                    `\textbf{{\folioCJK 粗体}}`,
		`\href{https://x.cn/中文}{中文}`:     `\href{https://x.cn/中文}{{\folioCJK 中文}}`,
		`\label{sec:中}\ref{sec:中} 中`:     `\label{sec:中}\ref{sec:中} {\folioCJK 中}`,
		`\includegraphics[w=1]{图.png} 图`: `\includegraphics[w=1]{图.png} {\folioCJK 图}`,
		`\\ 中`:                           `\\ {\folioCJK 中}`,
	}
	for in, want := range cases {
		if got := wrapScriptRuns(in, active); got != want {
			t.Errorf("wrapScriptRuns(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestApplyScriptFallback(t *testing.T) {
	origFind, origStage := findFallback, stageFallback
	defer func() { findFallback, stageFallback = origFind, origStage }()

	var asked []rune
	findFallback = func(need []rune, _ string) (*assets.FallbackFont, bool) {
		if assets.ScriptOf(need[0]) == assets.ScriptHangul {
			return nil, false // simulate: no Korean font installed
		}
		asked = need
		return &assets.FallbackFont{Path: "/fonts/Song.ttc", Index: 3, Family: "Song"}, true
	}
	stageFallback = func(_ string, f *assets.FallbackFont, name string) (string, error) {
		return name + ".ttc", nil
	}

	tex := "\\documentclass{book}\n\\begin{document}\n你好 world 안녕\n\\end{document}"
	out, notes := applyScriptFallback(tex, t.TempDir(), "")
	if string(asked) != "你好" {
		t.Errorf("asked font for %q, want sorted unique CJK runes", string(asked))
	}
	for _, want := range []string{
		`\newfontfamily\folioCJK{fallback-cjk.ttc}[Path=fonts/,FontIndex=3`,
		`\XeTeXlinebreaklocale "zh"`,
		`{\folioCJK 你好} world 안녕`, // Hangul left alone: no font
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Index(out, `\newfontfamily`) > strings.Index(out, `\begin{document}`) {
		t.Error("font definitions must precede \\begin{document}")
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "set in Song") || !strings.Contains(joined, "No installed font covers the Korean text") {
		t.Errorf("notes = %q", notes)
	}
}

func TestApplyScriptFallbackNoop(t *testing.T) {
	tex := "\\begin{document}Latin only, café, naïve, Ελληνικά, Русский\\end{document}"
	if out, notes := applyScriptFallback(tex, t.TempDir(), ""); out != tex || notes != nil {
		t.Errorf("Latin/Greek/Cyrillic text should not be touched: %q %v", out, notes)
	}
}

func TestFallbackFontStagedForTectonic(t *testing.T) {
	// Real font lookup: uses whatever CJK font this machine has.
	f, ok := assets.FindFallbackFont([]rune("中文"), "")
	if !ok {
		t.Skip("no CJK font installed")
	}
	dir := t.TempDir()
	name, err := assets.StageFallbackFont(dir, f, "fallback-cjk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fonts", name)); err != nil {
		t.Fatalf("staged font not readable: %v", err)
	}
}
