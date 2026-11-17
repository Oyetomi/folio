// Package build runs the full Folio pipeline (fetch → discover → typeset →
// compile → verify). It is shared by the `folio build` CLI and the
// `folio serve` web UI, and reports errors instead of exiting.
package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oyetomi/folio/internal/assets"
	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/epub"
	"github.com/Oyetomi/folio/internal/highlight"
	"github.com/Oyetomi/folio/internal/latex"
	"github.com/Oyetomi/folio/internal/markdown"
	"github.com/Oyetomi/folio/internal/verify"
)

// Stage names a step of the pipeline, reported through Options.OnStage.
type Stage string

const (
	StageFetch    Stage = "fetch"
	StageDiscover Stage = "discover"
	StageTypeset  Stage = "typeset"
	StageCompile  Stage = "compile"
	StageVerify   Stage = "verify"
)

// Options configures one build.
type Options struct {
	Source   string // directory, .epub file, or GitHub/GitBook URL
	Out      string // output PDF path; empty means the book's configured output, relative to the CWD
	Title    string // metadata overrides (empty = keep discovered value)
	Subtitle string
	Author   string
	Cover    string
	FontsDir string
	NoIndex  bool
	Fonts    string // "open", "pro" or "mono"; empty = folio.yml, then $FOLIO_FONTS, then "open"
	KeepTex  bool   // also write <out>.tex
	Verify   bool   // run the post-compile audit

	// Log receives human-readable progress lines. Nil discards them.
	Log io.Writer
	// FetchLog, if set, receives `git clone` output as it happens.
	FetchLog io.Writer
	// CompilerLog, if set, receives Tectonic output as it happens. It is
	// always included in the error when compilation fails.
	CompilerLog io.Writer
	// OnStage is called as each stage begins.
	OnStage func(Stage)
}

// Result describes a finished build.
type Result struct {
	OutPath  string
	TexPath  string // set when KeepTex
	Title    string
	Subtitle string
	Author   string
	Chapters int
	Figures  int
	TexBytes int
	Size     int64
	Duration time.Duration
	Fonts    string // font mode actually used

	Verified       bool // false when the audit was skipped or pdftotext is missing
	Pages          int
	FiguresAudited int
	Overlaps       []verify.OverlapIssue
	OverfullHbox   int

	// MissingGlyphs are characters no font could render; they are absent
	// from the PDF. Always checked, independent of Verify.
	MissingGlyphs []rune
}

// Manuscript is a loaded, not-yet-typeset book.
type Manuscript struct {
	Config  *config.BookConfig
	Docs    []markdown.ChapterDoc
	RootDir string // tree images are resolved against (workDir for EPUBs)
	IsEPUB  bool
}

// Build runs the whole pipeline.
func Build(ctx context.Context, opts Options) (*Result, error) {
	start := time.Now()
	logf := func(format string, a ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", a...)
		}
	}
	stage := func(s Stage) {
		if opts.OnStage != nil {
			opts.OnStage(s)
		}
	}

	workDir, err := os.MkdirTemp("", "folio-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 1. Fetch remote sources.
	source := opts.Source
	if source == "" {
		source = "."
	}
	if url, ok := RemoteURL(source); ok {
		stage(StageFetch)
		logf("Cloning %s ...", url)
		cloneDir := filepath.Join(workDir, "repo")
		if err := Clone(ctx, url, cloneDir, opts.FetchLog); err != nil {
			return nil, err
		}
		source = cloneDir
	}

	// 2. Discover chapters.
	stage(StageDiscover)
	hasProFonts, err := assets.StageFonts(workDir, opts.FontsDir)
	if err != nil {
		return nil, fmt.Errorf("font staging failed: %w", err)
	}
	if IsEPUB(source) {
		logf("Extracting EPUB %s ...", source)
	}
	ms, err := Load(source, workDir)
	if err != nil {
		return nil, err
	}
	cfg := ms.Config
	if opts.Title != "" {
		cfg.Title = opts.Title
	}
	if opts.Subtitle != "" {
		cfg.Subtitle = opts.Subtitle
	}
	if opts.Author != "" {
		cfg.Author = opts.Author
	}
	if opts.Cover != "" {
		cover := opts.Cover
		if ms.IsEPUB && !filepath.IsAbs(cover) {
			cover = filepath.Join(filepath.Dir(source), cover)
		}
		cfg.CoverImage = cover
	}
	if opts.NoIndex {
		cfg.NoIndex = true
	}
	cfg.FontsDir = opts.FontsDir

	fontMode, err := latex.ParseFontMode(firstSet(opts.Fonts, cfg.Fonts, os.Getenv("FOLIO_FONTS")))
	if err != nil {
		return nil, err
	}
	if fontMode == latex.FontsPro && !hasProFonts {
		return nil, errors.New("fonts: pro needs MinionPro-Regular.otf and MyriadPro-Regular.otf in ~/.folio/fonts, $FOLIO_FONTS_DIR or --fonts-dir; use fonts: open for the free equivalents")
	}

	outPath := opts.Out
	if outPath == "" {
		outPath = cfg.Output
	}
	if !filepath.IsAbs(outPath) {
		cwd, _ := os.Getwd()
		outPath = filepath.Join(cwd, outPath)
	}

	// 3. Generate LaTeX.
	stage(StageTypeset)
	texContent, stats, err := latex.BuildTexDocument(ms.RootDir, workDir, cfg, ms.Docs, fontMode)
	if err != nil {
		return nil, fmt.Errorf("LaTeX generation failed: %w", err)
	}
	texPath := filepath.Join(workDir, "book.tex")
	if err := os.WriteFile(texPath, []byte(texContent), 0o644); err != nil {
		return nil, fmt.Errorf("write book.tex: %w", err)
	}
	res := &Result{
		OutPath:  outPath,
		Title:    cfg.Title,
		Subtitle: cfg.Subtitle,
		Author:   cfg.Author,
		Chapters: stats.Chapters,
		Figures:  stats.Figures,
		TexBytes: stats.TexBytes,
		Fonts:    string(fontMode),
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	if opts.KeepTex {
		res.TexPath = strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".tex"
		if err := os.WriteFile(res.TexPath, []byte(texContent), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", res.TexPath, err)
		}
	}
	for _, note := range stats.Notes {
		logf("%s", note)
	}
	logf("Fonts: %s", fontDescription[fontMode])
	logf("Generated LaTeX (%d chapters, %d vector figures, %.1f KB). Compiling PDF with Tectonic...",
		stats.Chapters, stats.Figures, float64(stats.TexBytes)/1024.0)

	// 4. Compile.
	stage(StageCompile)
	tectonic, err := FindTectonic()
	if err != nil {
		return nil, err
	}
	var captured bytes.Buffer
	var sink io.Writer = &captured
	if opts.CompilerLog != nil {
		sink = io.MultiWriter(&captured, opts.CompilerLog)
	}
	cmd := exec.CommandContext(ctx, tectonic, "--keep-logs", "book.tex")
	cmd.Dir = workDir
	cmd.Stdout = sink
	cmd.Stderr = sink
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("tectonic compilation failed:\n%s", captured.String())
	}
	if err := copyFile(filepath.Join(workDir, "book.pdf"), outPath); err != nil {
		return nil, fmt.Errorf("write output PDF to %s: %w", outPath, err)
	}

	// Characters lost either way: stripped before TeX (no font at all) or
	// reported missing by XeTeX (font lacked the glyph).
	res.MissingGlyphs = mergeRunes(droppedInManuscript(ms), verify.MissingGlyphs(filepath.Join(workDir, "book.log")))
	if n := len(res.MissingGlyphs); n > 0 {
		logf("Warning: %d characters have no glyph in any available font and are missing from the PDF: %s",
			n, glyphSample(res.MissingGlyphs, 24))
	}

	// 5. Verify.
	if opts.Verify {
		stage(StageVerify)
		rep, err := verify.AuditPDF(outPath, filepath.Join(workDir, "book.log"))
		if err == nil && rep.Pages > 0 {
			res.Verified = true
			res.Pages = rep.Pages
			res.FiguresAudited = rep.FiguresChecked
			res.Overlaps = rep.Overlaps
			res.OverfullHbox = rep.OverfullHbox
			logf("Verification Gate: %d pages, %d figures audited, %d word overlaps, %d overfull boxes.",
				rep.Pages, rep.FiguresChecked, len(rep.Overlaps), rep.OverfullHbox)
		}
	}

	if info, err := os.Stat(outPath); err == nil {
		res.Size = info.Size()
	}
	res.Duration = time.Since(start)
	logf("Built %s (%.2f MB) in %s", outPath, float64(res.Size)/(1024.0*1024.0), res.Duration.Round(100*time.Millisecond))
	return res, nil
}

// Load reads a local directory or .epub into a Manuscript. workDir receives
// extracted EPUB contents and must outlive the returned Manuscript's use.
func Load(source, workDir string) (*Manuscript, error) {
	// Resolve "." and other relative paths so titles and discovery see a
	// real directory name.
	if abs, err := filepath.Abs(source); err == nil {
		source = abs
	}
	if IsEPUB(source) {
		cfg, docs, err := epub.ExtractEPUB(source, workDir)
		if err != nil {
			return nil, fmt.Errorf("EPUB extraction failed: %w", err)
		}
		base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		cfg.Output = base + ".pdf"
		if len(docs) == 0 {
			return nil, fmt.Errorf("no chapters found in %s", source)
		}
		// An EPUB has no source tree on disk: everything it needs was
		// extracted into workDir. Never scan the EPUB's parent directory.
		return &Manuscript{Config: cfg, Docs: docs, RootDir: workDir, IsEPUB: true}, nil
	}
	st, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("source not found: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory or .epub file", source)
	}
	cfg, err := config.LoadOrDiscover(source)
	if err != nil {
		return nil, fmt.Errorf("config load failed: %w", err)
	}
	docs, err := markdown.DiscoverChapters(source, cfg)
	if err != nil {
		return nil, fmt.Errorf("chapter discovery failed: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no chapters found in %s", source)
	}
	return &Manuscript{Config: cfg, Docs: docs, RootDir: source}, nil
}

// IsEPUB reports whether source names an .epub file.
func IsEPUB(source string) bool {
	return strings.HasSuffix(strings.ToLower(source), ".epub")
}

// RemoteURL maps a GitHub or GitBook source to a clonable GitHub URL.
func RemoteURL(source string) (string, bool) {
	s := strings.TrimSpace(source)
	if strings.Contains(s, ".gitbook.io/") {
		// https://<owner>.gitbook.io/<repo> -> https://github.com/<owner>/<repo>
		trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		parts := strings.SplitN(trimmed, ".gitbook.io/", 2)
		if len(parts) == 2 {
			owner := parts[0]
			slug := strings.Trim(strings.Split(parts[1], "/")[0], "/")
			if owner != "" && slug != "" {
				s = fmt.Sprintf("https://github.com/%s/%s", owner, slug)
			}
		}
	}
	switch {
	case strings.HasPrefix(s, "https://github.com/"), strings.HasPrefix(s, "http://github.com/"):
		return s, true
	case strings.HasPrefix(s, "github.com/"):
		return "https://" + s, true
	}
	return "", false
}

// Clone shallow-clones a GitHub repository into dir.
func Clone(ctx context.Context, url, dir string, out io.Writer) error {
	if !strings.HasPrefix(url, "https://github.com/") && !strings.HasPrefix(url, "http://github.com/") {
		return fmt.Errorf("only github.com repositories are supported: %s", url)
	}
	var captured bytes.Buffer
	var sink io.Writer = &captured
	if out != nil {
		sink = io.MultiWriter(&captured, out)
	}
	// "--" stops a crafted URL from being read as a git option.
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--", url, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = sink
	cmd.Stderr = sink
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("git clone failed: %v\n%s", err, strings.TrimSpace(captured.String()))
	}
	return nil
}

// ErrNoTectonic is returned when the Tectonic compiler cannot be found.
var ErrNoTectonic = errors.New("tectonic compiler not found in PATH (install via 'brew install tectonic' or https://tectonic-typesetting.github.io/)")

// FindTectonic locates the tectonic binary.
func FindTectonic() (string, error) {
	if p, err := exec.LookPath("tectonic"); err == nil {
		return p, nil
	}
	for _, cand := range []string{"/opt/homebrew/bin/tectonic", "/usr/local/bin/tectonic"} {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", ErrNoTectonic
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// glyphSample renders up to n characters with their code points.
func glyphSample(rs []rune, n int) string {
	var parts []string
	for i, r := range rs {
		if i == n {
			parts = append(parts, fmt.Sprintf("… (+%d more)", len(rs)-n))
			break
		}
		parts = append(parts, fmt.Sprintf("%c U+%04X", r, r))
	}
	return strings.Join(parts, ", ")
}

func droppedInManuscript(ms *Manuscript) []rune {
	var all []rune
	for _, d := range ms.Docs {
		all = mergeRunes(all, highlight.DroppedRunes(d.RawText))
	}
	return all
}

func mergeRunes(a, b []rune) []rune {
	seen := make(map[rune]bool, len(a)+len(b))
	out := make([]rune, 0, len(a)+len(b))
	for _, list := range [][]rune{a, b} {
		for _, r := range list {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

var fontDescription = map[latex.FontMode]string{
	latex.FontsOpen: "open (Crimson Pro, Source Sans Pro, IBM Plex Sans Condensed, Ubuntu Mono)",
	latex.FontsPro:  "pro (Minion Pro, Myriad Pro, Ubuntu Mono)",
	latex.FontsMono: "mono (Ubuntu Mono)",
}

func firstSet(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
