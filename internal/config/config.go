package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type PartMeta struct {
	Index       int    `yaml:"index"`
	Folder      string `yaml:"folder"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}

type RenderConfig struct {
	CoverImage string `yaml:"cover_image"`
}

type BookConfig struct {
	Title         string       `yaml:"title"`
	Subtitle      string       `yaml:"subtitle"`
	Author        string       `yaml:"author"`
	Source        string       `yaml:"source"`
	Output        string       `yaml:"output"`
	CoverImage    string       `yaml:"cover_image"`
	RepoURL       string       `yaml:"repo_url"`
	SiteURL       string       `yaml:"site_url"`
	DefaultBranch string       `yaml:"default_branch"`
	Description   string       `yaml:"description"`
	Highlights    []string     `yaml:"highlights"`
	AuthorBio     string       `yaml:"author_bio"`
	LicenseText   string       `yaml:"license_text"`
	LicenseCode   string       `yaml:"license_code"`
	NoIndex       bool         `yaml:"no_index"`
	WorkDir       string       `yaml:"-"`
	Render        RenderConfig `yaml:"render"`
	Parts         []PartMeta   `yaml:"parts"`
}

// LoadOrDiscover loads folio.yml or go-pretty-pdf.yml from rootDir, or infers
// book metadata automatically from README.md and directory structure.
func LoadOrDiscover(rootDir string) (*BookConfig, error) {
	cfg := &BookConfig{
		Source:        "docs",
		Output:        "book.pdf",
		DefaultBranch: "master",
		LicenseText:   "Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0).",
		LicenseCode:   "MIT License (MIT).",
	}

	configCandidates := []string{
		filepath.Join(rootDir, "folio.yml"),
		filepath.Join(rootDir, "folio.yaml"),
		filepath.Join(rootDir, "go-pretty-pdf.yml"),
		filepath.Join(rootDir, "go-pretty-pdf.yaml"),
	}

	for _, p := range configCandidates {
		if data, err := os.ReadFile(p); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
			break
		}
	}

	if cfg.CoverImage == "" && cfg.Render.CoverImage != "" {
		cfg.CoverImage = cfg.Render.CoverImage
	}

	// Support Pandoc title.txt metadata if present
	titleTxtPath := filepath.Join(rootDir, "title.txt")
	if data, err := os.ReadFile(titleTxtPath); err == nil {
		var pMeta struct {
			Title      string `yaml:"title"`
			Author     string `yaml:"author"`
			Rights     string `yaml:"rights"`
			CoverImage string `yaml:"cover-image"`
		}
		cleanYML := strings.TrimSpace(string(data))
		cleanYML = strings.TrimPrefix(cleanYML, "---")
		cleanYML = strings.TrimSuffix(cleanYML, "...")
		cleanYML = strings.TrimSuffix(cleanYML, "---")
		if yaml.Unmarshal([]byte(cleanYML), &pMeta) == nil {
			if cfg.Title == "" && pMeta.Title != "" {
				cfg.Title = pMeta.Title
			}
			if cfg.Author == "" && pMeta.Author != "" {
				cfg.Author = pMeta.Author
			}
			if pMeta.Rights != "" {
				cfg.LicenseText = pMeta.Rights
			}
			if cfg.CoverImage == "" && pMeta.CoverImage != "" {
				cfg.CoverImage = pMeta.CoverImage
			}
		}
	}

	// Auto-detect cover image if not explicitly set
	if cfg.CoverImage == "" {
		for _, cand := range []string{"assets/cover.jpg", "assets/cover.png", "cover.jpg", "cover.png", "epub-cover.png", "docs/cover.jpg", "docs/cover.png"} {
			if fileExists(filepath.Join(rootDir, cand)) {
				cfg.CoverImage = cand
				break
			}
		}
	}

	// Parse GitBook / mdBook SUMMARY.md for Part headings if present
	for _, sumCand := range []string{filepath.Join(rootDir, "SUMMARY.md"), filepath.Join(rootDir, "src", "SUMMARY.md")} {
		if data, err := os.ReadFile(sumCand); err == nil && len(cfg.Parts) == 0 {
			enrichFromSummary(cfg, string(data))
			break
		}
	}

	// Enrich metadata from README.md if present
	readmePath := filepath.Join(rootDir, "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		enrichFromReadme(cfg, string(data))
	}

	// Auto-detect Git remote origin URL and default branch if present
	if headData, err := os.ReadFile(filepath.Join(rootDir, ".git", "HEAD")); err == nil {
		headStr := strings.TrimSpace(string(headData))
		if strings.HasPrefix(headStr, "ref: refs/heads/") {
			cfg.DefaultBranch = strings.TrimPrefix(headStr, "ref: refs/heads/")
		}
	}
	if cfg.RepoURL == "" {
		gitCfgPath := filepath.Join(rootDir, ".git", "config")
		if data, err := os.ReadFile(gitCfgPath); err == nil {
			if m := reGitHub.FindString(string(data)); m != "" {
				cfg.RepoURL = strings.TrimSuffix(m, ".git")
			}
		}
	}

	if cfg.Title == "" {
		base := filepath.Base(rootDir)
		cfg.Title = strings.Title(strings.ReplaceAll(base, "-", " "))
	}
	if cfg.Author == "" {
		cfg.Author = "Author"
	}
	if cfg.Output == "" || cfg.Output == "book.pdf" {
		slug := strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(cfg.Title, "-"))
		slug = strings.Trim(slug, "-")
		if slug != "" {
			cfg.Output = slug + ".pdf"
		}
	}

	return cfg, nil
}

var (
	reH1        = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	reSummaryH2 = regexp.MustCompile(`(?m)^##\s+(.+)$`)
	rePartH3    = regexp.MustCompile(`(?m)^###\s+([^\n]+)\n+([^\n#<>\-1-9][^\n]+)`)
	reBullet    = regexp.MustCompile(`(?m)^>\s*-\s+(.+)$`)
	reGitHub    = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`)
)

func enrichFromSummary(cfg *BookConfig, summary string) {
	matches := reSummaryH2.FindAllStringSubmatch(summary, -1)
	for idx, m := range matches {
		pTitle := strings.TrimSpace(m[1])
		cfg.Parts = append(cfg.Parts, PartMeta{
			Index: idx + 1,
			Title: pTitle,
		})
	}
}

func enrichFromReadme(cfg *BookConfig, readme string) {
	if m := reH1.FindStringSubmatch(readme); len(m) > 1 {
		h1 := strings.TrimSpace(m[1])
		// Prefer cleaner H1 title if go-pretty-pdf.yml had a hyphenated suffix
		if cfg.Title == "" || strings.Contains(cfg.Title, " - ") {
			cfg.Title = h1
		}
	}

	if cfg.RepoURL == "" {
		if m := reGitHub.FindString(readme); m != "" && !strings.Contains(m, "go-pretty-pdf") && !strings.Contains(m, "sindresorhus") {
			cfg.RepoURL = m
		}
	}

	if len(cfg.Highlights) == 0 {
		matches := reBullet.FindAllStringSubmatch(readme, 6)
		for _, m := range matches {
			cfg.Highlights = append(cfg.Highlights, strings.TrimSpace(m[1]))
		}
	}

	if len(cfg.Parts) == 0 {
		matches := rePartH3.FindAllStringSubmatch(readme, -1)
		for idx, m := range matches {
			pTitle := strings.TrimSpace(m[1])
			pTitle = regexp.MustCompile(`^Part\s+[0-9IVX]+:\s*`).ReplaceAllString(pTitle, "")
			pDesc := strings.TrimSpace(m[2])
			cfg.Parts = append(cfg.Parts, PartMeta{
				Index:       idx + 1,
				Title:       pTitle,
				Description: pDesc,
			})
		}
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
