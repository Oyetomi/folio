package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Oyetomi/folio/internal/build"
	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/markdown"
	"github.com/Oyetomi/folio/internal/server"
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
	case "serve", "ui":
		runServe(os.Args[2:])
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
  folio serve [--addr host:port]  Open the Folio desk: a local web UI for building books
  folio version                   Print version

Flags for 'folio build':
  -o, --out string        Output PDF path (defaults to config output or <title>.pdf)
  --title string          Override book title
  --subtitle string       Override book subtitle
  --author string         Override book author
  --cover string          Override cover image path
  --fonts string          Typefaces: open (free, reproducible; default), pro (local Minion/Myriad), mono
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
fonts: open   # open (free, reproducible), pro (local Minion/Myriad) or mono
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
	if abs, err := filepath.Abs(src); err == nil {
		src = abs
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
		fontsFlag    string
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
	fs.StringVar(&fontsFlag, "fonts", "", "Typefaces: open (default), pro or mono")
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
			if (a == "-o" || a == "--out" || a == "--title" || a == "--subtitle" || a == "--author" || a == "--cover" || a == "--fonts-dir" || a == "--fonts") && i+1 < len(args) {
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

	_, err := build.Build(context.Background(), build.Options{
		Source:   source,
		Out:      outFlag,
		Title:    titleFlag,
		Subtitle: subtitleFlag,
		Author:   authorFlag,
		Cover:    coverFlag,
		FontsDir: fontsDirFlag,
		Fonts:    fontsFlag,
		NoIndex:  noIndexFlag,
		KeepTex:  keepTexFlag,
		Verify:   verifyFlag,
		Log:      os.Stdout,
		FetchLog: os.Stderr, // git clone progress
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7350", "Address to listen on (keep it on localhost)")
	noOpen := fs.Bool("no-open", false, "Do not open the browser automatically")
	_ = fs.Parse(args)
	if err := server.Run(context.Background(), server.Config{Addr: *addr, OpenBrowser: !*noOpen, Version: version}); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
