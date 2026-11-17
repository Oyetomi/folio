package markdown

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/highlight"
	"github.com/Oyetomi/folio/internal/tikz"
	"gopkg.in/yaml.v3"
)

type ChapterDoc struct {
	IDTuple      [3]int
	HasID        bool
	PartIdx      int
	Title        string
	FilePath     string
	RawText      string
	InlineImages map[string][]byte
}

type IndexHit struct {
	Category string
	Subterm  string
}

var (
	reFrontmatter   = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n*`)
	reHTMLComment   = regexp.MustCompile(`(?s)<!--\s*(.*?)\s*-->`)
	reMdBookInc     = regexp.MustCompile(`\{\{#(?:rustdoc_)?include\s+([^}\s]+)\}\}`)
	reListingOpen   = regexp.MustCompile(`<Listing\b([^>]*)>`)
	reListingAttr   = regexp.MustCompile(`(number|file-name|caption)="([^"]*)"`)
	reSpanFilename  = regexp.MustCompile(`(?i)<span[^>]+class=["']?filename["']?[^>]*>(.*?)</span>`)
	reFMID          = regexp.MustCompile(`(?m)^id:\s*"?\[(\d+)\.(\d+)\.(\d+)\]"?`)
	reFMTitle       = regexp.MustCompile(`(?m)^title:\s*"?([^"\n]+)"?`)
	reH1Title       = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	reInlineCode    = regexp.MustCompile("`([^`\\n]+)`")
	reMystRoleMath  = regexp.MustCompile("\\{math\\}`([^`\\n]+)`")
	reMystRoleRef   = regexp.MustCompile("\\{(?:ref|doc|numref|term)\\}`([^`<\\n]+?)(?:\\s*<[^>`\\n]+>)?`")
	reMystRoleEq    = regexp.MustCompile("\\{eq\\}`([^`\\n]+)`")
	reMystRolePlain = regexp.MustCompile("\\{(?:cite|cite:t|cite:p|file|command|guilabel|menuselection)\\}`([^`\\n]+)`")
	reInlineMath    = regexp.MustCompile(`\$([^$\n]+)\$`)
	reSuperscript   = regexp.MustCompile(`\^\[([^\]\n]+)\]`)
	reCamelBreak    = regexp.MustCompile(`([a-z])([A-Z])`)
	reMDImage       = regexp.MustCompile(`^!\[([^\]]*)\]\((?:<([^>]+)>|([^)]+))\)$`)
	reMDImageAny    = regexp.MustCompile(`!\[([^\]]*)\]\((?:<([^>]+)>|([^)]+))\)`)
	reHTMLAnchor    = regexp.MustCompile(`(?i)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	reHTMLAnchorEmp = regexp.MustCompile(`(?i)<a\b[^>]*>\s*</a>`)
	reHTMLU         = regexp.MustCompile(`(?i)<u>(.*?)</u>`)
	reHTMLSummary   = regexp.MustCompile(`(?i)<summary>(.*?)</summary>`)
	reHTMLTagRem    = regexp.MustCompile(`(?i)</?(?:details|listing|span|p|div|img|sup|sub|small|kbd)\b[^>]*>`)
	reMDLink        = regexp.MustCompile(`\[([^\]]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)`)
	reRefDef        = regexp.MustCompile(`(?m)^\[([^\]]+)\]:\s*(\S+)[^\n]*$`)
	reRefLink       = regexp.MustCompile(`(?s)\[([^\]\[]{1,200}?)\]\[([^\[\]\n]{0,80})\]`)
	reBoldItalic    = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic        = regexp.MustCompile(`(?:^|[^*])\*([^*\n]+)\*`)
	reStraightQuot  = regexp.MustCompile(`"([^"\n]+)"`)
	reEmojiShort    = regexp.MustCompile(`:(?:coffee|shrug|smile|wink|tada|rocket|warning|bulb|memo|book):`)
	reHR            = regexp.MustCompile(`^-{3,}$`)
	reSceneBreak    = regexp.MustCompile(`^(?:\*\s*\*\s*\*|\*{3,})$`)
	reHeading       = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reListItem      = regexp.MustCompile(`^(\s*)([-*]|\d+\.)\s+(.*)$`)
	reSepCell       = regexp.MustCompile(`^:?-+:?$`)
	reDeepDive      = regexp.MustCompile(`^<(?:DeepDive|Note|Callout)(?:\s+title="([^"]*)")?\s*>(.*)</(?:DeepDive|Note|Callout)>$`)
	reWarning       = regexp.MustCompile(`^<(?:Warning|Important|Caution)(?:\s+title="([^"]*)")?\s*>(.*)</(?:Warning|Important|Caution)>$`)
	reAxiom         = regexp.MustCompile(`^<(?:Axiom|Tip|Quote)>(.*)</(?:Axiom|Tip|Quote)>$`)
	reGHAlert       = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION|QUOTE)\]\s*(.*)$`)
	reBoldCallout   = regexp.MustCompile(`^\*\*(Note|Tip|Warning|Important|Caution|Analogy|Key Takeaway|Definition|Example):\*\*\s*(.+)$`)
	reBoldQ         = regexp.MustCompile(`^\*\*.*\?\*\*$`)
	reInlineFence   = regexp.MustCompile("```([a-zA-Z0-9_,-]*)\\s+(.*?)\\s*```")
	rePkgSymbol     = regexp.MustCompile("`([a-z][a-z0-9_]{1,14})\\.([A-Z][A-Za-z0-9_]{1,28})`")
	reBoldTerm      = regexp.MustCompile(`(?m)^\s*[-*]\s+\*\*([A-Za-z][A-Za-z0-9 /()-]{2,34}?)(?::|\*\*\s*[—–-])`)
	reSummaryItem   = regexp.MustCompile(`^\s*([-*]\s+)?\[([^\]]+)\]\(([^)]+)\)`)
	reMystTarget    = regexp.MustCompile(`^\([a-zA-Z0-9_.-]+\)=$`)
	reMystDirective = regexp.MustCompile(`^(:{3,}|` + "`{3,})" + `\{([a-zA-Z0-9_:-]+)\}(.*)$`)
	reBareColonDiv  = regexp.MustCompile(`^(:{3,})\s+([a-zA-Z0-9_.-]+)(.*)$`)
)

func SanitizeImageFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	clean := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(base, "_")
	clean = strings.Trim(clean, "_")
	if clean == "" {
		clean = "img"
	}
	return clean + ext
}

type jupyterToc struct {
	Root  string `yaml:"root"`
	Parts []struct {
		Caption  string `yaml:"caption"`
		Chapters []struct {
			File     string `yaml:"file"`
			Sections []struct {
				File string `yaml:"file"`
			} `yaml:"sections"`
		} `yaml:"chapters"`
	} `yaml:"parts"`
	Chapters []struct {
		File     string `yaml:"file"`
		Sections []struct {
			File string `yaml:"file"`
		} `yaml:"sections"`
	} `yaml:"chapters"`
}

type mystConfig struct {
	Project struct {
		TOC []mystTocNode `yaml:"toc"`
	} `yaml:"project"`
}

type mystTocNode struct {
	File     string        `yaml:"file"`
	Title    string        `yaml:"title"`
	Children []mystTocNode `yaml:"children"`
}

func extractChapterHeadingTitle(txt string, fallback string) string {
	if mH1 := reH1Title.FindStringSubmatch(txt); len(mH1) > 1 {
		return strings.TrimSpace(mH1[1])
	}
	lines := strings.Split(strings.TrimLeft(txt, "\r\n"), "\n")
	if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[1]), "===") {
		t := strings.TrimSpace(lines[0])
		if t != "" {
			return t
		}
	}
	return fallback
}

func loadTocEntry(baseDir string, relFile string, overrideTitle string, partIdx int, chIdx int) (ChapterDoc, bool) {
	if relFile == "" {
		return ChapterDoc{}, false
	}
	fp := filepath.Join(baseDir, relFile)
	ext := strings.ToLower(filepath.Ext(fp))
	if ext == "" {
		if _, err := os.Stat(fp + ".md"); err == nil {
			fp += ".md"
			ext = ".md"
		} else if _, err := os.Stat(fp + ".ipynb"); err == nil {
			fp += ".ipynb"
			ext = ".ipynb"
		} else if _, err := os.Stat(fp + ".mdx"); err == nil {
			fp += ".mdx"
			ext = ".mdx"
		} else {
			fp += ".md"
			ext = ".md"
		}
	}

	fallbackTitle := strings.Title(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(relFile), filepath.Ext(relFile)), "_", " "))
	if ext == ".ipynb" {
		txt, nbTitle, imgs, err := ExtractIpynb(fp)
		if err != nil || strings.TrimSpace(txt) == "" {
			return ChapterDoc{}, false
		}
		title := nbTitle
		if overrideTitle != "" {
			title = overrideTitle
		}
		return ChapterDoc{
			IDTuple:      [3]int{partIdx, chIdx, 0},
			HasID:        true,
			PartIdx:      partIdx,
			Title:        title,
			FilePath:     fp,
			RawText:      txt,
			InlineImages: imgs,
		}, true
	}

	b, err := os.ReadFile(fp)
	if err != nil {
		return ChapterDoc{}, false
	}
	txt := string(b)
	title := extractChapterHeadingTitle(txt, fallbackTitle)
	if overrideTitle != "" {
		title = overrideTitle
	}
	return ChapterDoc{
		IDTuple:  [3]int{partIdx, chIdx, 0},
		HasID:    true,
		PartIdx:  partIdx,
		Title:    title,
		FilePath: fp,
		RawText:  txt,
	}, true
}

func DiscoverChapters(rootDir string, cfg *config.BookConfig) ([]ChapterDoc, error) {
	// 0. Check for Jupyter Book v2 myst.yml
	if mystData, err := os.ReadFile(filepath.Join(rootDir, "myst.yml")); err == nil {
		var mc mystConfig
		if yaml.Unmarshal(mystData, &mc) == nil && len(mc.Project.TOC) > 0 {
			var docs []ChapterDoc
			chIdx := 0
			var walkMyst func(nodes []mystTocNode, partIdx int)
			walkMyst = func(nodes []mystTocNode, partIdx int) {
				for _, node := range nodes {
					pNum := partIdx
					if strings.EqualFold(strings.TrimSuffix(filepath.Base(node.File), filepath.Ext(node.File)), "intro") && chIdx == 0 {
						pNum = 0
					}
					if doc, ok := loadTocEntry(rootDir, node.File, node.Title, pNum, chIdx+1); ok {
						chIdx++
						doc.IDTuple = [3]int{pNum, chIdx, 0}
						docs = append(docs, doc)
					}
					if len(node.Children) > 0 {
						walkMyst(node.Children, partIdx)
					}
				}
			}
			walkMyst(mc.Project.TOC, 1)
			if len(docs) > 0 {
				return docs, nil
			}
		}
	}

	// 1. Check for Jupyter Book _toc.yml first
	for _, baseCand := range []string{rootDir, filepath.Join(rootDir, cfg.Source), filepath.Join(rootDir, "lectures"), filepath.Join(rootDir, "book"), filepath.Join(rootDir, "docs")} {
		tocPath := filepath.Join(baseCand, "_toc.yml")
		if tocData, err := os.ReadFile(tocPath); err == nil {
			var toc jupyterToc
			if yaml.Unmarshal(tocData, &toc) == nil && (len(toc.Parts) > 0 || len(toc.Chapters) > 0) {
				var docs []ChapterDoc
				chIdx := 0
				addTocFile := func(relFile string, partIdx int) {
					if doc, ok := loadTocEntry(baseCand, relFile, "", partIdx, chIdx+1); ok {
						chIdx++
						doc.IDTuple = [3]int{partIdx, chIdx, 0}
						docs = append(docs, doc)
					}
				}
				for pIdx, p := range toc.Parts {
					for _, c := range p.Chapters {
						pNum := pIdx + 1
						if strings.EqualFold(filepath.Base(c.File), "about") && pIdx == 0 {
							pNum = 0
						}
						addTocFile(c.File, pNum)
						for _, s := range c.Sections {
							addTocFile(s.File, pNum)
						}
					}
				}
				for _, c := range toc.Chapters {
					addTocFile(c.File, 1)
					for _, s := range c.Sections {
						addTocFile(s.File, 1)
					}
				}
				if len(docs) > 0 {
					return docs, nil
				}
			}
		}
	}

	// 2. Check for GitBook / mdBook SUMMARY.md
	for _, sumCand := range []string{filepath.Join(rootDir, "SUMMARY.md"), filepath.Join(rootDir, "src", "SUMMARY.md")} {
		if sumData, err := os.ReadFile(sumCand); err == nil {
			sumDir := filepath.Dir(sumCand)
			sumStr := string(sumData)
			hasPartHeadings := strings.Contains(sumStr, "\n## ") || strings.HasPrefix(sumStr, "## ")
			var docs []ChapterDoc
			partIdx := 0
			chIdx := 0
			for _, line := range strings.Split(sumStr, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "## ") {
					partIdx++
					continue
				}
				if m := reSummaryItem.FindStringSubmatch(line); len(m) == 4 {
					hasBullet := strings.TrimSpace(m[1]) != ""
					linkTitle := strings.TrimSpace(m[2])
					relPath := strings.TrimSpace(m[3])
					baseLow := strings.ToLower(filepath.Base(relPath))
					if baseLow == "contributing.md" || baseLow == "template.md" || baseLow == "title-page.md" {
						continue
					}
					fp := filepath.Join(sumDir, relPath)
					b, err := os.ReadFile(fp)
					if err != nil {
						continue
					}
					chIdx++
					txt := string(b)
					title := linkTitle
					if mH1 := reH1Title.FindStringSubmatch(txt); len(mH1) > 1 {
						title = strings.TrimSpace(mH1[1])
					}
					docPart := partIdx
					if !hasPartHeadings {
						if !hasBullet || strings.Contains(baseLow, "foreword") || strings.Contains(baseLow, "preface") || strings.Contains(baseLow, "introduction") {
							docPart = 0
						} else if strings.HasPrefix(baseLow, "appendix") {
							docPart = 999
						} else {
							docPart = 1
						}
					}
					docs = append(docs, ChapterDoc{
						IDTuple:  [3]int{docPart, chIdx, 0},
						HasID:    true,
						PartIdx:  docPart,
						Title:    title,
						FilePath: fp,
						RawText:  txt,
					})
				}
			}
			if len(docs) > 0 {
				return docs, nil
			}
		}
	}

	// 2b. Check for The Carpentries sandpaper config.yaml (episodes ordering)
	if carpData, err := os.ReadFile(filepath.Join(rootDir, "config.yaml")); err == nil {
		var cc struct {
			Episodes []string `yaml:"episodes"`
		}
		if yaml.Unmarshal(carpData, &cc) == nil && len(cc.Episodes) > 0 {
			var docs []ChapterDoc
			chIdx := 0
			for _, ep := range cc.Episodes {
				rel := filepath.Join("episodes", ep)
				if doc, ok := loadTocEntry(rootDir, rel, "", 1, chIdx+1); ok {
					chIdx++
					doc.IDTuple = [3]int{1, chIdx, 0}
					docs = append(docs, doc)
				}
			}
			if len(docs) > 0 {
				return docs, nil
			}
		}
	}

	// 3. Fallback directory walk
	srcDir := filepath.Join(rootDir, cfg.Source)
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		srcDir = rootDir
	}

	var mdxFiles []string
	var mdFiles []string
	var ipynbFiles []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Never skip the root itself: "." would match the hidden-dir rule.
			if path != srcDir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "website" || name == "exercises") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".mdx" {
			mdxFiles = append(mdxFiles, path)
		} else if ext == ".md" && !strings.EqualFold(d.Name(), "README.md") && !strings.EqualFold(d.Name(), "SUMMARY.md") && !strings.EqualFold(d.Name(), "CLAUDE.md") && !strings.EqualFold(d.Name(), "AGENTS.md") && !strings.EqualFold(d.Name(), "CODE_OF_CONDUCT.md") && !strings.EqualFold(d.Name(), "CONTRIBUTING.md") {
			mdFiles = append(mdFiles, path)
		} else if ext == ".ipynb" {
			ipynbFiles = append(ipynbFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	files := mdxFiles
	if len(files) == 0 {
		files = mdFiles
	}
	if len(files) == 0 {
		files = ipynbFiles
	}

	var docs []ChapterDoc
	for _, fp := range files {
		if strings.EqualFold(filepath.Ext(fp), ".ipynb") {
			txt, title, imgs, err := ExtractIpynb(fp)
			if err != nil || strings.TrimSpace(txt) == "" {
				continue
			}
			docs = append(docs, ChapterDoc{
				PartIdx:      1,
				Title:        title,
				FilePath:     fp,
				RawText:      txt,
				InlineImages: imgs,
			})
			continue
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		txt := string(data)
		var doc ChapterDoc
		doc.FilePath = fp
		doc.RawText = txt

		if m := reFMID.FindStringSubmatch(txt); len(m) == 4 {
			x, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[2])
			z, _ := strconv.Atoi(m[3])
			doc.IDTuple = [3]int{x, y, z}
			doc.HasID = true
			doc.PartIdx = x
		} else {
			doc.PartIdx = 1
		}

		if m := reFMTitle.FindStringSubmatch(txt); len(m) > 1 {
			doc.Title = strings.TrimSpace(m[1])
		} else if m := reH1Title.FindStringSubmatch(txt); len(m) > 1 {
			doc.Title = strings.TrimSpace(m[1])
		} else {
			base := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
			doc.Title = strings.Title(strings.ReplaceAll(base, "-", " "))
		}

		docs = append(docs, doc)
	}

	sort.Slice(docs, func(i, j int) bool {
		if docs[i].HasID && docs[j].HasID {
			if docs[i].IDTuple[0] != docs[j].IDTuple[0] {
				return docs[i].IDTuple[0] < docs[j].IDTuple[0]
			}
			if docs[i].IDTuple[1] != docs[j].IDTuple[1] {
				return docs[i].IDTuple[1] < docs[j].IDTuple[1]
			}
			return docs[i].IDTuple[2] < docs[j].IDTuple[2]
		}
		return docs[i].FilePath < docs[j].FilePath
	})

	return docs, nil
}

func FormatInline(text string, cfg *config.BookConfig) string {
	if text == "" {
		return ""
	}

	var tokens []string
	saveToken := func(tex string) string {
		idx := len(tokens)
		tokens = append(tokens, tex)
		return fmt.Sprintf("\x00TOK%d\x00", idx)
	}

	// Preserve explicit Markdown hard line breaks (2+ trailing spaces or literal newline inside poetry/blocks)
	text = strings.ReplaceAll(text, "  \n", saveToken(`\\\relax `))

	// 0. Normalize MyST inline roles before standard code/math tokenization
	text = reMystRoleMath.ReplaceAllStringFunc(text, func(m string) string {
		sub := reMystRoleMath.FindStringSubmatch(m)
		return saveToken("$" + sub[1] + "$")
	})
	text = reMystRoleEq.ReplaceAllStringFunc(text, func(m string) string {
		sub := reMystRoleEq.FindStringSubmatch(m)
		return saveToken(fmt.Sprintf(`\textit{(%s)}`, highlight.LatexEscape(sub[1])))
	})
	text = reMystRoleRef.ReplaceAllStringFunc(text, func(m string) string {
		sub := reMystRoleRef.FindStringSubmatch(m)
		return strings.TrimSpace(sub[1])
	})
	text = reMystRolePlain.ReplaceAllString(text, "$1")

	// 1. Tokenize inline code spans first so brackets/parens inside backticks are untouched
	text = reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		raw := match[1 : len(match)-1]
		if len(raw) > 16 {
			raw = reCamelBreak.ReplaceAllString(raw, "$1\x01$2")
		}
		esc := highlight.CodeEscapeInline(raw)
		esc = strings.ReplaceAll(esc, "\x01", `\allowbreak{}`)
		for _, sep := range []string{"/", ".", `\_`, "-", "?", `\&`, "=", ":", ","} {
			esc = strings.ReplaceAll(esc, sep, sep+`\allowbreak{}`)
		}
		return saveToken(fmt.Sprintf(`\inlinecode{%s}`, esc))
	})

	// 1b. Preserve escaped \$ before matching inline math ($...$)
	text = strings.ReplaceAll(text, `\$`, saveToken(`\$`))
	text = reInlineMath.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1]
		trimIn := strings.TrimSpace(inner)
		lowerIn := strings.ToLower(trimIn)
		if len(trimIn) > 0 && trimIn[0] >= '0' && trimIn[0] <= '9' {
			if strings.Contains(lowerIn, " million") || strings.Contains(lowerIn, " billion") || strings.Contains(lowerIn, " thousand") || strings.Contains(lowerIn, " hundred") || strings.Contains(lowerIn, " to ") || strings.Contains(lowerIn, " per ") || (len(trimIn) > 2 && strings.Contains(trimIn, " ") && !strings.ContainsAny(trimIn, "+-=*/^_\\")) {
				return match
			}
		}
		return saveToken("$" + highlight.NormalizeMathUnicode(inner) + "$")
	})

	// 1b-bis. Preserve common Markdown backslash escapes outside inline code and math
	for _, pair := range [][2]string{
		{`\@`, `@`},
		{`\*`, `*`},
		{`\#`, `\#`},
		{`\&`, `\&`},
		{`\%`, `\%`},
		{`\_`, `\_`},
		{`\[`, `[`},
		{`\]`, `]`},
		{`\+`, `+`},
		{`\-`, `-`},
		{`\!`, `!`},
		{`\~`, `\textasciitilde{}`},
		{`\<`, `\textless{}`},
		{`\>`, `\textgreater{}`},
	} {
		text = strings.ReplaceAll(text, pair[0], saveToken(pair[1]))
	}

	// 1c. Superscripts ^[25] (footnotes / exponents)
	text = reSuperscript.ReplaceAllStringFunc(text, func(match string) string {
		sub := reSuperscript.FindStringSubmatch(match)
		return saveToken(fmt.Sprintf(`\textsuperscript{%s}`, highlight.LatexEscape(sub[1])))
	})

	// 1d. Normalize inline HTML tags (<a href="...">, <a id="..."></a>, <summary>, <u>) outside code spans
	text = reHTMLAnchorEmp.ReplaceAllString(text, "")
	text = reHTMLAnchor.ReplaceAllString(text, "[$2]($1)")
	text = reHTMLSummary.ReplaceAllString(text, "**$1**")
	text = reHTMLTagRem.ReplaceAllString(text, "")
	text = reMDImageAny.ReplaceAllString(text, "")
	text = reEmojiShort.ReplaceAllString(text, "")

	escSub := func(s string) string {
		s = reStraightQuot.ReplaceAllString(s, "“$1”")
		res := highlight.LatexEscape(s)
		res = strings.ReplaceAll(res, "---", "—")
		res = strings.ReplaceAll(res, "--", "–")
		return res
	}

	text = reHTMLU.ReplaceAllStringFunc(text, func(match string) string {
		sub := reHTMLU.FindStringSubmatch(match)
		return saveToken(fmt.Sprintf(`\underline{%s}`, FormatInline(sub[1], cfg)))
	})

	// 2. Markdown links [label](url)
	text = reMDLink.ReplaceAllStringFunc(text, func(match string) string {
		sub := reMDLink.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		lbl, url := strings.TrimSpace(sub[1]), strings.TrimSpace(sub[2])
		lblTex := ""
		if strings.HasPrefix(lbl, "***") && strings.HasSuffix(lbl, "***") && len(lbl) > 6 {
			lblTex = fmt.Sprintf(`\textbf{\textit{%s}}`, escSub(strings.TrimSpace(lbl[3:len(lbl)-3])))
		} else if strings.HasPrefix(lbl, "**") && strings.HasSuffix(lbl, "**") && len(lbl) > 4 {
			lblTex = fmt.Sprintf(`\textbf{%s}`, escSub(strings.TrimSpace(lbl[2:len(lbl)-2])))
		} else if (strings.HasPrefix(lbl, "*") && strings.HasSuffix(lbl, "*") && len(lbl) > 2) || (strings.HasPrefix(lbl, "_") && strings.HasSuffix(lbl, "_") && len(lbl) > 2) {
			lblTex = fmt.Sprintf(`\textit{%s}`, escSub(strings.TrimSpace(lbl[1:len(lbl)-1])))
		} else {
			lblWork := reItalic.ReplaceAllStringFunc(lbl, func(im string) string {
				isub := reItalic.FindStringSubmatch(im)
				pref := ""
				if !strings.HasPrefix(im, "*") {
					_, rSize := utf8.DecodeRuneInString(im)
					pref = im[:rSize]
				}
				return pref + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(isub[1])))
			})
			lblTex = escSub(lblWork)
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "mailto:") {
			safeURL := escapeHrefURL(url)
			return saveToken(fmt.Sprintf(`\href{%s}{%s}`, safeURL, lblTex))
		}
		if cfg.RepoURL != "" && (strings.HasPrefix(url, "../") || strings.HasPrefix(url, "./") || strings.Contains(url, "/")) {
			relPath := strings.TrimLeft(url, "./")
			branch := cfg.DefaultBranch
			if branch == "" {
				branch = "master"
			}
			fullURL := fmt.Sprintf("%s/blob/%s/%s", strings.TrimRight(cfg.RepoURL, "/"), branch, relPath)
			safeURL := escapeHrefURL(fullURL)
			return saveToken(fmt.Sprintf(`\href{%s}{\sanssemiboldcond\fontsize{8.2pt}{10pt}\selectfont\textcolor{brandcrimson}{[%s $\rightarrow$]}}`, safeURL, lblTex))
		}
		return saveToken(fmt.Sprintf(`\textbf{%s}`, lblTex))
	})

	// 3. Bold-Italic ***...***, Bold **...**, and Italic *...* / _..._
	text = reBoldItalic.ReplaceAllStringFunc(text, func(match string) string {
		sub := reBoldItalic.FindStringSubmatch(match)
		return saveToken(fmt.Sprintf(`\textbf{\textit{%s}}`, escSub(sub[1])))
	})
	text = applyUnderscoreDelim(text, "__", func(seg string) string {
		return saveToken(fmt.Sprintf(`\textbf{%s}`, escSub(seg)))
	})
	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		sub := reBold.FindStringSubmatch(match)
		inner := sub[1]
		inner = reItalic.ReplaceAllStringFunc(inner, func(im string) string {
			isub := reItalic.FindStringSubmatch(im)
			pref := ""
			if !strings.HasPrefix(im, "*") {
				_, rSize := utf8.DecodeRuneInString(im)
				pref = im[:rSize]
			}
			return pref + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(isub[1])))
		})
		inner = applyUnderscoreEmphasis(inner, func(seg string) string {
			return saveToken(fmt.Sprintf(`\textit{%s}`, escSub(seg)))
		})
		return saveToken(fmt.Sprintf(`\textbf{%s}`, escSub(inner)))
	})
	text = reItalic.ReplaceAllStringFunc(text, func(match string) string {
		sub := reItalic.FindStringSubmatch(match)
		prefix := ""
		if !strings.HasPrefix(match, "*") {
			_, rSize := utf8.DecodeRuneInString(match)
			prefix = match[:rSize]
		}
		return prefix + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(sub[1])))
	})
	text = applyUnderscoreEmphasis(text, func(seg string) string {
		return saveToken(fmt.Sprintf(`\textit{%s}`, escSub(seg)))
	})

	escaped := escSub(text)
	reRestore := regexp.MustCompile(`\x00TOK(\d+)\x00`)
	for strings.Contains(escaped, "\x00TOK") {
		escaped = reRestore.ReplaceAllStringFunc(escaped, func(m string) string {
			sub := reRestore.FindStringSubmatch(m)
			idx, _ := strconv.Atoi(sub[1])
			if idx >= 0 && idx < len(tokens) {
				return tokens[idx]
			}
			return ""
		})
	}
	return escaped
}

func isASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func applyUnderscoreEmphasis(s string, wrapFn func(string) string) string {
	return applyUnderscoreDelim(s, "_", wrapFn)
}

// applyUnderscoreDelim converts CommonMark _italic_ or __bold__ spans without breaking snake_case.
func applyUnderscoreDelim(s string, delim string, wrapFn func(string) string) string {
	if !strings.Contains(s, delim) {
		return s
	}
	dLen := len([]rune(delim))
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 16)
	i := 0
	for i < len(runes) {
		if runes[i] == 0 {
			b.WriteRune(runes[i])
			i++
			for i < len(runes) && runes[i] != 0 {
				b.WriteRune(runes[i])
				i++
			}
			if i < len(runes) {
				b.WriteRune(runes[i])
				i++
			}
			continue
		}
		matchAt := func(pos int) bool {
			if pos+dLen > len(runes) {
				return false
			}
			for k := 0; k < dLen; k++ {
				if runes[pos+k] != '_' {
					return false
				}
			}
			return true
		}
		if matchAt(i) && (i == 0 || !isASCIIWordRune(runes[i-1])) && i+dLen < len(runes) && runes[i+dLen] != '_' && runes[i+dLen] != ' ' && runes[i+dLen] != '\t' && runes[i+dLen] != '\n' {
			closeIdx := -1
			j := i + dLen
			for j < len(runes) {
				if runes[j] == '\n' {
					break
				}
				if runes[j] == 0 {
					j++
					for j < len(runes) && runes[j] != 0 {
						j++
					}
					j++
					continue
				}
				if matchAt(j) && runes[j-1] != '_' && runes[j-1] != ' ' && runes[j-1] != '\t' && (j+dLen == len(runes) || !isASCIIWordRune(runes[j+dLen])) {
					closeIdx = j
					break
				}
				j++
			}
			if closeIdx > i+dLen {
				inner := string(runes[i+dLen : closeIdx])
				b.WriteString(wrapFn(inner))
				i = closeIdx + dLen
				continue
			}
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func escapeHrefURL(u string) string {
	u = strings.ReplaceAll(u, "%", `\%`)
	u = strings.ReplaceAll(u, "#", `\#`)
	u = strings.ReplaceAll(u, "_", `\_`)
	u = strings.ReplaceAll(u, "&", `\&`)
	return u
}

func RenderTable(tableLines []string, cfg *config.BookConfig, noHead bool) string {
	var rows [][]string
	for _, line := range tableLines {
		s := strings.TrimSpace(line)
		if strings.Count(s, "|") < 2 || !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
			continue
		}
		rawCells := strings.Split(s[1:len(s)-1], "|")
		cells := make([]string, 0, len(rawCells))
		isSep := true
		for _, c := range rawCells {
			tc := strings.TrimSpace(c)
			cells = append(cells, tc)
			if tc != "" && !reSepCell.MatchString(strings.ReplaceAll(tc, " ", "")) {
				isSep = false
			}
		}
		if isSep {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) < 1 {
		return ""
	}
	if len(rows) == 1 && !noHead {
		return ""
	}

	numCols := 0
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	for i := range rows {
		for len(rows[i]) < numCols {
			rows[i] = append(rows[i], "")
		}
	}

	maxLen := make([]int, numCols)
	maxWord := make([]int, numCols)
	monoMaxWord := make([]int, numCols)
	for i := 0; i < numCols; i++ {
		maxLen[i] = 1
		maxWord[i] = 1
	}

	for _, r := range rows {
		for cIdx, cell := range r {
			plain := strings.NewReplacer("`", "", "*", "", "_", "").Replace(cell)
			if len(plain) > maxLen[cIdx] {
				maxLen[cIdx] = len(plain)
			}
			for _, w := range strings.Fields(plain) {
				if len(w) > maxWord[cIdx] {
					maxWord[cIdx] = len(w)
				}
			}
			for _, cm := range reInlineCode.FindAllStringSubmatch(cell, -1) {
				for _, cw := range strings.Fields(cm[1]) {
					if len(cw) > monoMaxWord[cIdx] {
						monoMaxWord[cIdx] = len(cw)
					}
				}
			}
		}
	}

	availIn := 5.33 - (12.0*float64(numCols))/72.27
	minW := make([]float64, numCols)
	prefW := make([]float64, numCols)
	sumMin := 0.0
	for i := 0; i < numCols; i++ {
		mw := math.Max(0.45, math.Max(float64(maxWord[i])*0.056+0.14, float64(monoMaxWord[i])*0.068+0.16))
		minW[i] = math.Min(mw, availIn*0.48)
		prefW[i] = math.Max(minW[i], math.Pow(float64(maxLen[i]), 0.65)*0.22)
		sumMin += minW[i]
	}

	colW := make([]float64, numCols)
	if sumMin >= availIn {
		for i := 0; i < numCols; i++ {
			colW[i] = minW[i] * (availIn / sumMin)
		}
	} else {
		rem := availIn - sumMin
		sumEx := 0.0
		extras := make([]float64, numCols)
		for i := 0; i < numCols; i++ {
			extras[i] = math.Max(0.0, prefW[i]-minW[i])
			sumEx += extras[i]
		}
		if sumEx == 0 {
			sumEx = 1.0
		}
		for i := 0; i < numCols; i++ {
			colW[i] = minW[i] + rem*(extras[i]/sumEx)
		}
	}

	var spec strings.Builder
	for _, w := range colW {
		spec.WriteString(fmt.Sprintf(`>{\raggedright\arraybackslash}p{%.3fin}`, w))
	}

	var out []string
	out = append(out, `\par\medskip\noindent`)
	out = append(out, `{\fontsize{8.5pt}{11pt}\selectfont`)
	out = append(out, `\setlength{\tabcolsep}{6pt}\renewcommand{\arraystretch}{1.32}`)
	out = append(out, fmt.Sprintf(`\begin{longtable}{%s}`, spec.String()))

	dataRows := rows
	if !noHead {
		out = append(out, `\rowcolor{tableheadbg}`)
		hdrCells := make([]string, numCols)
		for i, c := range rows[0] {
			hdrCells[i] = fmt.Sprintf(`\sansbold\fontsize{8.3pt}{10.5pt}\selectfont\textcolor{white}{%s}`, FormatInline(c, cfg))
		}
		out = append(out, strings.Join(hdrCells, " & ")+` \\ \endhead`)
		dataRows = rows[1:]
	} else {
		out = append(out, `\hline`)
	}

	for rIdx, r := range dataRows {
		bg := ""
		if (rIdx+1)%2 == 0 {
			bg = `\rowcolor{tablerowalt} `
		}
		bCells := make([]string, numCols)
		for i, c := range r {
			cellTex := FormatInline(c, cfg)
			if i == 0 && strings.HasPrefix(cellTex, "[") {
				cellTex = "{[}" + cellTex[1:]
			}
			bCells[i] = cellTex
		}
		out = append(out, bg+strings.Join(bCells, " & ")+` \\`)
	}
	out = append(out, `\hline`)
	out = append(out, `\end{longtable}}`)
	out = append(out, `\par\smallskip`)
	return strings.Join(out, "\n")
}

func formatCalloutBody(raw string, cfg *config.BookConfig) string {
	if strings.Contains(raw, "```") {
		var sb strings.Builder
		last := 0
		for _, loc := range reInlineFence.FindAllStringSubmatchIndex(raw, -1) {
			if loc[0] > last {
				pre := strings.TrimSpace(raw[last:loc[0]])
				if pre != "" {
					sb.WriteString(FormatInline(pre, cfg))
				}
			}
			lang := raw[loc[2]:loc[3]]
			code := formatSingleLineGo(strings.TrimSpace(raw[loc[4]:loc[5]]))
			sb.WriteString("\n\\par\\smallskip\n")
			sb.WriteString(strings.TrimSpace(highlight.HighlightBlock(lang, code)))
			sb.WriteString("\n\\par\\smallskip\\noindent ")
			last = loc[1]
		}
		if last < len(raw) {
			rem := strings.TrimSpace(raw[last:])
			if rem != "" {
				sb.WriteString(FormatInline(rem, cfg))
			}
		}
		return strings.TrimSpace(sb.String())
	}
	return FormatInline(raw, cfg)
}

func formatSingleLineGo(code string) string {
	if strings.Contains(code, "\n") {
		return code
	}
	var out strings.Builder
	indent := 0
	inStr := false
	for i := 0; i < len(code); i++ {
		ch := code[i]
		if ch == '"' && (i == 0 || code[i-1] != '\\') {
			inStr = !inStr
		}
		if !inStr {
			if ch == '{' {
				out.WriteByte('{')
				indent++
				out.WriteByte('\n')
				out.WriteString(strings.Repeat("    ", indent))
				for i+1 < len(code) && code[i+1] == ' ' {
					i++
				}
				continue
			}
			if ch == '}' {
				if indent > 0 {
					indent--
				}
				out.WriteByte('\n')
				out.WriteString(strings.Repeat("    ", indent))
				out.WriteByte('}')
				if i+2 < len(code) && code[i+1] == '(' && code[i+2] == ')' {
					out.WriteString("()")
					i += 2
				}
				out.WriteByte('\n')
				out.WriteString(strings.Repeat("    ", indent))
				for i+1 < len(code) && code[i+1] == ' ' {
					i++
				}
				continue
			}
		}
		out.WriteByte(ch)
	}
	return strings.TrimSpace(out.String())
}

var reScriptStyle = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)>`)

func stripNonFenceScriptStyle(content string) string {
	low := strings.ToLower(content)
	if !strings.Contains(low, "<script") && !strings.Contains(low, "<style") {
		return content
	}
	var out []string
	var prose []string
	inFence := false
	for _, ln := range strings.Split(content, "\n") {
		tr := strings.TrimSpace(ln)
		if strings.HasPrefix(tr, "```") {
			if !inFence {
				if len(prose) > 0 {
					out = append(out, reScriptStyle.ReplaceAllString(strings.Join(prose, "\n"), ""))
					prose = nil
				}
				inFence = true
			} else {
				inFence = false
			}
			out = append(out, ln)
			continue
		}
		if inFence {
			out = append(out, ln)
		} else {
			prose = append(prose, ln)
		}
	}
	if len(prose) > 0 {
		out = append(out, reScriptStyle.ReplaceAllString(strings.Join(prose, "\n"), ""))
	}
	return strings.Join(out, "\n")
}

func preprocessChapter(rawText string, docPath string) string {
	content := reFrontmatter.ReplaceAllString(rawText, "")

	// Strip HTML comments (keep <!-- nohead --> marker for headerless tables)
	content = reHTMLComment.ReplaceAllStringFunc(content, func(m string) string {
		if strings.Contains(m, "nohead") {
			return "<!-- nohead -->"
		}
		return ""
	})

	// Strip <script>...</script> and <style>...</style> web widget blocks outside code fences
	content = stripNonFenceScriptStyle(content)

	// Convert <Listing ...> tags to readable bold captions
	content = reListingOpen.ReplaceAllStringFunc(content, func(m string) string {
		attrs := make(map[string]string)
		for _, a := range reListingAttr.FindAllStringSubmatch(m, -1) {
			attrs[a[1]] = a[2]
		}
		num, fn, cap := attrs["number"], attrs["file-name"], attrs["caption"]
		if num == "" && cap == "" {
			return ""
		}
		var sb strings.Builder
		if num != "" {
			sb.WriteString("**Listing " + num + "**")
		}
		if fn != "" {
			sb.WriteString(" (`" + fn + "`)")
		}
		if cap != "" {
			sb.WriteString(": " + cap)
		}
		return "\n" + sb.String() + "\n"
	})
	content = reSpanFilename.ReplaceAllString(content, "\n*$1*\n")

	// Resolve Markdown reference-style link definitions `[id]: https://...` outside code blocks
	refURLs := make(map[string]string)
	inFence := false
	var step1 []string
	for _, ln := range strings.Split(content, "\n") {
		tr := strings.TrimSpace(ln)
		if strings.HasPrefix(tr, "```") {
			inFence = !inFence
			step1 = append(step1, ln)
			continue
		}
		if !inFence {
			// Skip MyST % comments (e.g. `%We start...` or `%$$ ...`)
			if strings.HasPrefix(tr, "%") {
				continue
			}
			if m := reRefDef.FindStringSubmatch(ln); len(m) == 3 {
				key := strings.ToLower(strings.TrimSpace(m[1]))
				url := strings.Trim(strings.TrimSpace(m[2]), "<>")
				refURLs[key] = url
				continue
			}
		}
		step1 = append(step1, ln)
	}
	// Normalize Setext headings (`Title\n===` and `Subheading\n---`) outside code fences
	inFence = false
	for idx := 0; idx < len(step1); idx++ {
		tr := strings.TrimSpace(step1[idx])
		if strings.HasPrefix(tr, "```") {
			inFence = !inFence
			continue
		}
		if inFence || idx == 0 {
			continue
		}
		if len(tr) >= 3 && (strings.Trim(tr, "=") == "" || strings.Trim(tr, "-") == "") {
			prev := strings.TrimSpace(step1[idx-1])
			prevBlank := idx < 2 || strings.TrimSpace(step1[idx-2]) == ""
			if prev != "" && len(prev) <= 100 && prevBlank && !strings.HasPrefix(prev, "#") && !strings.HasPrefix(prev, ">") && !strings.HasPrefix(prev, "-") && !strings.HasPrefix(prev, "*") && !strings.HasPrefix(prev, "|") && !strings.HasPrefix(prev, "```") {
				if tr[0] == '=' {
					step1[idx-1] = "# " + prev
				} else {
					step1[idx-1] = "## " + prev
				}
				step1[idx] = ""
			}
		}
	}
	if len(refURLs) > 0 {
		resolveChunk := func(chunk string) string {
			return reRefLink.ReplaceAllStringFunc(chunk, func(m string) string {
				sub := reRefLink.FindStringSubmatch(m)
				lbl := sub[1]
				if strings.Contains(lbl, "\n\n") {
					return m
				}
				flatLbl := strings.Join(strings.Fields(lbl), " ")
				refKey := strings.ToLower(strings.TrimSpace(sub[2]))
				if refKey == "" {
					refKey = strings.ToLower(flatLbl)
				}
				if url, ok := refURLs[refKey]; ok {
					return fmt.Sprintf("[%s](%s)", flatLbl, url)
				}
				return m
			})
		}
		var chunks []string
		var proseLines []string
		inFence = false
		for _, ln := range step1 {
			tr := strings.TrimSpace(ln)
			if strings.HasPrefix(tr, "```") {
				if !inFence {
					if len(proseLines) > 0 {
						chunks = append(chunks, resolveChunk(strings.Join(proseLines, "\n")))
						proseLines = nil
					}
					inFence = true
					chunks = append(chunks, ln)
				} else {
					inFence = false
					chunks = append(chunks, ln)
				}
				continue
			}
			if inFence {
				chunks = append(chunks, ln)
			} else {
				proseLines = append(proseLines, ln)
			}
		}
		if len(proseLines) > 0 {
			chunks = append(chunks, resolveChunk(strings.Join(proseLines, "\n")))
		}
		content = strings.Join(chunks, "\n")
	} else {
		content = strings.Join(step1, "\n")
	}

	// Expand mdBook {{#include path}} and {{#rustdoc_include path[:anchor]}}
	if docPath != "" && strings.Contains(content, "{{#") {
		baseDir := filepath.Dir(docPath)
		content = reMdBookInc.ReplaceAllStringFunc(content, func(m string) string {
			sub := reMdBookInc.FindStringSubmatch(m)
			spec := sub[1]
			relPath := spec
			anchor := ""
			if idx := strings.LastIndexByte(spec, ':'); idx > 0 {
				relPath = spec[:idx]
				anchor = spec[idx+1:]
			}
			fullPath := filepath.Join(baseDir, relPath)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				return ""
			}
			codeText := string(data)
			if anchor != "" {
				if extracted := extractMdBookAnchor(codeText, anchor); extracted != "" {
					codeText = extracted
				}
			}
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(relPath)), ".")
			if ext == "rs" {
				ext = "rust"
			} else if ext == "txt" {
				ext = "text"
			}
			return "```" + ext + "\n" + strings.TrimSpace(codeText) + "\n```"
		})
	}

	return content
}

func extractMdBookAnchor(src, anchor string) string {
	lines := strings.Split(src, "\n")
	var kept []string
	inAnchor := false
	for _, l := range lines {
		tr := strings.TrimSpace(l)
		if strings.Contains(tr, "ANCHOR: "+anchor) {
			inAnchor = true
			continue
		}
		if inAnchor && strings.Contains(tr, "ANCHOR_END: "+anchor) {
			break
		}
		if inAnchor {
			if strings.Contains(tr, "ANCHOR") {
				continue
			}
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

func CompileChapter(mdxText string, chNum int, cfg *config.BookConfig) (string, int) {
	return CompileChapterFile(mdxText, chNum, cfg, "")
}

func CompileChapterFile(mdxText string, chNum int, cfg *config.BookConfig, docPath string) (string, int) {
	content := preprocessChapter(mdxText, docPath)
	lines := strings.Split(content, "\n")

	var out []string
	figCounter := 0
	i := 0
	n := len(lines)
	nextTableNoHead := false

	for i < n {
		line := lines[i]
		stripped := strings.TrimSpace(line)

		if stripped == "" {
			out = append(out, "")
			i++
			continue
		}
		if stripped == "<!-- nohead -->" {
			nextTableNoHead = true
			i++
			continue
		}
		if reMystTarget.MatchString(stripped) || reHR.MatchString(stripped) {
			i++
			continue
		}
		if reSceneBreak.MatchString(stripped) {
			out = append(out, `\par\bigskip\begin{center}{\fontsize{10pt}{12pt}\selectfont\textcolor{diaggray}{$\ast\quad\ast\quad\ast$}}\end{center}\par\bigskip`)
			i++
			continue
		}
		if stripped == "**Diagram:**" || stripped == "- **Diagram:**" || stripped == "**Visualization (Mermaid):**" {
			i++
			continue
		}

		// Display Math Block ($$ ... $$ or $$ ... $$ (eq:label))
		if strings.HasPrefix(stripped, "$$") {
			if strings.HasSuffix(stripped, "$$") && len(stripped) > 4 {
				mathBody := strings.TrimSpace(stripped[2 : len(stripped)-2])
				out = append(out, formatDisplayMath(mathBody))
				i++
				continue
			}
			var mathLines []string
			if len(stripped) > 2 {
				mathLines = append(mathLines, stripped[2:])
			}
			i++
			for i < n {
				curS := strings.TrimSpace(lines[i])
				if strings.HasPrefix(curS, "$$") || strings.HasSuffix(curS, "$$") {
					lastClean := strings.TrimSpace(strings.Split(curS, "$$")[0])
					if lastClean != "" {
						mathLines = append(mathLines, lastClean)
					}
					i++
					break
				}
				mathLines = append(mathLines, lines[i])
				i++
			}
			out = append(out, formatDisplayMath(strings.Join(mathLines, "\n")))
			continue
		}

		// Standalone Image Figure ![Caption](path) or ![Caption](<path with spaces>)
		if m := reMDImage.FindStringSubmatch(stripped); len(m) > 0 {
			alt := strings.TrimSpace(m[1])
			imgPath := strings.TrimSpace(m[2])
			if imgPath == "" && len(m) > 3 {
				imgPath = strings.TrimSpace(m[3])
			}
			resolvedName := resolveAndStageImage(imgPath, cfg, docPath)
			if resolvedName != "" {
				figCounter++
				capTex := FormatInline(alt, cfg)
				if chNum > 0 {
					if capTex != "" {
						capTex = fmt.Sprintf("Figure %d.%d. %s", chNum, figCounter, capTex)
					} else {
						capTex = fmt.Sprintf("Figure %d.%d.", chNum, figCounter)
					}
				}
				out = append(out, tikz.WrapFigureTex(fmt.Sprintf(`\includegraphics{%s}`, resolvedName), capTex))
			}
			i++
			continue
		}

		// MyST Directive Block (```{kind} args / :::{kind} args) or Pandoc/Quarto/Carpentries bare colon div (::: kind)
		dirMatch := reMystDirective.FindStringSubmatch(stripped)
		if len(dirMatch) == 0 {
			dirMatch = reBareColonDiv.FindStringSubmatch(stripped)
		}
		if len(dirMatch) > 0 {
			fenceMarker := dirMatch[1]
			fenceChar := fenceMarker[0]
			fenceLen := len(fenceMarker)
			kind := strings.ToLower(strings.TrimSpace(dirMatch[2]))
			arg := strings.TrimSpace(dirMatch[3])

			var bodyLines []string
			i++
			depth := 1
			inInnerFence := false
			for i < n {
				curS := strings.TrimSpace(lines[i])
				if fenceChar == ':' && strings.HasPrefix(curS, "```") {
					inInnerFence = !inInnerFence
				}
				if !inInnerFence && fenceChar == ':' {
					if reMystDirective.MatchString(curS) || reBareColonDiv.MatchString(curS) {
						depth++
						if mSub := reBareColonDiv.FindStringSubmatch(curS); len(mSub) > 2 {
							bodyLines = append(bodyLines, "**"+strings.Title(strings.ReplaceAll(mSub[2], "-", " "))+":**")
							i++
							continue
						}
					}
				}
				isCloseFence := !inInnerFence && len(curS) >= 3 && strings.Trim(curS, string(fenceChar)) == "" && (fenceChar != '`' || len(curS) >= fenceLen)
				if isCloseFence {
					depth--
					i++
					if depth <= 0 {
						break
					}
					continue
				}
				bodyLines = append(bodyLines, lines[i])
				i++
			}

			// Parse optional MyST `:key: val` header lines
			opts := make(map[string]string)
			bodyStart := 0
			for bodyStart < len(bodyLines) {
				tr := strings.TrimSpace(bodyLines[bodyStart])
				if tr == "" && len(opts) > 0 {
					bodyStart++
					break
				}
				if strings.HasPrefix(tr, ":") && strings.Count(tr, ":") >= 2 {
					rest := tr[1:]
					cIdx := strings.IndexByte(rest, ':')
					if cIdx > 0 {
						k := strings.TrimSpace(rest[:cIdx])
						v := strings.TrimSpace(rest[cIdx+1:])
						opts[k] = v
						bodyStart++
						continue
					}
				}
				break
			}
			dirBody := strings.TrimSpace(strings.Join(bodyLines[bodyStart:], "\n"))

			switch kind {
			case "raw", "index", "only", "tableofcontents", "bibliography", "nb-exec-table":
				// omit web/metadata-only directives
			case "code-cell", "code-block":
				lang := strings.ToLower(arg)
				if lang == "" || lang == "ipython" || lang == "ipython3" {
					lang = "python"
				}
				cleanBody := strings.TrimSpace(reFrontmatter.ReplaceAllString(dirBody, ""))
				out = append(out, highlight.HighlightBlock(lang, cleanBody))
			case "math":
				if dirBody != "" {
					out = append(out, formatDisplayMath(dirBody))
				}
			case "figure", "image":
				resolvedName := resolveAndStageImage(arg, cfg, docPath)
				if resolvedName != "" {
					figCounter++
					alt := firstNonEmpty(dirBody, opts["alt"])
					capTex := FormatInline(alt, cfg)
					if chNum > 0 {
						if capTex != "" {
							capTex = fmt.Sprintf("Figure %d.%d. %s", chNum, figCounter, capTex)
						} else {
							capTex = fmt.Sprintf("Figure %d.%d.", chNum, figCounter)
						}
					}
					out = append(out, tikz.WrapFigureTex(fmt.Sprintf(`\includegraphics{%s}`, resolvedName), capTex))
				}
			case "epigraph":
				innerTex, fCnt := CompileChapter(dirBody, 0, cfg)
				figCounter += fCnt
				out = append(out, fmt.Sprintf("\\begin{quote}{\\itshape\\color{diaggray}%s}\n\\end{quote}", strings.TrimSpace(innerTex)))
			default:
				// Admonitions / proofs / exercises / notes / tips / warnings
				label := strings.ToUpper(strings.TrimPrefix(kind, "prf:"))
				if arg != "" {
					label = label + ": " + arg
				}
				env, col := "notebox", "noteaccent"
				if kind == "tip" || kind == "seealso" || kind == "solution" || kind == "keypoints" || kind == "callout-tip" {
					env, col = "tipbox", "tipaccent"
				} else if kind == "warning" || kind == "important" || kind == "caution" || kind == "callout-warning" || kind == "callout-important" {
					env, col = "importantbox", "impaccent"
				}
				innerTex, fCnt := CompileChapter(dirBody, 0, cfg)
				figCounter += fCnt
				out = append(out, fmt.Sprintf("\\Needspace{6\\baselineskip}\\begin{%s}\\sloppy\\noindent{\\sansbold\\fontsize{8.5pt}{10.5pt}\\selectfont\\textcolor{%s}{%s}}\\par\\smallskip\\noindent %s\n\\end{%s}",
					env, col, FormatInline(label, cfg), strings.TrimSpace(innerTex), env))
			}
			continue
		}

		// Standard Fenced Code or Diagram Block (```lang)
		if strings.HasPrefix(stripped, "```") {
			lang := strings.TrimSpace(stripped[3:])
			var bodyLines []string
			i++
			for i < n && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				bodyLines = append(bodyLines, lines[i])
				i++
			}
			i++ // skip closing ```
			body := strings.Join(bodyLines, "\n")

			if lang == "mermaid" {
				figCounter++
				cap := ""
				if chNum > 0 {
					cap = fmt.Sprintf("Figure %d.%d.", chNum, figCounter)
				}
				out = append(out, tikz.WrapFigure(tikz.RenderMermaid(body), cap))
				continue
			} else if lang == "" && tikz.IsASCIIDiagram(body) {
				figCounter++
				cap := ""
				if chNum > 0 {
					cap = fmt.Sprintf("Figure %d.%d.", chNum, figCounter)
				}
				out = append(out, tikz.WrapFigure(tikz.RenderASCIIDiagram(body), cap))
				continue
			} else {
				out = append(out, highlight.HighlightBlock(lang, body))
				continue
			}
		}

		// Custom MDX Callouts: <DeepDive>, <Warning>, <Axiom>
		if m := reDeepDive.FindStringSubmatch(stripped); len(m) > 0 {
			title := m[1]
			if title == "" {
				title = "Deep Dive"
			}
			bodyTex := formatCalloutBody(strings.TrimSpace(m[2]), cfg)
			out = append(out, fmt.Sprintf("\\Needspace{4\\baselineskip}\\begin{notebox}\\sloppy\\noindent{\\sansbold\\fontsize{8.5pt}{10.5pt}\\selectfont\\textcolor{noteaccent}{DEEP DIVE: %s}}\\par\\smallskip\\noindent %s\n\\end{notebox}",
				FormatInline(title, cfg), bodyTex))
			i++
			continue
		}
		if m := reWarning.FindStringSubmatch(stripped); len(m) > 0 {
			title := m[1]
			if title == "" {
				title = "Warning"
			}
			bodyTex := formatCalloutBody(strings.TrimSpace(m[2]), cfg)
			out = append(out, fmt.Sprintf("\\Needspace{4\\baselineskip}\\begin{importantbox}\\sloppy\\noindent{\\sansbold\\fontsize{8.5pt}{10.5pt}\\selectfont\\textcolor{impaccent}{WARNING: %s}}\\par\\smallskip\\noindent %s\n\\end{importantbox}",
				FormatInline(title, cfg), bodyTex))
			i++
			continue
		}
		if m := reAxiom.FindStringSubmatch(stripped); len(m) > 0 {
			bodyTex := FormatInline(strings.TrimSpace(m[1]), cfg)
			out = append(out, fmt.Sprintf(`\Needspace{3\baselineskip}\begin{tipbox}\sloppy\noindent{\itshape %s}\end{tipbox}`, bodyTex))
			i++
			continue
		}

		// Headings (# .. ######)
		if m := reHeading.FindStringSubmatch(stripped); len(m) > 0 {
			lvl := len(m[1])
			hTxt := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[2]), "(Mermaid)"))
			if lvl == 1 {
				i++
				continue
			} else if lvl == 2 {
				out = append(out, fmt.Sprintf(`\section*{%s}`, FormatInline(hTxt, cfg)))
				i++
				continue
			} else {
				out = append(out, fmt.Sprintf(`\subsection*{%s}`, FormatInline(hTxt, cfg)))
				i++
				continue
			}
		}

		// Markdown Table
		if strings.Count(stripped, "|") >= 2 && strings.HasPrefix(stripped, "|") && strings.HasSuffix(stripped, "|") {
			var tblLines []string
			for i < n {
				s := strings.TrimSpace(lines[i])
				if strings.Count(s, "|") >= 2 && strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") {
					tblLines = append(tblLines, lines[i])
					i++
				} else {
					break
				}
			}
			noH := nextTableNoHead
			nextTableNoHead = false
			out = append(out, RenderTable(tblLines, cfg, noH))
			continue
		}
		nextTableNoHead = false

		// Blockquotes & GitHub Alerts (`> [!NOTE] ...`)
		if strings.HasPrefix(stripped, ">") {
			var bqLines []string
			inBQMath := false
			for i < n {
				curT := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(curT, ">") {
					if !inBQMath {
						break
					}
					bqLines = append(bqLines, lines[i])
					if strings.HasPrefix(curT, "$$") || strings.HasSuffix(curT, "$$") {
						inBQMath = false
					}
					i++
					continue
				}
				rawBQ := strings.TrimPrefix(curT, ">")
				if strings.HasPrefix(rawBQ, " ") {
					rawBQ = rawBQ[1:]
				}
				trBQ := strings.TrimSpace(rawBQ)
				if strings.HasPrefix(trBQ, "$$") && !(strings.HasSuffix(trBQ, "$$") && len(trBQ) > 4) {
					inBQMath = !inBQMath
				}
				bqLines = append(bqLines, rawBQ)
				i++
			}
			if len(bqLines) > 0 {
				if m := reGHAlert.FindStringSubmatch(strings.TrimSpace(bqLines[0])); len(m) > 0 {
					kind := strings.ToUpper(m[1])
					rest := append([]string{m[2]}, bqLines[1:]...)
					bodyTex := formatCalloutBody(strings.TrimSpace(strings.Join(rest, " ")), cfg)
					env, col := "notebox", "noteaccent"
					if kind == "TIP" {
						env, col = "tipbox", "tipaccent"
					} else if kind == "WARNING" || kind == "CAUTION" || kind == "IMPORTANT" {
						env, col = "importantbox", "impaccent"
					}
					out = append(out, fmt.Sprintf("\\Needspace{4\\baselineskip}\\begin{%s}\\sloppy\\noindent{\\sansbold\\fontsize{8.5pt}{10.5pt}\\selectfont\\textcolor{%s}{%s}}\\par\\smallskip\\noindent %s\n\\end{%s}",
						env, col, kind, bodyTex, env))
					continue
				}
			}
			bqText := strings.TrimSpace(strings.Join(bqLines, "\n"))
			if m := reBoldCallout.FindStringSubmatch(bqText); len(m) > 0 {
				label := strings.ToUpper(m[1])
				rest := strings.TrimSpace(m[2])
				env, col := "notebox", "noteaccent"
				if label == "TIP" || label == "KEY TAKEAWAY" {
					env, col = "tipbox", "tipaccent"
				} else if label == "WARNING" || label == "IMPORTANT" || label == "CAUTION" {
					env, col = "importantbox", "impaccent"
				}
				out = append(out, fmt.Sprintf("\\Needspace{4\\baselineskip}\\begin{%s}\\sloppy\\noindent{\\sansbold\\fontsize{8.5pt}{10.5pt}\\selectfont\\textcolor{%s}{%s}}\\par\\smallskip\\noindent %s\n\\end{%s}",
					env, col, FormatInline(label, cfg), formatCalloutBody(rest, cfg), env))
			} else if strings.Contains(bqText, "```") {
				out = append(out, fmt.Sprintf("\\Needspace{3\\baselineskip}\\begin{notebox}\\sloppy\\noindent %s\n\\end{notebox}", formatCalloutBody(bqText, cfg)))
			} else {
				bqTex, _ := CompileChapter(bqText, 0, cfg)
				out = append(out, fmt.Sprintf("\\begin{quote}{\\itshape\\color{diaggray}%s}\n\\end{quote}", strings.TrimSpace(bqTex)))
			}
			continue
		}

		// Lists (`- `, `* `, `1. `)
		if reListItem.MatchString(line) {
			type lItem struct {
				indent  int
				ordered bool
				text    string
			}
			var items []lItem
			for i < n {
				cur := lines[i]
				if strings.TrimSpace(cur) == "**Diagram:**" || strings.TrimSpace(cur) == "- **Diagram:**" {
					break
				}
				ml := reListItem.FindStringSubmatch(cur)
				if len(ml) == 0 {
					break
				}
				ind := 0
				if len(ml[1]) >= 2 {
					ind = 1
				}
				isOrd := strings.HasSuffix(ml[2], ".")
				parts := []string{strings.TrimSpace(ml[3])}
				i++
				for i < n {
					nxt := lines[i]
					ns := strings.TrimSpace(nxt)
					if ns == "" || strings.HasPrefix(ns, "```") || strings.HasPrefix(ns, ":::") || strings.HasPrefix(ns, "#") || strings.HasPrefix(ns, "|") || strings.HasPrefix(ns, ">") || strings.HasPrefix(ns, "<") || reListItem.MatchString(nxt) || reHR.MatchString(ns) {
						break
					}
					parts = append(parts, ns)
					i++
				}
				items = append(items, lItem{indent: ind, ordered: isOrd, text: strings.Join(parts, " ")})
			}

			var envStack []string
			for _, it := range items {
				env := "itemize"
				if it.ordered {
					env = "enumerate"
				}
				effIndent := it.indent
				if effIndent > len(envStack) {
					effIndent = len(envStack)
				}
				targetDepth := effIndent + 1
				for len(envStack) > targetDepth {
					top := envStack[len(envStack)-1]
					envStack = envStack[:len(envStack)-1]
					out = append(out, fmt.Sprintf(`\end{%s}`, top))
				}
				if len(envStack) == targetDepth && envStack[len(envStack)-1] != env {
					top := envStack[len(envStack)-1]
					envStack = envStack[:len(envStack)-1]
					out = append(out, fmt.Sprintf(`\end{%s}`, top))
				}
				for len(envStack) < targetDepth {
					out = append(out, fmt.Sprintf(`\begin{%s}`, env))
					envStack = append(envStack, env)
				}
				out = append(out, "  \\item "+FormatInline(it.text, cfg))
			}
			for len(envStack) > 0 {
				top := envStack[len(envStack)-1]
				envStack = envStack[:len(envStack)-1]
				out = append(out, fmt.Sprintf(`\end{%s}`, top))
			}
			continue
		}

		// Regular Paragraph
		paraLines := []string{line}
		i++
		for i < n {
			nxt := lines[i]
			ns := strings.TrimSpace(nxt)
			if ns == "" || strings.HasPrefix(ns, "```") || strings.HasPrefix(ns, ":::") || strings.HasPrefix(ns, "$$") || strings.HasPrefix(ns, "#") || (strings.Count(ns, "|") >= 2 && strings.HasPrefix(ns, "|") && strings.HasSuffix(ns, "|")) || strings.HasPrefix(ns, ">") || strings.HasPrefix(ns, "<DeepDive") || strings.HasPrefix(ns, "<Warning") || strings.HasPrefix(ns, "<Axiom") || reListItem.MatchString(nxt) || reHR.MatchString(ns) || reMystTarget.MatchString(ns) || ns == "**Diagram:**" || ns == "- **Diagram:**" {
				break
			}
			paraLines = append(paraLines, nxt)
			i++
		}

		// Join paragraph lines preserving explicit hard linebreaks (`  \n`)
		var paraSB strings.Builder
		for idx, pl := range paraLines {
			if idx > 0 {
				if strings.HasSuffix(paraLines[idx-1], "  ") {
					paraSB.WriteString("\n")
				} else {
					paraSB.WriteByte(' ')
				}
			}
			paraSB.WriteString(strings.TrimLeft(pl, " \t"))
		}
		joined := strings.TrimSpace(paraSB.String())
		if len(paraLines) >= 2 && reBoldQ.MatchString(strings.TrimSpace(paraLines[0])) {
			qTex := FormatInline(strings.TrimSpace(paraLines[0]), cfg)
			aTex := FormatInline(strings.Join(paraLines[1:], " "), cfg)
			out = append(out, fmt.Sprintf(`\par\smallskip\noindent %s\par\nopagebreak[4]\noindent %s\par`, qTex, aTex))
		} else if len(paraLines) == 1 && strings.HasSuffix(strings.TrimSpace(paraLines[0]), ":") {
			out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\noindent %s\par\nopagebreak[4]`, FormatInline(joined, cfg)))
		} else {
			out = append(out, FormatInline(joined, cfg))
		}
	}

	return strings.Join(out, "\n"), figCounter
}

func formatDisplayMath(body string) string {
	var cleanLines []string
	for _, ln := range strings.Split(body, "\n") {
		if strings.TrimSpace(ln) != "" {
			cleanLines = append(cleanLines, highlight.NormalizeMathUnicode(ln))
		}
	}
	if len(cleanLines) == 0 {
		return ""
	}
	joined := strings.Join(cleanLines, "\n")
	trimmed := strings.TrimSpace(joined)
	for _, env := range []string{
		`\begin{equation`, `\begin{align`, `\begin{gather`, `\begin{multline`, `\begin{eqnarray`, `\begin{flalign`,
	} {
		// Standalone math environments cannot sit inside \[...\] (note \begin{aligned} IS excluded because alignat/aligned check)
		if strings.HasPrefix(trimmed, env) && !strings.HasPrefix(trimmed, `\begin{aligned}`) {
			return joined
		}
	}
	return "\\[\n" + joined + "\n\\]"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ExtractChapterIndexHits automatically discovers defined concepts (`- **Term:**`)
// across any subject domain, as well as code symbols (`pkg.Symbol`) when present.
func ExtractChapterIndexHits(rawText string) []IndexHit {
	seen := make(map[string]bool)
	var hits []IndexHit

	for _, m := range reBoldTerm.FindAllStringSubmatch(rawText, -1) {
		term := strings.TrimSpace(strings.TrimSuffix(m[1], ":"))
		if len(term) < 3 || len(term) > 34 || strings.ContainsAny(term, "?\"'`") {
			continue
		}
		lower := strings.ToLower(term)
		if lower == "example" || lower == "note" || lower == "tip" || lower == "warning" || lower == "summary" || lower == "diagram" || lower == "analogy" {
			continue
		}
		if !seen[lower] {
			seen[lower] = true
			hits = append(hits, IndexHit{Category: term, Subterm: ""})
		}
	}

	for _, m := range rePkgSymbol.FindAllStringSubmatch(rawText, -1) {
		pkg, sym := m[1], m[2]
		if pkg == "err" || pkg == "req" || pkg == "resp" || pkg == "ctx" || pkg == "srv" || pkg == "conn" || pkg == "ln" || pkg == "db" || pkg == "tx" || pkg == "c" || pkg == "r" || pkg == "w" || pkg == "s" || pkg == "b" {
			continue
		}
		cat := pkg + " package"
		sub := pkg + "." + sym
		key := cat + "|" + sub
		if !seen[key] {
			seen[key] = true
			hits = append(hits, IndexHit{Category: cat, Subterm: sub})
		}
	}

	return hits
}
