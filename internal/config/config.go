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
	Title         string   `yaml:"title"`
	Subtitle      string   `yaml:"subtitle"`
	Author        string   `yaml:"author"`
	Source        string   `yaml:"source"`
	Output        string   `yaml:"output"`
	CoverImage    string   `yaml:"cover_image"`
	RepoURL       string   `yaml:"repo_url"`
	SiteURL       string   `yaml:"site_url"`
	DefaultBranch string   `yaml:"default_branch"`
	Description   string   `yaml:"description"`
	Highlights    []string `yaml:"highlights"`
	AuthorBio     string   `yaml:"author_bio"`
	LicenseText   string   `yaml:"license_text"`
	LicenseCode   string   `yaml:"license_code"`
	NoIndex       bool     `yaml:"no_index"`
	// Fonts selects the typefaces: "open" (default, free fonts, reproducible),
	// "pro" (local Minion Pro / Myriad Pro) or "mono" (Ubuntu Mono throughout).
	Fonts   string `yaml:"fonts"`
	WorkDir string `yaml:"-"`
	// RootDir is the manuscript source tree that relative image paths are
	// resolved against. Empty for sources with no tree on disk (EPUB).
	RootDir string `yaml:"-"`
	// FontsDir is an extra directory searched for fonts (--fonts-dir).
	FontsDir string       `yaml:"-"`
	Render   RenderConfig `yaml:"render"`
	Parts    []PartMeta   `yaml:"parts"`
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

	// Support mdBook book.toml if present
	if data, err := os.ReadFile(filepath.Join(rootDir, "book.toml")); err == nil {
		enrichFromBookToml(cfg, string(data))
	}

	// Support The Carpentries sandpaper config.yaml if present
	if data, err := os.ReadFile(filepath.Join(rootDir, "config.yaml")); err == nil {
		var cCfg struct {
			Title  string `yaml:"title"`
			Source string `yaml:"source"`
			Lang   string `yaml:"lang"`
		}
		if yaml.Unmarshal(data, &cCfg) == nil {
			if cfg.Title == "" && strings.TrimSpace(cCfg.Title) != "" {
				cfg.Title = strings.TrimSpace(cCfg.Title)
			}
			if cfg.RepoURL == "" && strings.HasPrefix(strings.TrimSpace(cCfg.Source), "http") {
				cfg.RepoURL = strings.TrimSpace(cCfg.Source)
			}
		}
	}

	// Support Jupyter Book v2 myst.yml if present
	if data, err := os.ReadFile(filepath.Join(rootDir, "myst.yml")); err == nil {
		var mCfg struct {
			Project struct {
				Title       string `yaml:"title"`
				Description string `yaml:"description"`
				Github      string `yaml:"github"`
				Authors     []struct {
					Name string `yaml:"name"`
				} `yaml:"authors"`
			} `yaml:"project"`
		}
		if yaml.Unmarshal(data, &mCfg) == nil {
			if cfg.Title == "" && mCfg.Project.Title != "" {
				cfg.Title = strings.TrimSpace(mCfg.Project.Title)
			}
			if cfg.Author == "" && len(mCfg.Project.Authors) > 0 {
				var names []string
				for _, a := range mCfg.Project.Authors {
					if strings.TrimSpace(a.Name) != "" {
						names = append(names, strings.TrimSpace(a.Name))
					}
				}
				if len(names) > 0 {
					cfg.Author = strings.Join(names, ", ")
				}
			}
			if cfg.Description == "" && mCfg.Project.Description != "" {
				cfg.Description = strings.TrimSpace(mCfg.Project.Description)
			}
			if cfg.RepoURL == "" && mCfg.Project.Github != "" {
				gh := strings.TrimSpace(mCfg.Project.Github)
				if !strings.HasPrefix(gh, "http") {
					gh = "https://github.com/" + strings.TrimPrefix(gh, "/")
				}
				cfg.RepoURL = gh
			}
		}
	}

	// Support Jupyter Book _config.yml and _toc.yml if present (root or immediate subdirs)
	for _, dirCand := range []string{rootDir, filepath.Join(rootDir, "lectures"), filepath.Join(rootDir, "book"), filepath.Join(rootDir, "docs")} {
		if data, err := os.ReadFile(filepath.Join(dirCand, "_config.yml")); err == nil {
			var jb struct {
				Title       string `yaml:"title"`
				Author      string `yaml:"author"`
				Description string `yaml:"description"`
				Logo        string `yaml:"logo"`
			}
			if yaml.Unmarshal(data, &jb) == nil {
				if cfg.Title == "" && jb.Title != "" {
					cfg.Title = jb.Title
				}
				if cfg.Author == "" && jb.Author != "" {
					cfg.Author = jb.Author
				}
				if cfg.Description == "" && jb.Description != "" {
					cfg.Description = jb.Description
				}
				if relDir, rErr := filepath.Rel(rootDir, dirCand); rErr == nil && relDir != "." {
					cfg.Source = relDir
				}
			}
			if tocData, tErr := os.ReadFile(filepath.Join(dirCand, "_toc.yml")); tErr == nil && len(cfg.Parts) == 0 {
				enrichFromJupyterToc(cfg, string(tocData))
			}
			break
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
	reH1          = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	reSummaryH2   = regexp.MustCompile(`(?m)^##\s+(.+)$`)
	reTomlTitle   = regexp.MustCompile(`(?m)^title\s*=\s*"([^"]+)"`)
	reTomlAuthors = regexp.MustCompile(`(?m)^authors\s*=\s*\[(.*?)\]`)
	reTomlSrc     = regexp.MustCompile(`(?m)^src\s*=\s*"([^"]+)"`)
	reTomlRepo    = regexp.MustCompile(`(?m)^git-repository-url\s*=\s*"([^"]+)"`)
	reQuotedStr   = regexp.MustCompile(`"([^"]+)"`)
	rePartH3      = regexp.MustCompile(`(?m)^###\s+(Part\s+[0-9IVX]+:\s*[^\n]+)\n+([^\n#<>\-1-9][^\n]+)`)
	reBullet      = regexp.MustCompile(`(?m)^>\s*-\s+(.+)$`)
	reGitHub      = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`)
)

func enrichFromBookToml(cfg *BookConfig, toml string) {
	if cfg.Title == "" {
		if m := reTomlTitle.FindStringSubmatch(toml); len(m) > 1 {
			cfg.Title = strings.TrimSpace(m[1])
		}
	}
	if cfg.Author == "" {
		if m := reTomlAuthors.FindStringSubmatch(toml); len(m) > 1 {
			var auths []string
			for _, q := range reQuotedStr.FindAllStringSubmatch(m[1], -1) {
				auths = append(auths, q[1])
			}
			if len(auths) > 0 {
				cfg.Author = strings.Join(auths, ", ")
			}
		}
	}
	if cfg.RepoURL == "" {
		if m := reTomlRepo.FindStringSubmatch(toml); len(m) > 1 {
			cfg.RepoURL = strings.TrimSpace(m[1])
		}
	}
	if m := reTomlSrc.FindStringSubmatch(toml); len(m) > 1 {
		cfg.Source = strings.TrimSpace(m[1])
	} else {
		cfg.Source = "src"
	}
}

func enrichFromJupyterToc(cfg *BookConfig, tocYML string) {
	var parsed struct {
		Parts []struct {
			Caption string `yaml:"caption"`
		} `yaml:"parts"`
	}
	if yaml.Unmarshal([]byte(tocYML), &parsed) != nil {
		return
	}
	for idx, p := range parsed.Parts {
		if strings.TrimSpace(p.Caption) != "" {
			cfg.Parts = append(cfg.Parts, PartMeta{
				Index: idx + 1,
				Title: strings.TrimSpace(p.Caption),
			})
		}
	}
}

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
