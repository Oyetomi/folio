package latex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Oyetomi/folio/internal/assets"
)

// Fallback fonts for scripts the main typefaces cannot render. Without this,
// XeTeX silently drops the glyphs (it only logs "Missing character").

type scriptFont struct {
	script  assets.Script
	command string // LaTeX font-switch command
	file    string // staged file name stem
	label   string // human-readable script name
}

var scriptFonts = []scriptFont{
	{assets.ScriptCJK, `\folioCJK`, "fallback-cjk", "Chinese/Japanese"},
	{assets.ScriptHangul, `\folioHangul`, "fallback-hangul", "Korean"},
}

// Swappable in tests.
var (
	findFallback  = assets.FindFallbackFont
	stageFallback = assets.StageFallbackFont
)

// applyScriptFallback finds CJK and Hangul text in tex, stages a font that
// covers it, wraps each run in a font switch and adds the font definitions
// before \begin{document}. notes describe what happened, for the build log.
func applyScriptFallback(tex, workDir, fontsDir string) (out string, notes []string) {
	need := map[assets.Script]map[rune]bool{}
	for _, r := range tex {
		if s := assets.ScriptOf(r); s != "" {
			if need[s] == nil {
				need[s] = map[rune]bool{}
			}
			need[s][r] = true
		}
	}
	if len(need) == 0 {
		return tex, nil
	}

	active := map[assets.Script]string{} // script -> command
	var defs []string
	for _, sf := range scriptFonts {
		set := need[sf.script]
		if len(set) == 0 {
			continue
		}
		runes := make([]rune, 0, len(set))
		for r := range set {
			runes = append(runes, r)
		}
		sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
		font, ok := findFallback(runes, fontsDir)
		if !ok {
			notes = append(notes, fmt.Sprintf(
				"No installed font covers the %s text (%d characters); it will be missing from the PDF. Install Noto Serif CJK or put a suitable font in ~/.folio/fonts.",
				sf.label, len(runes)))
			continue
		}
		file, err := stageFallback(workDir, font, sf.file)
		if err != nil {
			notes = append(notes, fmt.Sprintf("Could not stage %s for %s text: %v", font.Path, sf.label, err))
			continue
		}
		defs = append(defs, fmt.Sprintf(`\newfontfamily%s{%s}[Path=fonts/,FontIndex=%d,AutoFakeBold=2.5,AutoFakeSlant=0.15]`,
			sf.command, file, font.Index))
		active[sf.script] = sf.command
		name := font.Family
		if name == "" {
			name = font.Path
		}
		notes = append(notes, fmt.Sprintf("%s text set in %s.", sf.label, name))
	}
	if len(active) == 0 {
		return tex, notes
	}

	// Let XeTeX break lines between ideographs, which have no spaces.
	if active[assets.ScriptCJK] != "" {
		defs = append(defs, `\XeTeXlinebreaklocale "zh"`, `\XeTeXlinebreakskip=0pt plus 1pt`)
	}
	tex = wrapScriptRuns(tex, active)
	if i := strings.Index(tex, `\begin{document}`); i >= 0 {
		tex = tex[:i] + "% Fallback fonts for non-Latin scripts\n" + strings.Join(defs, "\n") + "\n" + tex[i:]
	}
	return tex, notes
}

// Commands whose next argument is a URL or key, not printable text.
var verbatimArgCommands = map[string]bool{
	"href": true, "url": true, "label": true, "ref": true, "pageref": true,
	"hyperref": true, "includegraphics": true, "input": true, "include": true,
}

// wrapScriptRuns wraps each maximal run of same-script characters in
// {<command> ...}, skipping the arguments of verbatimArgCommands.
func wrapScriptRuns(tex string, active map[assets.Script]string) string {
	src := []rune(tex)
	var b strings.Builder
	b.Grow(len(tex) + 64)
	for i := 0; i < len(src); {
		r := src[i]
		if r == '\\' && i+1 < len(src) {
			// Copy a control word, and for URL/key commands its argument verbatim.
			j := i + 1
			for j < len(src) && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z')) {
				j++
			}
			if j == i+1 { // control symbol like \{ or \\
				j = i + 2
			}
			name := string(src[i+1 : j])
			b.WriteString(string(src[i:j]))
			i = j
			if verbatimArgCommands[name] {
				i = copyGroup(src, i, &b)
			}
			continue
		}
		if cmd, ok := active[assets.ScriptOf(r)]; ok {
			s := assets.ScriptOf(r)
			j := i
			for j < len(src) && assets.ScriptOf(src[j]) == s {
				j++
			}
			b.WriteString("{" + cmd + " " + string(src[i:j]) + "}")
			i = j
			continue
		}
		b.WriteRune(r)
		i++
	}
	return b.String()
}

// copyGroup copies an optional [..] and one balanced {..} group starting at i.
func copyGroup(src []rune, i int, b *strings.Builder) int {
	for _, pair := range [][2]rune{{'[', ']'}, {'{', '}'}} {
		if i >= len(src) || src[i] != pair[0] {
			continue
		}
		depth := 0
		j := i
		for ; j < len(src); j++ {
			switch src[j] {
			case pair[0]:
				depth++
			case pair[1]:
				depth--
			}
			if depth == 0 {
				j++
				break
			}
		}
		b.WriteString(string(src[i:j]))
		i = j
	}
	return i
}
