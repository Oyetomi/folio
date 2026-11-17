package main

import (
	"flag"
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
	"github.com/Oyetomi/folio/internal/latex"
	"github.com/Oyetomi/folio/internal/markdown"
	"github.com/Oyetomi/folio/internal/verify"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		runBuild(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "build":
		runBuild(os.Args[2:])
	case "init":
		runInit()
	case "check":
		runCheck(os.Args[2:])
	case "verify":
		runVerify(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("folio v%s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		runBuild(os.Args[1:])
	}
}

func printUsage() {
	fmt.Println(`folio - Universal Typesetting & Vector Book Compiler (Markdown / MDX / EPUB / GitHub -> Print PDF)

Usage:
  folio build [source] [flags]    Compile a directory, .epub file, or GitHub URL into a print-ready PDF
  folio init                      Create a starter folio.yml configuration file
  folio check [source]            Validate chapters, frontmatter, and diagrams without compiling TeX
  folio verify <file.pdf>         Run bounding-box overlap & typography audit on a compiled PDF
  folio version                   Print version

Flags for 'folio build':
  -o, --out string        Output PDF path (defaults to config output or <title>.pdf)
  --title string          Override book title
  --subtitle string       Override book subtitle
  --author string         Override book author
  --cover string          Override cover image path
  --fonts-dir string      Custom directory containing .otf/.ttf fonts
  --no-index              Omit the alphabetical back-of-book index (ideal for fiction/essays)
  --keep-tex              Save the generated .tex source file next to the output PDF
  --verify                Run post-compile bounding-box overlap audit (default true)`)
}

func runInit() {
	const sampleYML = `title: "Book Title"
subtitle: "A Comprehensive Guide"
author: "Author Name"
source: docs
output: book.pdf
cover_image: assets/cover.jpg
description: "Write a 1-2 paragraph overview of your book for the inside front cover spread."
highlights:
  - "First key takeaway or topic covered in this book"
  - "Second key takeaway or topic covered in this book"
  - "Third key takeaway or topic covered in this book"
author_bio: "**Author Name** is a writer, researcher, and practitioner."
license_text: "Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0)."
license_code: "MIT License (MIT)."
no_index: false
`
	if _, err := os.Stat("folio.yml"); err == nil {
		fmt.Fprintln(os.Stderr, "folio.yml already exists in current directory.")
		os.Exit(1)
	}
	if err := os.WriteFile("folio.yml", []byte(sampleYML), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing folio.yml: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Created folio.yml")
}

func runCheck(args []string) {
	src := "."
	if len(args) > 0 {
		src = args[0]
	}
	cfg, err := config.LoadOrDiscover(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	docs, err := markdown.DiscoverChapters(src, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Checked %d chapters in %s (Title: %q, Author: %q)\n", len(docs), src, cfg.Title, cfg.Author)
}

func runVerify(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: folio verify <file.pdf>")
		os.Exit(1)
	}
	rep, err := verify.AuditPDF(args[0], "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Verified %s: %d pages, %d figures checked, %d word overlaps.\n", args[0], rep.Pages, rep.FiguresChecked, len(rep.Overlaps))
	for _, ov := range rep.Overlaps {
		fmt.Printf("  OVERLAP on p.%d (%s): %q vs %q (%.1fpt x %.1fpt)\n", ov.Page, ov.Figure, ov.WordA, ov.WordB, ov.Width, ov.Height)
	}
	if len(rep.Overlaps) > 0 {
		os.Exit(1)
	}
}

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	var (
		outFlag      string
		titleFlag    string
		subtitleFlag string
		authorFlag   string
		coverFlag    string
		fontsDirFlag string
		noIndexFlag  bool
		keepTexFlag  bool
		verifyFlag   bool
	)
	fs.StringVar(&outFlag, "o", "", "Output PDF path")
	fs.StringVar(&outFlag, "out", "", "Output PDF path")
	fs.StringVar(&titleFlag, "title", "", "Override book title")
	fs.StringVar(&subtitleFlag, "subtitle", "", "Override book subtitle")
	fs.StringVar(&authorFlag, "author", "", "Override book author")
	fs.StringVar(&coverFlag, "cover", "", "Override cover image path")
	fs.StringVar(&fontsDirFlag, "fonts-dir", "", "Custom fonts directory")
	fs.BoolVar(&noIndexFlag, "no-index", false, "Disable back-of-book index")
	fs.BoolVar(&keepTexFlag, "keep-tex", false, "Save generated .tex file next to output PDF")
	fs.BoolVar(&verifyFlag, "verify", true, "Run post-compile bounding-box overlap verification")

	// Allow positional [source] before or after flags
	var posArgs []string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			if (a == "-o" || a == "--out" || a == "--title" || a == "--subtitle" || a == "--author" || a == "--cover" || a == "--fonts-dir") && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else {
			posArgs = append(posArgs, a)
		}
	}
	_ = fs.Parse(flagArgs)

	source := "."
	if len(posArgs) > 0 {
		source = posArgs[0]
	}

	start := time.Now()
	workDir, err := os.MkdirTemp("", "folio-build-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp workspace: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(workDir)

	// 1. Handle remote GitHub URL if passed
	rootDir := source
	if strings.HasPrefix(source, "https://github.com/") || strings.HasPrefix(source, "http://github.com/") || strings.HasPrefix(source, "github.com/") {
		repoURL := source
		if strings.HasPrefix(repoURL, "github.com/") {
			repoURL = "https://" + repoURL
		}
		cloneDir := filepath.Join(workDir, "repo")
		fmt.Printf("Cloning %s ...\n", repoURL)
		cmd := exec.Command("git", "clone", "--depth", "1", repoURL, cloneDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "git clone failed: %v\n", err)
			os.Exit(1)
		}
		rootDir = cloneDir
	}

	// 2. Stage fonts into workDir/fonts
	hasProFonts, err := assets.StageFonts(workDir, fontsDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "font staging failed: %v\n", err)
		os.Exit(1)
	}

	var cfg *config.BookConfig
	var docs []markdown.ChapterDoc

	// 3. Load input source (.epub vs directory)
	if strings.HasSuffix(strings.ToLower(rootDir), ".epub") {
		fmt.Printf("Extracting EPUB %s ...\n", rootDir)
		cfg, docs, err = epub.ExtractEPUB(rootDir, workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "EPUB extraction failed: %v\n", err)
			os.Exit(1)
		}
		base := strings.TrimSuffix(filepath.Base(rootDir), filepath.Ext(rootDir))
		cfg.Output = base + ".pdf"
		rootDir = filepath.Dir(rootDir)
	} else {
		cfg, err = config.LoadOrDiscover(rootDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
			os.Exit(1)
		}
		docs, err = markdown.DiscoverChapters(rootDir, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chapter discovery failed: %v\n", err)
			os.Exit(1)
		}
	}

	if len(docs) == 0 {
		fmt.Fprintf(os.Stderr, "no chapters found in %s\n", source)
		os.Exit(1)
	}

	// Apply CLI flag overrides
	if titleFlag != "" {
		cfg.Title = titleFlag
	}
	if subtitleFlag != "" {
		cfg.Subtitle = subtitleFlag
	}
	if authorFlag != "" {
		cfg.Author = authorFlag
	}
	if coverFlag != "" {
		cfg.CoverImage = coverFlag
	}
	if noIndexFlag {
		cfg.NoIndex = true
	}

	outPath := outFlag
	if outPath == "" {
		outPath = cfg.Output
	}
	if !filepath.IsAbs(outPath) {
		cwd, _ := os.Getwd()
		outPath = filepath.Join(cwd, outPath)
	}

	// 4. Generate XeLaTeX document
	texContent, stats, err := latex.BuildTexDocument(rootDir, workDir, cfg, docs, hasProFonts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "LaTeX generation failed: %v\n", err)
		os.Exit(1)
	}

	texPath := filepath.Join(workDir, "book.tex")
	if err := os.WriteFile(texPath, []byte(texContent), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write book.tex: %v\n", err)
		os.Exit(1)
	}
	if keepTexFlag {
		keepPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".tex"
		_ = os.WriteFile(keepPath, []byte(texContent), 0o644)
	}

	fmt.Printf("Generated LaTeX (%d chapters, %d vector figures, %.1f KB). Compiling PDF with Tectonic...\n",
		stats.Chapters, stats.Figures, float64(stats.TexBytes)/1024.0)

	// 5. Compile with Tectonic
	tectonicBin, err := exec.LookPath("tectonic")
	if err != nil {
		for _, cand := range []string{"/opt/homebrew/bin/tectonic", "/usr/local/bin/tectonic"} {
			if _, stErr := os.Stat(cand); stErr == nil {
				tectonicBin = cand
				break
			}
		}
	}
	if tectonicBin == "" {
		fmt.Fprintln(os.Stderr, "tectonic compiler not found in PATH (install via 'brew install tectonic' or https://tectonic-typesetting.github.io/)")
		os.Exit(1)
	}

	cmd := exec.Command(tectonicBin, "--keep-logs", "book.tex")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "tectonic compilation failed:\n%s\n", string(out))
		os.Exit(1)
	}

	compiledPDF := filepath.Join(workDir, "book.pdf")
	if err := copyFile(compiledPDF, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output PDF to %s: %v\n", outPath, err)
		os.Exit(1)
	}

	// 6. Run Post-Compile Verification Gate
	if verifyFlag {
		logPath := filepath.Join(workDir, "book.log")
		rep, err := verify.AuditPDF(outPath, logPath)
		if err == nil && rep.Pages > 0 {
			fmt.Printf("Verification Gate: %d pages, %d figures audited, %d word overlaps, %d overfull boxes.\n",
				rep.Pages, rep.FiguresChecked, len(rep.Overlaps), rep.OverfullHbox)
		}
	}

	info, _ := os.Stat(outPath)
	sizeMB := float64(info.Size()) / (1024.0 * 1024.0)
	fmt.Printf("Built %s (%.2f MB) in %s\n", outPath, sizeMB, time.Since(start).Round(100*time.Millisecond))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
