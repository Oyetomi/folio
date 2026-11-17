package assets

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Script groups characters that the bundled Latin fonts (Ubuntu Mono, Minion,
// Myriad) cannot render and that need a fallback font.
type Script string

const (
	ScriptCJK    Script = "cjk"    // Han, kana, bopomofo, CJK punctuation, fullwidth forms
	ScriptHangul Script = "hangul" // Korean syllables and jamo
)

// ScriptOf classifies r, returning "" for characters the main fonts handle.
func ScriptOf(r rune) Script {
	switch {
	case r >= 0x1100 && r <= 0x11FF, r >= 0x3130 && r <= 0x318F, r >= 0xA960 && r <= 0xA97F,
		r >= 0xAC00 && r <= 0xD7AF, r >= 0xD7B0 && r <= 0xD7FF:
		return ScriptHangul
	case r >= 0x2E80 && r <= 0x2FDF, // radicals
		r >= 0x3000 && r <= 0x30FF,                             // CJK punctuation, hiragana, katakana
		r >= 0x3100 && r <= 0x312F, r >= 0x31A0 && r <= 0x31FF, // bopomofo, kana ext
		r >= 0x3200 && r <= 0x33FF,                             // enclosed / compatibility
		r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, // Han
		r >= 0xF900 && r <= 0xFAFF,   // compatibility ideographs
		r >= 0xFF00 && r <= 0xFFEF,   // halfwidth & fullwidth forms
		r >= 0x20000 && r <= 0x3FFFF: // Han extensions
		return ScriptCJK
	}
	return ""
}

// FallbackFont is one face inside a font file.
type FallbackFont struct {
	Path   string // absolute path on disk
	Index  int    // face index within a .ttc collection (0 for .ttf/.otf)
	Family string
}

// Candidate files in preference order: serif faces first (they sit better
// next to Minion), then sans, then broad-coverage last resorts.
var fallbackCandidates = []string{
	// macOS
	"/System/Library/Fonts/Supplemental/Songti.ttc",
	"/System/Library/Fonts/ヒラギノ明朝 ProN.ttc",
	"/System/Library/Fonts/Supplemental/AppleMyungjo.ttf",
	"/System/Library/Fonts/Hiragino Sans GB.ttc",
	"/System/Library/Fonts/AppleSDGothicNeo.ttc",
	// Linux (Debian/Ubuntu, Fedora, Arch)
	"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
	"/usr/share/fonts/noto-cjk/NotoSerifCJK-Regular.ttc",
	"/usr/share/fonts/google-noto-cjk/NotoSerifCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/nanum/NanumMyeongjo.ttf",
	// Windows
	`C:\Windows\Fonts\simsun.ttc`,
	`C:\Windows\Fonts\batang.ttc`,
	`C:\Windows\Fonts\msyh.ttc`,
	// last resorts
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
	"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
}

// FindFallbackFont returns the first installed face that covers every rune in
// need. User font directories (custom dir, $FOLIO_FONTS_DIR, ~/.folio/fonts)
// are searched before system locations.
func FindFallbackFont(need []rune, customFontsDir string) (*FallbackFont, bool) {
	if len(need) == 0 {
		return nil, false
	}
	var files []string
	for _, dir := range userFontDirs(customFontsDir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".ttf", ".otf", ".ttc", ".otc":
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	files = append(files, fallbackCandidates...)

	for _, f := range files {
		faces, err := readFaces(f)
		if err != nil {
			continue
		}
		// Prefer an upright regular face within the file.
		sort.SliceStable(faces, func(i, j int) bool { return faceRank(faces[i]) < faceRank(faces[j]) })
		for _, fc := range faces {
			if fc.covers(need) {
				return &FallbackFont{Path: f, Index: fc.index, Family: fc.family}, true
			}
		}
	}
	return nil, false
}

func userFontDirs(custom string) []string {
	var dirs []string
	if custom != "" {
		dirs = append(dirs, custom)
	}
	if env := os.Getenv("FOLIO_FONTS_DIR"); env != "" {
		dirs = append(dirs, env)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".folio", "fonts"))
	}
	return dirs
}

// StageFallbackFont makes the font file available to Tectonic under
// workDir/fonts/<name><ext> (symlinked when possible: CJK fonts are large).
func StageFallbackFont(workDir string, f *FallbackFont, name string) (string, error) {
	dir := filepath.Join(workDir, "fonts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file := name + strings.ToLower(filepath.Ext(f.Path))
	dst := filepath.Join(dir, file)
	_ = os.Remove(dst)
	if err := os.Symlink(f.Path, dst); err == nil {
		return file, nil
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	return file, os.WriteFile(dst, data, 0o644)
}

// ---- minimal sfnt reader: name + cmap -------------------------------------

type face struct {
	index     int
	family    string
	subfamily string
	data      []byte
	cmapOff   uint32
}

func faceRank(f face) int {
	s := strings.ToLower(f.subfamily)
	switch {
	case s == "regular" || s == "w3" || s == "book" || s == "normal" || s == "roman":
		return 0
	case strings.Contains(s, "light") || s == "w2" || s == "w4" || s == "medium":
		return 1
	case strings.Contains(s, "bold") || strings.Contains(s, "black") || strings.Contains(s, "heavy"):
		return 3
	}
	return 2
}

var errNotFont = errors.New("not an sfnt font")

func readFaces(path string) ([]face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 {
		return nil, errNotFont
	}
	var offsets []uint32
	if string(data[:4]) == "ttcf" {
		n := int(binary.BigEndian.Uint32(data[8:12]))
		if n <= 0 || 12+4*n > len(data) {
			return nil, errNotFont
		}
		for i := 0; i < n; i++ {
			offsets = append(offsets, binary.BigEndian.Uint32(data[12+4*i:]))
		}
	} else {
		offsets = []uint32{0}
	}
	var faces []face
	for i, off := range offsets {
		fc, ok := parseFace(data, off)
		if !ok {
			continue
		}
		fc.index = i
		faces = append(faces, fc)
	}
	if len(faces) == 0 {
		return nil, errNotFont
	}
	return faces, nil
}

func parseFace(data []byte, off uint32) (face, bool) {
	o := int(off)
	if o+12 > len(data) {
		return face{}, false
	}
	numTables := int(binary.BigEndian.Uint16(data[o+4:]))
	fc := face{data: data}
	var nameOff uint32
	for t := 0; t < numTables; t++ {
		rec := o + 12 + 16*t
		if rec+16 > len(data) {
			return face{}, false
		}
		tag := string(data[rec : rec+4])
		tOff := binary.BigEndian.Uint32(data[rec+8:])
		switch tag {
		case "cmap":
			fc.cmapOff = tOff
		case "name":
			nameOff = tOff
		}
	}
	if fc.cmapOff == 0 || int(fc.cmapOff)+4 > len(data) {
		return face{}, false
	}
	if nameOff != 0 {
		fc.family, fc.subfamily = readNames(data, int(nameOff))
	}
	return fc, true
}

func readNames(data []byte, o int) (family, sub string) {
	if o+6 > len(data) {
		return
	}
	count := int(binary.BigEndian.Uint16(data[o+2:]))
	strOff := o + int(binary.BigEndian.Uint16(data[o+4:]))
	for i := 0; i < count; i++ {
		r := o + 6 + 12*i
		if r+12 > len(data) {
			return
		}
		platform := binary.BigEndian.Uint16(data[r:])
		lang := binary.BigEndian.Uint16(data[r+4:])
		id := binary.BigEndian.Uint16(data[r+6:])
		length := int(binary.BigEndian.Uint16(data[r+8:]))
		so := strOff + int(binary.BigEndian.Uint16(data[r+10:]))
		if (id != 1 && id != 2) || so+length > len(data) {
			continue
		}
		var s string
		switch {
		case platform == 3 && lang == 0x409: // Windows, US English, UTF-16BE
			u := make([]rune, 0, length/2)
			for k := 0; k+1 < length; k += 2 {
				u = append(u, rune(binary.BigEndian.Uint16(data[so+k:])))
			}
			s = string(u)
		case platform == 1 && lang == 0: // Mac Roman, English
			s = string(data[so : so+length])
		default:
			continue
		}
		if id == 1 && family == "" {
			family = s
		} else if id == 2 && sub == "" {
			sub = s
		}
	}
	return
}

// covers reports whether the face maps every rune in need to a glyph.
func (f face) covers(need []rune) bool {
	lookup := f.cmapLookup()
	if lookup == nil {
		return false
	}
	for _, r := range need {
		if !lookup(r) {
			return false
		}
	}
	return true
}

// cmapLookup returns a coverage test from the best Unicode subtable
// (format 12 preferred, else format 4).
func (f face) cmapLookup() func(rune) bool {
	d := f.data
	c := int(f.cmapOff)
	n := int(binary.BigEndian.Uint16(d[c+2:]))
	var best, bestFmt int
	for i := 0; i < n; i++ {
		r := c + 4 + 8*i
		if r+8 > len(d) {
			break
		}
		plat := binary.BigEndian.Uint16(d[r:])
		enc := binary.BigEndian.Uint16(d[r+2:])
		sub := c + int(binary.BigEndian.Uint32(d[r+4:]))
		if sub+2 > len(d) || !(plat == 0 || (plat == 3 && (enc == 1 || enc == 10))) {
			continue
		}
		format := int(binary.BigEndian.Uint16(d[sub:]))
		if format == 12 && bestFmt != 12 {
			best, bestFmt = sub, 12
		} else if format == 4 && bestFmt == 0 {
			best, bestFmt = sub, 4
		}
	}
	switch bestFmt {
	case 12:
		if best+16 > len(d) {
			return nil
		}
		groups := int(binary.BigEndian.Uint32(d[best+12:]))
		base := best + 16
		if base+12*groups > len(d) {
			return nil
		}
		return func(r rune) bool {
			lo, hi := 0, groups-1
			for lo <= hi {
				m := (lo + hi) / 2
				g := base + 12*m
				start := rune(binary.BigEndian.Uint32(d[g:]))
				end := rune(binary.BigEndian.Uint32(d[g+4:]))
				switch {
				case r < start:
					hi = m - 1
				case r > end:
					lo = m + 1
				default:
					return binary.BigEndian.Uint32(d[g+8:])+uint32(r-start) != 0
				}
			}
			return false
		}
	case 4:
		if best+14 > len(d) {
			return nil
		}
		segX2 := int(binary.BigEndian.Uint16(d[best+6:]))
		ends := best + 14
		starts := ends + segX2 + 2
		deltas := starts + segX2
		rangeOffs := deltas + segX2
		if rangeOffs+segX2 > len(d) {
			return nil
		}
		return func(r rune) bool {
			if r > 0xFFFF {
				return false
			}
			for s := 0; s < segX2; s += 2 {
				end := rune(binary.BigEndian.Uint16(d[ends+s:]))
				if r > end {
					continue
				}
				start := rune(binary.BigEndian.Uint16(d[starts+s:]))
				if r < start {
					return false
				}
				ro := int(binary.BigEndian.Uint16(d[rangeOffs+s:]))
				if ro == 0 {
					return uint16(int(r)+int(int16(binary.BigEndian.Uint16(d[deltas+s:])))) != 0
				}
				gi := rangeOffs + s + ro + 2*int(r-start)
				if gi+2 > len(d) {
					return false
				}
				return binary.BigEndian.Uint16(d[gi:]) != 0
			}
			return false
		}
	}
	return nil
}
