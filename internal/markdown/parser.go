package markdown

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/highlight"
	"github.com/Oyetomi/folio/internal/tikz"
)

type ChapterDoc struct {
	IDTuple  [3]int
	HasID    bool
	PartIdx  int
	Title    string
	FilePath string
	RawText  string
}

type IndexHit struct {
	Category string
	Subterm  string
}

var (
	reFrontmatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n*`)
	reFMID        = regexp.MustCompile(`(?m)^id:\s*"?\[(\d+)\.(\d+)\.(\d+)\]"?`)
	reFMTitle     = regexp.MustCompile(`(?m)^title:\s*"?([^"\n]+)"?`)
	reH1Title     = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	reInlineCode  = regexp.MustCompile("`([^`\\n]+)`")
	reInlineMath  = regexp.MustCompile(`\$([^$\n]+)\$`)
	reCamelBreak  = regexp.MustCompile(`([a-z])([A-Z])`)
	reMDImage     = regexp.MustCompile(`^!\[([^\]]*)\]\((?:<([^>]+)>|([^)]+))\)$`)
	reMDImageAny  = regexp.MustCompile(`!\[([^\]]*)\]\((?:<([^>]+)>|([^)]+))\)`)
	reHTMLAnchor  = regexp.MustCompile(`(?i)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	reHTMLU       = regexp.MustCompile(`(?i)<u>(.*?)</u>`)
	reHTMLSummary = regexp.MustCompile(`(?i)<summary>(.*?)</summary>`)
	reHTMLTagRem  = regexp.MustCompile(`(?i)</?(?:details|p|div|img)\b[^>]*>`)
	reMDLink       = regexp.MustCompile(`\[([^\]]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)`)
	reBoldItalic   = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold         = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic       = regexp.MustCompile(`(?:^|[^*])\*([^*\n]+)\*`)
	reItalicUnder  = regexp.MustCompile(`(^|[\s(])_([^_\n]+)_`)
	reStraightQuot = regexp.MustCompile(`"([^"\n]+)"`)
	reEmojiShort   = regexp.MustCompile(`:(?:coffee|shrug|smile|wink|tada|rocket|warning|bulb|memo|book):`)
	reHR           = regexp.MustCompile(`^-{3,}$`)
	reSceneBreak   = regexp.MustCompile(`^(?:\*\s*\*\s*\*|\*{3,})$`)
	reHeading      = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reListItem    = regexp.MustCompile(`^(\s*)([-*]|\d+\.)\s+(.*)$`)
	reSepCell     = regexp.MustCompile(`^:?-+:?$`)
	reDeepDive    = regexp.MustCompile(`^<(?:DeepDive|Note|Callout)(?:\s+title="([^"]*)")?\s*>(.*)</(?:DeepDive|Note|Callout)>$`)
	reWarning     = regexp.MustCompile(`^<(?:Warning|Important|Caution)(?:\s+title="([^"]*)")?\s*>(.*)</(?:Warning|Important|Caution)>$`)
	reAxiom       = regexp.MustCompile(`^<(?:Axiom|Tip|Quote)>(.*)</(?:Axiom|Tip|Quote)>$`)
	reGHAlert     = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION|QUOTE)\]\s*(.*)$`)
	reBoldCallout = regexp.MustCompile(`^\*\*(Note|Tip|Warning|Important|Caution|Analogy|Key Takeaway|Definition|Example):\*\*\s*(.+)$`)
	reBoldQ       = regexp.MustCompile(`^\*\*.*\?\*\*$`)
	reInlineFence = regexp.MustCompile("```([a-zA-Z0-9_-]*)\\s+(.*?)\\s*```")
	rePkgSymbol   = regexp.MustCompile("`([a-z][a-z0-9_]{1,14})\\.([A-Z][A-Za-z0-9_]{1,28})`")
	reBoldTerm    = regexp.MustCompile(`(?m)^\s*[-*]\s+\*\*([A-Za-z][A-Za-z0-9 /()-]{2,34}?)(?::|\*\*\s*[—–-])`)
	reSummaryItem = regexp.MustCompile(`^\s*[-*]\s+\[([^\]]+)\]\(([^)]+)\)`)
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

func DiscoverChapters(rootDir string, cfg *config.BookConfig) ([]ChapterDoc, error) {
	// 1. Check for GitBook / mdBook SUMMARY.md first
	for _, sumCand := range []string{filepath.Join(rootDir, "SUMMARY.md"), filepath.Join(rootDir, "src", "SUMMARY.md")} {
		if sumData, err := os.ReadFile(sumCand); err == nil {
			sumDir := filepath.Dir(sumCand)
			var docs []ChapterDoc
			partIdx := 0
			chIdx := 0
			for _, line := range strings.Split(string(sumData), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "## ") {
					partIdx++
					continue
				}
				if m := reSummaryItem.FindStringSubmatch(line); len(m) == 3 {
					linkTitle := strings.TrimSpace(m[1])
					relPath := strings.TrimSpace(m[2])
					if strings.EqualFold(filepath.Base(relPath), "contributing.md") || strings.EqualFold(filepath.Base(relPath), "template.md") {
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
					docs = append(docs, ChapterDoc{
						IDTuple:  [3]int{partIdx, chIdx, 0},
						HasID:    true,
						PartIdx:  partIdx,
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

	srcDir := filepath.Join(rootDir, cfg.Source)
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		srcDir = rootDir
	}

	var mdxFiles []string
	var mdFiles []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "website" || name == "exercises" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".mdx" {
			mdxFiles = append(mdxFiles, path)
		} else if ext == ".md" && !strings.EqualFold(d.Name(), "README.md") && !strings.EqualFold(d.Name(), "SUMMARY.md") && !strings.EqualFold(d.Name(), "CLAUDE.md") && !strings.EqualFold(d.Name(), "AGENTS.md") {
			mdFiles = append(mdFiles, path)
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

	var docs []ChapterDoc
	for _, fp := range files {
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

	// 1b. Tokenize inline LaTeX math ($...$) when it looks like a math expression (not currency like $100)
	text = reInlineMath.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1]
		if len(inner) > 0 && inner[0] >= '0' && inner[0] <= '9' && !strings.ContainsAny(inner, "+-=*/^_\\") {
			return match
		}
		return saveToken("$" + inner + "$")
	})

	// 1c. Normalize inline HTML tags (<a href="...">, <summary>, <u>, <details>) outside code spans
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
		isBoldLbl := false
		if strings.HasPrefix(lbl, "**") && strings.HasSuffix(lbl, "**") && len(lbl) > 4 {
			lbl = strings.TrimSpace(lbl[2 : len(lbl)-2])
			isBoldLbl = true
		}
		lblTex := escSub(lbl)
		if isBoldLbl {
			lblTex = fmt.Sprintf(`\textbf{%s}`, lblTex)
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
			return saveToken(fmt.Sprintf(`\href{%s}{\sanssemiboldcond\fontsize{8.2pt}{10pt}\selectfont\textcolor{brandcrimson}{[%s $\rightarrow$]}}`, safeURL, escSub(lbl)))
		}
		return saveToken(fmt.Sprintf(`\textbf{%s}`, escSub(lbl)))
	})

	// 3. Bold-Italic ***...***, Bold **...**, and Italic *...* / _..._
	text = reBoldItalic.ReplaceAllStringFunc(text, func(match string) string {
		sub := reBoldItalic.FindStringSubmatch(match)
		return saveToken(fmt.Sprintf(`\textbf{\textit{%s}}`, escSub(sub[1])))
	})
	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		sub := reBold.FindStringSubmatch(match)
		inner := sub[1]
		inner = reItalic.ReplaceAllStringFunc(inner, func(im string) string {
			isub := reItalic.FindStringSubmatch(im)
			pref := ""
			if !strings.HasPrefix(im, "*") {
				pref = im[:1]
			}
			return pref + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(isub[1])))
		})
		inner = reItalicUnder.ReplaceAllStringFunc(inner, func(im string) string {
			isub := reItalicUnder.FindStringSubmatch(im)
			return isub[1] + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(isub[2])))
		})
		return saveToken(fmt.Sprintf(`\textbf{%s}`, escSub(inner)))
	})
	text = reItalic.ReplaceAllStringFunc(text, func(match string) string {
		sub := reItalic.FindStringSubmatch(match)
		prefix := ""
		if !strings.HasPrefix(match, "*") {
			prefix = match[:1]
		}
		return prefix + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(sub[1])))
	})
	text = reItalicUnder.ReplaceAllStringFunc(text, func(match string) string {
		sub := reItalicUnder.FindStringSubmatch(match)
		return sub[1] + saveToken(fmt.Sprintf(`\textit{%s}`, escSub(sub[2])))
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

func escapeHrefURL(u string) string {
	u = strings.ReplaceAll(u, "%", `\%`)
	u = strings.ReplaceAll(u, "#", `\#`)
	u = strings.ReplaceAll(u, "_", `\_`)
	u = strings.ReplaceAll(u, "&", `\&`)
	return u
}

func RenderTable(tableLines []string, cfg *config.BookConfig) string {
	var rows [][]string
	for _, line := range tableLines {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
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
	if len(rows) < 2 {
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
		mw := math.Max(0.58, math.Max(float64(maxWord[i])*0.056+0.14, float64(monoMaxWord[i])*0.068+0.16))
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
	out = append(out, `\rowcolor{tableheadbg}`)

	hdrCells := make([]string, numCols)
	for i, c := range rows[0] {
		hdrCells[i] = fmt.Sprintf(`\sansbold\fontsize{8.3pt}{10.5pt}\selectfont\textcolor{white}{%s}`, FormatInline(c, cfg))
	}
	out = append(out, strings.Join(hdrCells, " & ")+` \\ \endhead`)

	for rIdx, r := range rows[1:] {
		bg := ""
		if (rIdx+1)%2 == 0 {
			bg = `\rowcolor{tablerowalt} `
		}
		bCells := make([]string, numCols)
		for i, c := range r {
			bCells[i] = FormatInline(c, cfg)
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
	// Expand single-line Go blocks into readable multi-line blocks
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

func CompileChapter(mdxText string, chNum int, cfg *config.BookConfig) (string, int) {
	content := reFrontmatter.ReplaceAllString(mdxText, "")
	lines := strings.Split(content, "\n")

	var out []string
	figCounter := 0
	i := 0
	n := len(lines)

	for i < n {
		line := lines[i]
		stripped := strings.TrimSpace(line)

		if stripped == "" {
			out = append(out, "")
			i++
			continue
		}
		if reHR.MatchString(stripped) {
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

		// Display Math Block ($$ ... $$)
		if strings.HasPrefix(stripped, "$$") {
			if strings.HasSuffix(stripped, "$$") && len(stripped) > 4 {
				mathBody := strings.TrimSpace(stripped[2 : len(stripped)-2])
				out = append(out, "\\[\n"+mathBody+"\n\\]")
				i++
				continue
			}
			var mathLines []string
			if len(stripped) > 2 {
				mathLines = append(mathLines, stripped[2:])
			}
			i++
			for i < n && !strings.HasSuffix(strings.TrimSpace(lines[i]), "$$") {
				mathLines = append(mathLines, lines[i])
				i++
			}
			if i < n {
				lastLine := strings.TrimSuffix(strings.TrimSpace(lines[i]), "$$")
				if lastLine != "" {
					mathLines = append(mathLines, lastLine)
				}
				i++
			}
			out = append(out, "\\[\n"+strings.Join(mathLines, "\n")+"\n\\]")
			continue
		}

		// Standalone Image Figure ![Caption](path) or ![Caption](<path with spaces>)
		if m := reMDImage.FindStringSubmatch(stripped); len(m) > 0 {
			alt := strings.TrimSpace(m[1])
			imgPath := strings.TrimSpace(m[2])
			if imgPath == "" && len(m) > 3 {
				imgPath = strings.TrimSpace(m[3])
			}
			resolvedName := resolveAndStageImage(imgPath, cfg.WorkDir)
			if resolvedName != "" {
				figCounter++
				cap := alt
				if chNum > 0 {
					if alt != "" {
						cap = fmt.Sprintf("Figure %d.%d. %s", chNum, figCounter, FormatInline(alt, cfg))
					} else {
						cap = fmt.Sprintf("Figure %d.%d.", chNum, figCounter)
					}
				}
				out = append(out, tikz.WrapFigure(fmt.Sprintf(`\includegraphics[width=4.6in,height=4.8in,keepaspectratio]{%s}`, resolvedName), cap))
			}
			i++
			continue
		}

		// Fenced code or diagram block
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
			out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\begin{notebox}\sloppy\noindent{\sansbold\fontsize{8.5pt}{10.5pt}\selectfont\textcolor{noteaccent}{DEEP DIVE: %s}}\par\smallskip\noindent %s\end{notebox}`,
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
			out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\begin{importantbox}\sloppy\noindent{\sansbold\fontsize{8.5pt}{10.5pt}\selectfont\textcolor{impaccent}{WARNING: %s}}\par\smallskip\noindent %s\end{importantbox}`,
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

		// Headings (#, ##, ###)
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
		if strings.HasPrefix(stripped, "|") && strings.HasSuffix(stripped, "|") {
			var tblLines []string
			for i < n {
				s := strings.TrimSpace(lines[i])
				if strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") {
					tblLines = append(tblLines, lines[i])
					i++
				} else {
					break
				}
			}
			out = append(out, RenderTable(tblLines, cfg))
			continue
		}

		// Blockquotes & GitHub Alerts (`> [!NOTE] ...`)
		if strings.HasPrefix(stripped, ">") {
			var bqLines []string
			for i < n && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">"))
				bqLines = append(bqLines, clean)
				i++
			}
			if len(bqLines) > 0 {
				if m := reGHAlert.FindStringSubmatch(bqLines[0]); len(m) > 0 {
					kind := strings.ToUpper(m[1])
					rest := append([]string{m[2]}, bqLines[1:]...)
					bodyTex := formatCalloutBody(strings.TrimSpace(strings.Join(rest, " ")), cfg)
					env, col := "notebox", "noteaccent"
					if kind == "TIP" {
						env, col = "tipbox", "tipaccent"
					} else if kind == "WARNING" || kind == "CAUTION" || kind == "IMPORTANT" {
						env, col = "importantbox", "impaccent"
					}
					out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\begin{%s}\sloppy\noindent{\sansbold\fontsize{8.5pt}{10.5pt}\selectfont\textcolor{%s}{%s}}\par\smallskip\noindent %s\end{%s}`,
						env, col, kind, bodyTex, env))
					continue
				}
			}
			bqText := strings.TrimSpace(strings.Join(bqLines, " "))
			if m := reBoldCallout.FindStringSubmatch(bqText); len(m) > 0 {
				label := strings.ToUpper(m[1])
				rest := strings.TrimSpace(m[2])
				env, col := "notebox", "noteaccent"
				if label == "TIP" || label == "KEY TAKEAWAY" {
					env, col = "tipbox", "tipaccent"
				} else if label == "WARNING" || label == "IMPORTANT" || label == "CAUTION" {
					env, col = "importantbox", "impaccent"
				}
				out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\begin{%s}\sloppy\noindent{\sansbold\fontsize{8.5pt}{10.5pt}\selectfont\textcolor{%s}{%s}}\par\smallskip\noindent %s\end{%s}`,
					env, col, FormatInline(label, cfg), formatCalloutBody(rest, cfg), env))
			} else if strings.Contains(bqText, "```") {
				out = append(out, fmt.Sprintf(`\Needspace{3\baselineskip}\begin{notebox}\sloppy\noindent %s\end{notebox}`, formatCalloutBody(bqText, cfg)))
			} else {
				out = append(out, fmt.Sprintf(`\begin{quote}\itshape\textcolor{diaggray}{%s}\end{quote}`, FormatInline(bqText, cfg)))
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
					if ns == "" || strings.HasPrefix(ns, "```") || strings.HasPrefix(ns, "#") || strings.HasPrefix(ns, "|") || strings.HasPrefix(ns, ">") || strings.HasPrefix(ns, "<") || reListItem.MatchString(nxt) || reHR.MatchString(ns) {
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
		paraLines := []string{stripped}
		i++
		for i < n {
			nxt := lines[i]
			ns := strings.TrimSpace(nxt)
			if ns == "" || strings.HasPrefix(ns, "```") || strings.HasPrefix(ns, "#") || (strings.HasPrefix(ns, "|") && strings.HasSuffix(ns, "|")) || strings.HasPrefix(ns, ">") || strings.HasPrefix(ns, "<DeepDive") || strings.HasPrefix(ns, "<Warning") || strings.HasPrefix(ns, "<Axiom") || reListItem.MatchString(nxt) || reHR.MatchString(ns) || ns == "**Diagram:**" || ns == "- **Diagram:**" {
				break
			}
			paraLines = append(paraLines, ns)
			i++
		}

		if len(paraLines) >= 2 && reBoldQ.MatchString(paraLines[0]) {
			qTex := FormatInline(paraLines[0], cfg)
			aTex := FormatInline(strings.Join(paraLines[1:], " "), cfg)
			out = append(out, fmt.Sprintf(`\par\smallskip\noindent %s\par\nopagebreak[4]\noindent %s\par`, qTex, aTex))
		} else if len(paraLines) == 1 && strings.HasSuffix(paraLines[0], ":") {
			out = append(out, fmt.Sprintf(`\Needspace{4\baselineskip}\noindent %s\par\nopagebreak[4]`, FormatInline(paraLines[0], cfg)))
		} else {
			out = append(out, FormatInline(strings.Join(paraLines, " "), cfg))
		}
	}

	return strings.Join(out, "\n"), figCounter
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

func resolveAndStageImage(imgPath string, workDir string) string {
	if workDir == "" || imgPath == "" {
		return ""
	}
	// Skip CI badges
	if strings.Contains(imgPath, "goreportcard.com") || strings.Contains(imgPath, "shields.io") || strings.Contains(imgPath, "travis-ci") {
		return ""
	}
	// Handle remote PNG/JPG URLs
	if strings.HasPrefix(imgPath, "http://") || strings.HasPrefix(imgPath, "https://") {
		ext := strings.ToLower(filepath.Ext(imgPath))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return ""
		}
		san := SanitizeImageFilename(filepath.Base(imgPath))
		dst := filepath.Join(workDir, san)
		if st, err := os.Stat(dst); err == nil && st.Size() > 100 {
			return san
		}
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(imgPath)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return ""
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil || len(data) < 100 {
			return ""
		}
		ct := http.DetectContentType(data)
		if !strings.HasPrefix(ct, "image/png") && !strings.HasPrefix(ct, "image/jpeg") {
			return ""
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return ""
		}
		return san
	}

	base := filepath.Base(imgPath)
	ext := strings.ToLower(filepath.Ext(base))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".pdf" {
		return ""
	}
	san := SanitizeImageFilename(base)
	for _, cand := range []string{san, base} {
		if st, err := os.Stat(filepath.Join(workDir, cand)); err == nil && !st.IsDir() && st.Size() > 0 {
			return cand
		}
	}
	return ""
}
