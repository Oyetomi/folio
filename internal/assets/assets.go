package assets

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed fonts/*
var embeddedFonts embed.FS

// StageFonts extracts embedded open-source fonts into workDir/fonts, and if
// MinionPro + MyriadPro are available in customFontsDir, $FOLIO_FONTS_DIR, or
// ~/.folio/fonts, copies them in and returns hasProFonts = true.
func StageFonts(workDir string, customFontsDir string) (bool, error) {
	destDir := filepath.Join(workDir, "fonts")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false, err
	}

	entries, err := fs.ReadDir(embeddedFonts, "fonts")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := embeddedFonts.ReadFile("fonts/" + entry.Name())
		if err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(destDir, entry.Name()), data, 0o644); err != nil {
			return false, err
		}
	}

	// Copy local Minion Pro / Myriad Pro (and any other local faces) next to
	// the embedded ones; only used when the build asks for fonts: pro.
	dir, ok := FindProFonts(customFontsDir)
	if !ok {
		return false, nil
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return false, nil
	}
	for _, f := range files {
		if ext := filepath.Ext(f.Name()); !f.IsDir() && (ext == ".otf" || ext == ".ttf") {
			if data, err := os.ReadFile(filepath.Join(dir, f.Name())); err == nil {
				_ = os.WriteFile(filepath.Join(destDir, f.Name()), data, 0o644)
			}
		}
	}
	return true, nil
}

// FindProFonts returns the first font directory (custom dir, $FOLIO_FONTS_DIR,
// ~/.folio/fonts) that holds Minion Pro and Myriad Pro.
func FindProFonts(customFontsDir string) (string, bool) {
	for _, dir := range userFontDirs(customFontsDir) {
		if fileExists(filepath.Join(dir, "MinionPro-Regular.otf")) && fileExists(filepath.Join(dir, "MyriadPro-Regular.otf")) {
			return dir, true
		}
	}
	return "", false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Fonts returns the embedded open-source font files (UbuntuMono *.otf), e.g.
// for serving to the web UI so it matches the typeset PDF.
func Fonts() fs.FS {
	sub, err := fs.Sub(embeddedFonts, "fonts")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return sub
}
