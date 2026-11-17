package latex

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/highlight"
	"github.com/Oyetomi/folio/internal/markdown"
	"github.com/Oyetomi/folio/internal/tikz"
)

type BuildStats struct {
	Chapters int
	Figures  int
	TexBytes int
	Notes    []string // fallback-font decisions and warnings, for the build log
}

func BuildTexDocument(rootDir string, workDir string, cfg *config.BookConfig, docs []markdown.ChapterDoc, fonts FontMode) (string, BuildStats, error) {
	var stats BuildStats
	var parts []string

	parts = append(parts, buildPreamble(fonts))
	parts = append(parts, `\begin{document}`)

	cfg.WorkDir = workDir

	// Chapter images are resolved and staged lazily, relative to the chapter
	// that references them (see markdown.resolveAndStageImage).
	cfg.RootDir = rootDir
	if rootDir == workDir {
		cfg.RootDir = ""
	}

	coverFile := ""
	if cfg.CoverImage != "" {
		cand := cfg.CoverImage
		if !filepath.IsAbs(cand) {
			cand = filepath.Join(rootDir, cand)
		}
		if data, err := os.ReadFile(cand); err == nil {
			ext := filepath.Ext(cand)
			if ext == "" {
				ext = ".jpg"
			}
			coverFile = "cover_art" + ext
			_ = os.WriteFile(filepath.Join(workDir, coverFile), data, 0o644)
		}
	}

	titleEsc := markdown.FormatInline(cfg.Title, cfg)
	subEsc := markdown.FormatInline(cfg.Subtitle, cfg)
	authorEsc := markdown.FormatInline(cfg.Author, cfg)

	// Page 1: Front Cover
	if coverFile != "" {
		parts = append(parts, fmt.Sprintf(`
\thispagestyle{empty}
\begin{tikzpicture}[remember picture,overlay]
  \fill[white] (current page.south west) rectangle (current page.north east);
  \node[anchor=center,inner sep=0pt] at (current page.center) {%%
    \includegraphics[width=\paperwidth,height=\paperheight,keepaspectratio]{%s}%%
  };
\end{tikzpicture}
\clearpage
`, coverFile))
	} else {
		parts = append(parts, fmt.Sprintf(`
\thispagestyle{empty}
\begin{tikzpicture}[remember picture,overlay]
  \fill[white] (current page.south west) rectangle (current page.north east);
  \fill[brandcrimson] (current page.north west) rectangle ([yshift=-0.28in]current page.north east);
  \node[anchor=north west, inner sep=0pt, align=left] at ([xshift=0.68in, yshift=-1.45in]current page.north west) {%%
    \begin{minipage}{5.6in}
      \raggedright
      {\sansbold\bfseries\fontsize{36pt}{42pt}\selectfont\textcolor{bodyblack}{%s}\par}
      \vspace{14pt}
      {\sanscond\fontsize{16pt}{20pt}\selectfont\textcolor{brandcrimson}{%s}\par}
    \end{minipage}%%
  };
  \node[anchor=south east, inner sep=0pt] at ([xshift=-0.68in, yshift=0.65in]current page.south east) {%%
    {\sansbold\bfseries\fontsize{14pt}{18pt}\selectfont\textcolor{bodyblack}{%s}}%%
  };
\end{tikzpicture}
\clearpage
`, titleEsc, subEsc, authorEsc))
	}

	// Page 2: Inside Overview Spread
	desc := cfg.Description
	if desc == "" {
		if cfg.Subtitle != "" {
			desc = fmt.Sprintf("**%s** — %s.", cfg.Title, cfg.Subtitle)
		} else {
			desc = fmt.Sprintf("**%s**, written by %s.", cfg.Title, cfg.Author)
		}
	}
	descEsc := markdown.FormatInline(desc, cfg)

	var hlItems []string
	for _, h := range cfg.Highlights {
		hlItems = append(hlItems, "        \\item "+markdown.FormatInline(h, cfg))
	}
	hlBlock := ""
	if len(hlItems) > 0 {
		hlBlock = "\\vspace{4pt}\n      \\begin{itemize}[leftmargin=1.1em, itemsep=4.5pt, topsep=3pt, label={\\textcolor{brandcrimson}{\\large$\\bullet$}}]\n        \\raggedright\n" + strings.Join(hlItems, "\n") + "\n      \\end{itemize}"
	}

	bio := cfg.AuthorBio
	if bio == "" {
		bio = fmt.Sprintf("**%s** is the author of *%s*.", cfg.Author, cfg.Title)
	}
	bioEsc := markdown.FormatInline(bio, cfg)

	parts = append(parts, fmt.Sprintf(`
\thispagestyle{empty}
\begin{tikzpicture}[remember picture,overlay]
  \fill[tableheadbg] (current page.north west) rectangle ([yshift=-0.16in]current page.north east);
  \fill[tableheadbg] (current page.south west) rectangle ([yshift=0.16in]current page.south east);

  \draw[black!18, line width=0.6pt] ([xshift=4.32in, yshift=-1.28in]current page.north west) -- ([xshift=4.32in, yshift=1.20in]current page.south west);

  \node[anchor=north west, inner sep=0pt] at ([xshift=0.58in, yshift=-0.65in]current page.north west) {%%
    {\sansbold\bfseries\fontsize{21pt}{24pt}\selectfont\textcolor{bodyblack}{%s}}%%
  };

  \node[anchor=north west, inner sep=0pt] at ([xshift=0.58in, yshift=-1.22in]current page.north west) {%%
    \begin{minipage}[t]{3.48in}
      \sffamily\fontsize{9.2pt}{13.0pt}\selectfont\color{bodyblack}
      \setlength{\parskip}{7.5pt}
      \raggedright
      %s
      %s
    \end{minipage}%%
  };

  \node[anchor=north west, inner sep=0pt] at ([xshift=4.52in, yshift=-1.22in]current page.north west) {%%
    \begin{minipage}[t]{1.92in}
      \sffamily\fontsize{8.8pt}{12.4pt}\selectfont\color{bodyblack}
      \setlength{\parskip}{8.5pt}
      \raggedright
      %s
    \end{minipage}%%
  };
\end{tikzpicture}
\clearpage
`, titleEsc, descEsc, hlBlock, bioEsc))

	// Page 3 & 4: Interior Title Page & Copyright Page
	repoLine := ""
	if cfg.RepoURL != "" {
		safeR := strings.ReplaceAll(cfg.RepoURL, "_", `\_`)
		repoLine = fmt.Sprintf(`\noindent\textbf{Repository:} \href{%s}{\inlinecode{%s}}\par`, safeR, highlight.CodeEscapeInline(cfg.RepoURL))
	}

	parts = append(parts, fmt.Sprintf(`
\frontmatter
\thispagestyle{empty}
\vspace*{1.1in}
{\sansbold\bfseries\fontsize{26pt}{31pt}\selectfont %s\par}
\vspace{0.16in}
{\sanscond\fontsize{14pt}{18pt}\selectfont\textcolor{brandcrimson}{%s}\par}
\vfill
{\sansbold\bfseries\fontsize{13pt}{16pt}\selectfont %s\par}
\vspace{0.4in}
\clearpage

\thispagestyle{empty}
\vspace*{0.4in}
{\sansbold\bfseries\fontsize{11pt}{14pt}\selectfont %s\par}
\noindent by %s\par
\vspace{8pt}
\noindent Copyright \copyright\ %s.\par
\vspace{8pt}
\noindent\textbf{Text License:} %s\par
\noindent\textbf{Code License:} %s\par
\vspace{8pt}
%s
\clearpage
`, titleEsc, subEsc, authorEsc, titleEsc, authorEsc, authorEsc, markdown.FormatInline(cfg.LicenseText, cfg), markdown.FormatInline(cfg.LicenseCode, cfg), repoLine))

	// Table of Contents
	parts = append(parts, `
\renewcommand{\currentparttitle}{Table of Contents}
\renewcommand{\currentchaptitle}{Table of Contents}
\setcounter{tocdepth}{0}
{\sansbold\bfseries\fontsize{22pt}{26pt}\selectfont\textcolor{brandcrimson}{Table of Contents}\par}
\vspace{6pt}
\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.8pt}}\par
\vspace{6pt}
\makeatletter
\@starttoc{toc}
\makeatother
\clearpage
`)

	partMap := make(map[int]config.PartMeta)
	for _, pm := range cfg.Parts {
		partMap[pm.Index] = pm
	}

	// Index accumulator: Category -> Subterm -> []chapterLabel
	indexDirect := make(map[string][]string)
	indexSubs := make(map[string]map[string][]string)

	inMainMatter := false
	currentPart := -1
	chCounter := 0
	romanNumerals := []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII", "XIII", "XIV", "XV", "XVI", "XVII", "XVIII", "XIX", "XX"}

	for _, doc := range docs {
		for imgName, imgBytes := range doc.InlineImages {
			_ = os.WriteFile(filepath.Join(workDir, imgName), imgBytes, 0o644)
		}
		if doc.PartIdx == 0 && !inMainMatter {
			bodyTex, fCnt := markdown.CompileChapterFile(doc.RawText, 0, cfg, doc.FilePath)
			stats.Figures += fCnt
			tEsc := markdown.FormatInline(doc.Title, cfg)
			parts = append(parts, fmt.Sprintf(`
\clearpage
\phantomsection
\renewcommand{\currentparttitle}{Front Matter}
\renewcommand{\currentchaptitle}{%s}
\addcontentsline{toc}{chapter}{%s}
{\sansbold\bfseries\fontsize{21pt}{25pt}\selectfont\textcolor{bodyblack}{%s}\par}
\vspace{4pt}
\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.6pt}}\par
\vspace{8pt}
%s
`, tEsc, tEsc, tEsc, bodyTex))
			continue
		}

		if doc.PartIdx >= 999 {
			bodyTex, fCnt := markdown.CompileChapterFile(doc.RawText, 0, cfg, doc.FilePath)
			stats.Figures += fCnt
			tEsc := markdown.FormatInline(doc.Title, cfg)
			parts = append(parts, fmt.Sprintf(`
\clearpage
\phantomsection
\renewcommand{\currentparttitle}{Back Matter}
\renewcommand{\currentchaptitle}{%s}
\addcontentsline{toc}{chapter}{%s}
{\sansbold\bfseries\fontsize{21pt}{25pt}\selectfont\textcolor{bodyblack}{%s}\par}
\vspace{4pt}
\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.6pt}}\par
\vspace{8pt}
%s
`, tEsc, tEsc, tEsc, bodyTex))
			continue
		}

		if !inMainMatter {
			parts = append(parts, `\mainmatter`, fmt.Sprintf(`\renewcommand{\currentparttitle}{%s}`, titleEsc))
			inMainMatter = true
		}

		if doc.PartIdx != currentPart && len(cfg.Parts) > 0 {
			currentPart = doc.PartIdx
			pRoman := fmt.Sprintf("%d", currentPart)
			if currentPart >= 1 && currentPart <= len(romanNumerals) {
				pRoman = romanNumerals[currentPart-1]
			}
			pm, ok := partMap[currentPart]
			if !ok {
				pm = config.PartMeta{Title: fmt.Sprintf("Part %s", pRoman)}
			}
			// The builder supplies the "Part <roman>" label itself, so drop it
			// from titles written as "Part I: Foundations" (or the bare
			// fallback "Part I") to avoid "Part I: Part I: Foundations".
			shortTitle := strings.TrimSpace(rePartPrefix.ReplaceAllString(pm.Title, ""))
			pTitleEsc := markdown.FormatInline(shortTitle, cfg)
			pDescEsc := markdown.FormatInline(pm.Description, cfg)
			footTitle, tocTitle, headTitle := "Part "+pRoman, "Part "+pRoman+".", "Part "+pRoman
			if pTitleEsc != "" {
				footTitle += ": " + pTitleEsc
				tocTitle += " " + pTitleEsc
				headTitle = pTitleEsc
			}
			parts = append(parts, fmt.Sprintf(`
\clearpage
\thispagestyle{empty}
\phantomsection
\renewcommand{\currentparttitle}{%s}
\addcontentsline{toc}{part}{%s}
\vspace*{1.1in}
{\sanscond\fontsize{14pt}{18pt}\selectfont\textcolor{brandcrimson}{PART %s}\par}
\vspace{6pt}
{\sansbold\bfseries\fontsize{24pt}{29pt}\selectfont\textcolor{bodyblack}{%s}\par}
\vspace{8pt}
\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.9pt}}\par
\vspace{14pt}
{\fontsize{10.8pt}{15pt}\selectfont\textcolor{diaggray}{%s}\par}
\clearpage
`, footTitle, tocTitle, pRoman, headTitle, pDescEsc))
		}

		chCounter++
		chLabel := fmt.Sprintf("ch:%d", chCounter)
		bodyTex, fCnt := markdown.CompileChapterFile(doc.RawText, chCounter, cfg, doc.FilePath)
		stats.Figures += fCnt
		tEsc := markdown.FormatInline(doc.Title, cfg)

		for _, hit := range markdown.ExtractChapterIndexHits(doc.RawText) {
			if hit.Subterm == "" {
				indexDirect[hit.Category] = appendUnique(indexDirect[hit.Category], chLabel)
			} else {
				if indexSubs[hit.Category] == nil {
					indexSubs[hit.Category] = make(map[string][]string)
				}
				indexSubs[hit.Category][hit.Subterm] = appendUnique(indexSubs[hit.Category][hit.Subterm], chLabel)
			}
		}

		parts = append(parts, fmt.Sprintf(`
\clearpage
\phantomsection
\label{%s}
\renewcommand{\currentchaptitle}{Chapter %d: %s}
\addcontentsline{toc}{chapter}{\protect\numberline{%d}%s}
\begin{flushright}
{\sansbold\bfseries\fontsize{34pt}{36pt}\selectfont\textcolor{brandcrimson}{%d}\par}
\end{flushright}
\vspace{-6pt}
\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.7pt}}\par
\vspace{6pt}
{\sansbold\bfseries\fontsize{19pt}{23pt}\selectfont\textcolor{bodyblack}{%s}\par}
\vspace{8pt}
%s
`, chLabel, chCounter, tEsc, chCounter, tEsc, chCounter, tEsc, bodyTex))
	}

	// Render Alphabetical Index if entries exist and indexing is not disabled
	if !cfg.NoIndex && (len(indexDirect) > 0 || len(indexSubs) > 0) {
		parts = append(parts, renderIndexSection(indexDirect, indexSubs))
	}

	parts = append(parts, `\end{document}`)
	fullTex := strings.Join(parts, "\n")
	fullTex, stats.Notes = applyScriptFallback(fullTex, workDir, cfg.FontsDir)
	stats.Chapters = chCounter
	stats.TexBytes = len(fullTex)
	return fullTex, stats, nil
}

var rePartPrefix = regexp.MustCompile(`(?i)^\s*part\s+(?:[ivxlcdm]+|\d+)\b\s*[.:\x{2014}\x{2013}-]?\s*`)

func appendUnique(list []string, val string) []string {
	for _, v := range list {
		if v == val {
			return list
		}
	}
	return append(list, val)
}

func renderIndexSection(direct map[string][]string, subs map[string]map[string][]string) string {
	allCatsMap := make(map[string]bool)
	for k := range direct {
		allCatsMap[k] = true
	}
	for k := range subs {
		allCatsMap[k] = true
	}
	var allCats []string
	for k := range allCatsMap {
		allCats = append(allCats, k)
	}
	sort.Slice(allCats, func(i, j int) bool {
		return strings.ToLower(allCats[i]) < strings.ToLower(allCats[j])
	})

	byLetter := make(map[string][]string)
	var letters []string
	for _, cat := range allCats {
		first := strings.ToUpper(cat[:1])
		if first[0] < 'A' || first[0] > 'Z' {
			first = "#"
		}
		if _, ok := byLetter[first]; !ok {
			letters = append(letters, first)
		}
		byLetter[first] = append(byLetter[first], cat)
	}
	sort.Strings(letters)

	var out []string
	out = append(out,
		`\clearpage`,
		`\phantomsection`,
		`\renewcommand{\currentparttitle}{Index}`,
		`\renewcommand{\currentchaptitle}{Index}`,
		`\addcontentsline{toc}{chapter}{Index}`,
		`{\sansbold\bfseries\fontsize{22pt}{26pt}\selectfont\textcolor{brandcrimson}{Index}\par}`,
		`\vspace{4pt}`,
		`\noindent\textcolor{brandcrimson}{\rule{\textwidth}{0.7pt}}\par`,
		`\vspace{6pt}`,
		`\begin{multicols}{2}`,
		`\raggedright\footnotesize\setlength{\parskip}{1.5pt}`,
	)

	fmtRefs := func(lbls []string) string {
		if len(lbls) > 8 {
			lbls = lbls[:8]
		}
		refs := make([]string, len(lbls))
		for i, l := range lbls {
			refs[i] = fmt.Sprintf(`\pageref{%s}`, l)
		}
		return strings.Join(refs, ", ")
	}

	for _, letter := range letters {
		out = append(out, fmt.Sprintf(`\par\medskip\noindent{\sansbold\bfseries\fontsize{10.5pt}{13pt}\selectfont\textcolor{brandcrimson}{%s}}\par\nopagebreak[4]`, letter))
		for _, cat := range byLetter[letter] {
			cEsc := highlight.LatexEscape(cat)
			dList := direct[cat]
			sMap := subs[cat]
			if len(dList) > 0 && len(sMap) == 0 {
				out = append(out, fmt.Sprintf(`\noindent %s, %s\par`, cEsc, fmtRefs(dList)))
			} else {
				if len(dList) > 0 {
					out = append(out, fmt.Sprintf(`\noindent %s, %s\par\nopagebreak[4]`, cEsc, fmtRefs(dList)))
				} else {
					out = append(out, fmt.Sprintf(`\noindent %s\par\nopagebreak[4]`, cEsc))
				}
				var subKeys []string
				for sk := range sMap {
					subKeys = append(subKeys, sk)
				}
				sort.Slice(subKeys, func(i, j int) bool {
					return strings.ToLower(subKeys[i]) < strings.ToLower(subKeys[j])
				})
				for _, sk := range subKeys {
					sEsc := highlight.LatexEscape(sk)
					out = append(out, fmt.Sprintf(`\noindent\hspace*{1.1em}%s, %s\par`, sEsc, fmtRefs(sMap[sk])))
				}
			}
		}
	}

	out = append(out, `\end{multicols}`)
	return strings.Join(out, "\n")
}

// FontMode selects the typefaces used for the book.
type FontMode string

const (
	// FontsOpen uses free OFL fonts shipped in Tectonic's TeX bundle, so the
	// same source produces the same PDF on any machine, including CI.
	FontsOpen FontMode = "open"
	// FontsPro uses Adobe Minion Pro and Myriad Pro from a local font
	// directory. They are commercial and cannot be used in public CI.
	FontsPro FontMode = "pro"
	// FontsMono sets everything in the embedded Ubuntu Mono.
	FontsMono FontMode = "mono"
)

// ParseFontMode validates a user-supplied mode; empty means FontsOpen.
func ParseFontMode(s string) (FontMode, error) {
	switch m := FontMode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return FontsOpen, nil
	case FontsOpen, FontsPro, FontsMono:
		return m, nil
	}
	return "", fmt.Errorf("unknown fonts setting %q (use open, pro or mono)", s)
}

// Ubuntu Mono is embedded in the binary and staged into fonts/ for every
// build; it is the code face in all modes.
const monoCodeFont = `
\setmonofont[
  Path=fonts/,
  UprightFont=UbuntuMono-Regular.otf,
  ItalicFont=UbuntuMono-Italic.otf,
  BoldFont=UbuntuMono-Bold.otf,
  BoldItalicFont=UbuntuMono-BoldItalic.otf,
  Scale=0.88
]{UbuntuMono}
`

// Crimson Pro (body, an old-style serif in the spirit of Minion), Source
// Sans Pro (headings, a humanist sans in the spirit of Myriad) and IBM Plex
// Sans Condensed (running heads and labels). All SIL Open Font License, all
// resolved from Tectonic's bundle by file name.
const openFonts = `
\setmainfont{CrimsonPro}[
  Extension=.ttf,
  UprightFont=*-Regular,
  ItalicFont=*-Italic,
  BoldFont=*-SemiBold,
  BoldItalicFont=*-SemiBoldItalic,
  Ligatures=TeX,
  Scale=1.06
]
\setsansfont{SourceSansPro}[
  Extension=.otf,
  UprightFont=*-Regular,
  ItalicFont=*-RegularIt,
  BoldFont=*-Semibold,
  BoldItalicFont=*-SemiboldIt,
  Ligatures=TeX
]
\newfontfamily\sanscond{IBMPlexSansCondensed}[
  Extension=.otf,
  UprightFont=*-Regular,
  ItalicFont=*-Italic,
  BoldFont=*-SemiBold,
  BoldItalicFont=*-SemiBoldItalic,
  Ligatures=TeX
]
\newfontfamily\sanssemiboldcond{IBMPlexSansCondensed}[
  Extension=.otf,
  UprightFont=*-SemiBold,
  ItalicFont=*-SemiBoldItalic,
  BoldFont=*-Bold,
  Ligatures=TeX
]
\newfontfamily\sansbold{SourceSansPro}[
  Extension=.otf,
  UprightFont=*-Bold,
  BoldFont=*-Bold,
  Ligatures=TeX
]
` + monoCodeFont

const monoFonts = `
\setmainfont[
  Path=fonts/,
  UprightFont=UbuntuMono-Regular.otf,
  ItalicFont=UbuntuMono-Italic.otf,
  BoldFont=UbuntuMono-Bold.otf,
  BoldItalicFont=UbuntuMono-BoldItalic.otf
]{UbuntuMono}
\setsansfont[
  Path=fonts/,
  UprightFont=UbuntuMono-Regular.otf,
  ItalicFont=UbuntuMono-Italic.otf,
  BoldFont=UbuntuMono-Bold.otf,
  BoldItalicFont=UbuntuMono-BoldItalic.otf
]{UbuntuMono}
\newfontfamily\sanscond[Path=fonts/,UprightFont=UbuntuMono-Regular.otf,BoldFont=UbuntuMono-Bold.otf]{UbuntuMono}
\newfontfamily\sanssemiboldcond[Path=fonts/,UprightFont=UbuntuMono-Bold.otf,BoldFont=UbuntuMono-Bold.otf]{UbuntuMono}
\newfontfamily\sansbold[Path=fonts/,UprightFont=UbuntuMono-Bold.otf,BoldFont=UbuntuMono-Bold.otf]{UbuntuMono}
\setmonofont[
  Path=fonts/,
  UprightFont=UbuntuMono-Regular.otf,
  ItalicFont=UbuntuMono-Italic.otf,
  BoldFont=UbuntuMono-Bold.otf,
  BoldItalicFont=UbuntuMono-BoldItalic.otf,
  Scale=0.88
]{UbuntuMono}
`

const proFonts = `
\setmainfont[
  Path=fonts/,
  UprightFont=MinionPro-Regular.otf,
  ItalicFont=MinionPro-It.otf,
  BoldFont=MinionPro-Semibold.otf,
  BoldItalicFont=MinionPro-SemiboldIt.otf,
  Ligatures=TeX
]{MinionPro}

\setsansfont[
  Path=fonts/,
  UprightFont=MyriadPro-Regular.otf,
  ItalicFont=MyriadPro-It.otf,
  BoldFont=MyriadPro-Semibold.otf,
  Ligatures=TeX
]{MyriadPro}

\newfontfamily\sanscond[
  Path=fonts/,
  UprightFont=MyriadPro-Cond.otf,
  ItalicFont=MyriadPro-CondIt.otf,
  BoldFont=MyriadPro-SemiboldCond.otf,
  BoldItalicFont=MyriadPro-BoldCondIt.otf,
  Ligatures=TeX
]{MyriadProCond}

\newfontfamily\sanssemiboldcond[
  Path=fonts/,
  UprightFont=MyriadPro-SemiboldCond.otf,
  ItalicFont=MyriadPro-BoldCondIt.otf,
  BoldFont=MyriadPro-BoldCond.otf,
  Ligatures=TeX
]{MyriadProSemiboldCond}

\newfontfamily\sansbold[
  Path=fonts/,
  UprightFont=MyriadPro-Bold.otf,
  BoldFont=MyriadPro-Bold.otf,
  Ligatures=TeX
]{MyriadProBold}

\setmonofont[
  Path=fonts/,
  UprightFont=UbuntuMono-Regular.otf,
  ItalicFont=UbuntuMono-Italic.otf,
  BoldFont=UbuntuMono-Bold.otf,
  BoldItalicFont=UbuntuMono-BoldItalic.otf,
  Scale=0.88
]{UbuntuMono}
`

func buildPreamble(mode FontMode) string {
	var fontSetup string
	switch mode {
	case FontsPro:
		fontSetup = proFonts
	case FontsMono:
		fontSetup = monoFonts
	default:
		fontSetup = openFonts
	}

	return `\documentclass[10pt,twoside,openany]{book}
\usepackage[paperwidth=7in,paperheight=9.1875in,top=0.80in,bottom=0.88in,inner=0.88in,outer=0.76in,footskip=0.38in]{geometry}
\usepackage{amsmath,amssymb,mathrsfs}
\setcounter{MaxMatrixCols}{30}
\providecommand{\EE}{\mathbb{E}}
\providecommand{\PP}{\mathbb{P}}
\providecommand{\VV}{\mathbb{V}}
\providecommand{\RR}{\mathbb{R}}
\providecommand{\NN}{\mathbb{N}}
\providecommand{\ZZ}{\mathbb{Z}}
\providecommand{\CC}{\mathbb{C}}
\providecommand{\QQ}{\mathbb{Q}}
\providecommand{\argmax}{\operatorname*{arg\,max}}
\providecommand{\argmin}{\operatorname*{arg\,min}}
\usepackage{fontspec}
\usepackage[protrusion=true,expansion=false]{microtype}
\usepackage{xcolor}
\usepackage{graphicx}
\usepackage{tikz}
\usepackage{fancyvrb}
\usepackage{fvextra}
\usepackage{longtable}
\usepackage{colortbl}
\usepackage{array}
\usepackage{enumitem}
\usepackage{titlesec}
\usepackage{fancyhdr}
\usepackage{multicol}
\usepackage{needspace}
\usepackage{mdframed}
\usepackage[hidelinks,bookmarksnumbered=true,hypertexnames=false,pdfcreator={},pdfproducer={}]{hyperref}

\let\cleardoublepage\clearpage
` + fontSetup + `
\linespread{1.12}
\setlength{\parindent}{0pt}
\setlength{\parskip}{5.5pt plus 1.5pt minus 1pt}
\widowpenalty=10000
\clubpenalty=10000
\sloppy
\tolerance=2000
\emergencystretch=3em
\raggedbottom

\makeatletter
\renewcommand{\numberline}[1]{\hb@xt@ 2.4em{#1\hfil}}
\makeatother

% --- Editorial Color Palette ---
\definecolor{brandcrimson}{HTML}{8E0012}
\definecolor{bodyblack}{HTML}{1A1A1A}
\definecolor{codebg}{HTML}{F7F8FA}
\definecolor{codeborder}{HTML}{E1E4E8}
\definecolor{kwcol}{HTML}{8E0012}
\definecolor{typecol}{HTML}{2E5B88}
\definecolor{strcol}{HTML}{2E6B3E}
\definecolor{numcol}{HTML}{9E5810}
\definecolor{cmcol}{HTML}{6A737D}
\definecolor{fncol}{HTML}{4A3870}
\definecolor{tableheadbg}{HTML}{2C3E50}
\definecolor{tablerowalt}{HTML}{F4F6F9}
\definecolor{noteaccent}{HTML}{2E5B88}
\definecolor{notebg}{HTML}{F4F7FA}
\definecolor{tipaccent}{HTML}{3E7048}
\definecolor{tipbg}{HTML}{F2F7F4}
\definecolor{impaccent}{HTML}{9E4B10}
\definecolor{impbg}{HTML}{FCF5F0}

\color{bodyblack}

\newcommand{\inlinecode}[1]{{\ttfamily\fontsize{8.7pt}{10.5pt}\selectfont\textcolor{bodyblack}{#1}}}
\newcommand{\codewrapmark}{\textcolor{cmcol}{\ensuremath{\hookrightarrow}}}
\fvset{
  fontsize=\fontsize{8.1pt}{10.2pt}\selectfont,
  breaklines=true,
  breakanywhere=true,
  breakanywheresymbolpre={},
  breaksymbolright=\codewrapmark,
  xleftmargin=6pt,
  xrightmargin=4pt,
  framesep=5pt
}

\newmdenv[
  topline=false,bottomline=false,rightline=false,leftline=true,
  linewidth=2.4pt,linecolor=noteaccent,backgroundcolor=notebg,
  innerleftmargin=10pt,innerrightmargin=10pt,innertopmargin=7pt,innerbottommargin=7pt,
  skipabove=7pt,skipbelow=7pt
]{notebox}

\newmdenv[
  topline=false,bottomline=false,rightline=false,leftline=true,
  linewidth=2.4pt,linecolor=tipaccent,backgroundcolor=tipbg,
  innerleftmargin=10pt,innerrightmargin=10pt,innertopmargin=7pt,innerbottommargin=7pt,
  skipabove=7pt,skipbelow=7pt
]{tipbox}

\newmdenv[
  topline=false,bottomline=false,rightline=false,leftline=true,
  linewidth=2.4pt,linecolor=impaccent,backgroundcolor=impbg,
  innerleftmargin=10pt,innerrightmargin=10pt,innertopmargin=7pt,innerbottommargin=7pt,
  skipabove=7pt,skipbelow=7pt
]{importantbox}

\setlist[itemize]{topsep=3pt,itemsep=2.5pt,parsep=1pt,leftmargin=1.4em}
\setlist[enumerate]{topsep=3pt,itemsep=2.5pt,parsep=1pt,leftmargin=1.6em}

\titleformat{\section}
  {\Needspace{4\baselineskip}\sansbold\bfseries\fontsize{13.2pt}{16pt}\selectfont\color{brandcrimson}}
  {}{0pt}{}
\titlespacing*{\section}{0pt}{13pt plus 3pt minus 2pt}{4pt}

\titleformat{\subsection}
  {\Needspace{3\baselineskip}\sansbold\bfseries\fontsize{10.8pt}{13.5pt}\selectfont\color{diagslate}}
  {}{0pt}{}
\titlespacing*{\subsection}{0pt}{9pt plus 2pt minus 1pt}{2.5pt}

\newcommand{\currentparttitle}{Book}
\newcommand{\currentchaptitle}{Introduction}
\pagestyle{fancy}
\fancyhf{}
\renewcommand{\headrulewidth}{0pt}
\renewcommand{\footrulewidth}{0.4pt}
\fancyfoot[LE]{\sanscond\fontsize{8.2pt}{10pt}\selectfont\textcolor{diaggray}{\thepage\hspace{6pt}|\hspace{6pt}\currentparttitle}}
\fancyfoot[RO]{\sanscond\fontsize{8.2pt}{10pt}\selectfont\textcolor{diaggray}{\currentchaptitle\hspace{6pt}|\hspace{6pt}\thepage}}

\fancypagestyle{plain}{
  \fancyhf{}
  \renewcommand{\headrulewidth}{0pt}
  \renewcommand{\footrulewidth}{0.4pt}
  \fancyfoot[LE]{\sanscond\fontsize{8.2pt}{10pt}\selectfont\textcolor{diaggray}{\thepage\hspace{6pt}|\hspace{6pt}\currentparttitle}}
  \fancyfoot[RO]{\sanscond\fontsize{8.2pt}{10pt}\selectfont\textcolor{diaggray}{\currentchaptitle\hspace{6pt}|\hspace{6pt}\thepage}}
}
` + tikz.GetPreamble()
}
