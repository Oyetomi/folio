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

	// Candidate directories for local custom OpenType fonts
	var candidates []string
	if customFontsDir != "" {
		candidates = append(candidates, customFontsDir)
	}
	if envDir := os.Getenv("FOLIO_FONTS_DIR"); envDir != "" {
		candidates = append(candidates, envDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".folio", "fonts"))
	}

	for _, dir := range candidates {
		minionPath := filepath.Join(dir, "MinionPro-Regular.otf")
		myriadPath := filepath.Join(dir, "MyriadPro-Regular.otf")
		if fileExists(minionPath) && fileExists(myriadPath) {
			files, err := os.ReadDir(dir)
			if err == nil {
				for _, f := range files {
					if f.IsDir() {
						continue
					}
					ext := filepath.Ext(f.Name())
					if ext == ".otf" || ext == ".ttf" {
						if data, err := os.ReadFile(filepath.Join(dir, f.Name())); err == nil {
							_ = os.WriteFile(filepath.Join(destDir, f.Name()), data, 0o644)
						}
					}
				}
				return true, nil
			}
		}
	}

	return false, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
