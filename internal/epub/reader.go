package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Oyetomi/folio/internal/config"
	"github.com/Oyetomi/folio/internal/highlight"
	"github.com/Oyetomi/folio/internal/markdown"
)

type containerXML struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opfPackage struct {
	Metadata struct {
		Title       []string `xml:"title"`
		Creator     []string `xml:"creator"`
		Description []string `xml:"description"`
		Rights      []string `xml:"rights"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Itemrefs []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

// ExtractEPUB reads an .epub archive, stages images into workDir, and converts
// all spine XHTML chapters into ChapterDoc structs ready for LaTeX compilation.
func ExtractEPUB(epubPath string, workDir string) (*config.BookConfig, []markdown.ChapterDoc, error) {
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()

	fileMap := make(map[string]*zip.File)
	for _, f := range zr.File {
		fileMap[f.Name] = f
	}

	contBytes, err := readZipEntry(fileMap["META-INF/container.xml"])
	if err != nil {
		return nil, nil, fmt.Errorf("missing META-INF/container.xml: %w", err)
	}
	var cont containerXML
	if err := xml.Unmarshal(contBytes, &cont); err != nil || len(cont.Rootfiles) == 0 {
		return nil, nil, fmt.Errorf("invalid META-INF/container.xml")
	}

	opfPath := cont.Rootfiles[0].FullPath
	opfDir := path.Dir(opfPath)
	if opfDir == "." {
		opfDir = ""
	}
	opfBytes, err := readZipEntry(fileMap[opfPath])
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read %s: %w", opfPath, err)
	}

	var pkg opfPackage
	if err := xml.Unmarshal(opfBytes, &pkg); err != nil {
		return nil, nil, fmt.Errorf("invalid OPF XML: %w", err)
	}

	cfg := &config.BookConfig{
		Title:       "Book",
		Author:      "Author",
		LicenseText: "All rights reserved.",
		WorkDir:     workDir,
	}
	if len(pkg.Metadata.Title) > 0 && strings.TrimSpace(pkg.Metadata.Title[0]) != "" {
		cfg.Title = strings.TrimSpace(pkg.Metadata.Title[0])
	}
	if len(pkg.Metadata.Creator) > 0 && strings.TrimSpace(pkg.Metadata.Creator[0]) != "" {
		cfg.Author = strings.Join(pkg.Metadata.Creator, ", ")
	}
	if len(pkg.Metadata.Description) > 0 {
		cfg.Description = stripTags(pkg.Metadata.Description[0])
	}
	if len(pkg.Metadata.Rights) > 0 && strings.TrimSpace(pkg.Metadata.Rights[0]) != "" {
		cfg.LicenseText = stripTags(pkg.Metadata.Rights[0])
	}

	manifestHref := make(map[string]string)
	manifestProps := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		fullHref := item.Href
		if opfDir != "" {
			fullHref = path.Join(opfDir, item.Href)
		}
		manifestHref[item.ID] = fullHref
		manifestProps[item.ID] = item.Properties

		if strings.HasPrefix(item.MediaType, "image/") {
			if zf, ok := fileMap[fullHref]; ok {
				if imgData, err := readZipEntry(zf); err == nil {
					baseName := filepath.Base(item.Href)
					sanName := markdown.SanitizeImageFilename(baseName)
					_ = os.WriteFile(filepath.Join(workDir, baseName), imgData, 0o644)
					if sanName != baseName {
						_ = os.WriteFile(filepath.Join(workDir, sanName), imgData, 0o644)
					}
					if cfg.CoverImage == "" && (strings.Contains(strings.ToLower(item.ID), "cover") || strings.Contains(strings.ToLower(baseName), "cover") || strings.Contains(item.Properties, "cover-image")) {
						if !strings.HasSuffix(strings.ToLower(baseName), ".svg") {
							cfg.CoverImage = filepath.Join(workDir, baseName)
						}
					}
				}
			}
		}
	}

	var docs []markdown.ChapterDoc
	chIdx := 0
	for _, ref := range pkg.Spine.Itemrefs {
		href, ok := manifestHref[ref.IDRef]
		if !ok {
			continue
		}
		props := manifestProps[ref.IDRef]
		baseLower := strings.ToLower(path.Base(href))
		if strings.Contains(props, "nav") || isBoilerplateEPUBFile(baseLower) {
			continue
		}
		zf, ok := fileMap[href]
		if !ok {
			continue
		}
		rawXHTML, err := readZipEntry(zf)
		if err != nil {
			continue
		}
		xhtmlStr := string(rawXHTML)
		if isBoilerplateXHTML(xhtmlStr, baseLower) {
			continue
		}
		title, mdText := xhtmlToMarkdown(xhtmlStr)
		if strings.TrimSpace(mdText) == "" {
			continue
		}
		chIdx++
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chIdx)
		}
		partNum := classifyEPUBSection(baseLower, xhtmlStr)
		docs = append(docs, markdown.ChapterDoc{
			IDTuple:  [3]int{partNum, chIdx, 0},
			HasID:    true,
			PartIdx:  partNum,
			Title:    title,
			FilePath: href,
			RawText:  mdText,
		})
	}

	if len(docs) == 1 && strings.Count("\n"+docs[0].RawText, "\n## ") >= 2 {
		docs = splitSingleDocChapters(docs[0])
	}

	return cfg, docs, nil
}

func splitSingleDocChapters(doc markdown.ChapterDoc) []markdown.ChapterDoc {
	lines := strings.Split(doc.RawText, "\n")
	var out []markdown.ChapterDoc
	var cur []string
	curTitle := ""
	chIdx := 0

	flush := func(isPreamble bool) {
		body := strings.TrimSpace(strings.Join(cur, "\n"))
		if body == "" {
			cur = nil
			return
		}
		pNum := 1
		title := curTitle
		if isPreamble {
			pNum = 0
			if title == "" {
				title = "Front Matter"
			}
		} else {
			low := strings.ToLower(title)
			if strings.HasPrefix(low, "note") || strings.HasPrefix(low, "endnote") || strings.HasPrefix(low, "appendix") || strings.HasPrefix(low, "colophon") || strings.HasPrefix(low, "bibliography") {
				pNum = 999
			}
		}
		chIdx++
		out = append(out, markdown.ChapterDoc{
			IDTuple:  [3]int{pNum, chIdx, 0},
			HasID:    true,
			PartIdx:  pNum,
			Title:    title,
			FilePath: doc.FilePath,
			RawText:  body,
		})
		cur = nil
	}

	seenH2 := false
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			flush(!seenH2)
			seenH2 = true
			curTitle = strings.TrimSpace(strings.TrimPrefix(l, "## "))
			continue
		}
		cur = append(cur, l)
	}
	flush(!seenH2)
	if len(out) == 0 {
		return []markdown.ChapterDoc{doc}
	}
	return out
}

func isBoilerplateEPUBFile(baseLower string) bool {
	stem := strings.TrimSuffix(baseLower, path.Ext(baseLower))
	switch stem {
	case "toc", "nav", "cover", "titlepage", "halftitlepage", "imprint", "copyright", "uncopyright":
		return true
	}
	return strings.HasPrefix(stem, "cover-") || strings.HasPrefix(stem, "toc-") || strings.HasPrefix(stem, "nav-")
}

func isBoilerplateXHTML(xhtml string, baseLower string) bool {
	l := strings.ToLower(xhtml)
	if len(l) < 8000 && !strings.Contains(l, "bodymatter") && !strings.Contains(l, `epub:type="chapter"`) {
		if strings.Contains(l, `epub:type="halftitlepage"`) || strings.Contains(l, `epub:type="titlepage"`) || strings.Contains(l, `epub:type="cover"`) || strings.Contains(l, `epub:type="copyright-page"`) {
			return true
		}
	}
	if strings.Contains(baseLower, "colophon") && strings.Contains(l, `epub:type="colophon"`) {
		return false
	}
	return false
}

func classifyEPUBSection(baseLower string, xhtml string) int {
	l := strings.ToLower(xhtml)
	if strings.Contains(l, `epub:type="backmatter"`) || strings.Contains(baseLower, "endnotes") || strings.Contains(baseLower, "colophon") || strings.Contains(baseLower, "bibliography") || strings.Contains(baseLower, "appendix") {
		return 999
	}
	if strings.Contains(l, `epub:type="frontmatter"`) || strings.Contains(baseLower, "preface") || strings.Contains(baseLower, "foreword") || strings.Contains(baseLower, "dedication") || strings.Contains(baseLower, "epigraph") || strings.Contains(baseLower, "introduction") || strings.Contains(baseLower, "etymology") || strings.Contains(baseLower, "extracts") || strings.Contains(baseLower, "dramatis") || strings.Contains(baseLower, "acknowledg") {
		return 0
	}
	return 1
}

func readZipEntry(zf *zip.File) ([]byte, error) {
	if zf == nil {
		return nil, fmt.Errorf("nil zip entry")
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

var (
	reXHTMLHeadTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reXHTMLBody      = regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
	reXHTMLChDiv     = regexp.MustCompile(`(?is)<div[^>]+class=["'][^"']*\bchapter\b[^"']*["'][^>]*>.*?</div>`)
	reXHTMLHgroup    = regexp.MustCompile(`(?is)<hgroup[^>]*>(.*?)</hgroup>`)
	reXHTMLMath      = regexp.MustCompile(`(?is)<math\b([^>]*)>(.*?)</math>`)
	reXHTMLTable     = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	reXHTMLCaption   = regexp.MustCompile(`(?is)<caption[^>]*>(.*?)</caption>`)
	reXHTMLThead     = regexp.MustCompile(`(?is)<thead[^>]*>(.*?)</thead>`)
	reXHTMLTR        = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reXHTMLCell      = regexp.MustCompile(`(?is)<(th|td)([^>]*)(?:/>|>(.*?)</(?:th|td)>)`)
	reColspanAttr    = regexp.MustCompile(`(?i)\bcolspan=["']?(\d+)["']?`)
	reXHTMLH1        = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	reXHTMLH2        = regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>`)
	reXHTMLH3        = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`)
	reXHTMLH456      = regexp.MustCompile(`(?is)<h[456][^>]*>(.*?)</h[456]>`)
	reXHTMLPre       = regexp.MustCompile(`(?is)<pre([^>]*)>(.*?)</pre>`)
	reXHTMLImg       = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["'][^>]*>`)
	reXHTMLAlt       = regexp.MustCompile(`(?is)alt=["']([^"']*)["']`)
	reLangAttr       = regexp.MustCompile(`(?is)(?:language-|lang-|brush:\s*)([a-zA-Z0-9_+-]+)`)
	reXHTMLOL        = regexp.MustCompile(`(?is)<ol[^>]*>(.*?)</ol>`)
	reXHTMLLI        = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	reXHTMLDL        = regexp.MustCompile(`(?is)<dl[^>]*>(.*?)</dl>`)
	reXHTMLDT        = regexp.MustCompile(`(?is)<dt[^>]*>(.*?)</dt>`)
	reXHTMLDD        = regexp.MustCompile(`(?is)<dd[^>]*>(.*?)</dd>`)
	reXHTMLBQ        = regexp.MustCompile(`(?is)<blockquote[^>]*>(.*?)</blockquote>`)
	reXHTMLP         = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	reXHTMLBR        = regexp.MustCompile(`(?is)<br\s*/?>`)
	reXHTMLDivOpen   = regexp.MustCompile(`(?is)<div\b[^>]*>`)
	reXHTMLDivClose  = regexp.MustCompile(`(?is)</div>`)
	reXHTMLLnum      = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*\blnum\b[^"']*["'][^>]*>.*?</span>`)
	reNoteref        = regexp.MustCompile(`(?is)<a[^>]+(?:noteref|footnote)[^>]*>(.*?)</a>`)
	reSup            = regexp.MustCompile(`(?is)<sup[^>]*>(.*?)</sup>`)
	reCodeSpan       = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	reStrong         = regexp.MustCompile(`(?is)<(?:strong|b)(?:\s+[^>]*)?>(.*?)</(?:strong|b)>`)
	reEm             = regexp.MustCompile(`(?is)<(?:em|i)(?:\s+[^>]*)?>(.*?)</(?:em|i)>`)
	reBacklink       = regexp.MustCompile(`(?is)<a[^>]+backlink[^>]*>.*?</a>`)
	reAllTags        = regexp.MustCompile(`(?is)<[^>]+>`)
)

func xhtmlToMarkdown(xhtml string) (string, string) {
	headTitle := ""
	if mt := reXHTMLHeadTitle.FindStringSubmatch(xhtml); len(mt) > 1 {
		headTitle = strings.TrimSpace(stripTags(mt[1]))
		if idx := strings.Index(headTitle, " — "); idx > 0 {
			headTitle = strings.TrimSpace(headTitle[:idx])
		}
	}

	body := xhtml
	if m := reXHTMLBody.FindStringSubmatch(xhtml); len(m) > 1 {
		body = m[1]
	}

	// Clean zero-width BOM / word joiner / ASCII control characters
	body = strings.Map(func(r rune) rune {
		if r == 0xFEFF || r == 0x2060 || (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || (r >= 0x7F && r <= 0x9F) {
			return -1
		}
		return r
	}, body)
	body = reXHTMLChDiv.ReplaceAllString(body, "")
	body = reXHTMLLnum.ReplaceAllString(body, "")
	body = reBacklink.ReplaceAllString(body, "")

	title := ""
	if loc := reXHTMLHgroup.FindStringSubmatchIndex(body); len(loc) >= 4 {
		hg := body[loc[2]:loc[3]]
		hgTitle := strings.Join(strings.Fields(stripTags(hg)), ": ")
		// Extract explicit epub:type="title" paragraph if present inside hgroup
		if mTitleP := regexp.MustCompile(`(?is)<p[^>]*epub:type=["'][^"']*title[^"']*["'][^>]*>(.*?)</p>`).FindStringSubmatch(hg); len(mTitleP) > 1 {
			title = strings.TrimSpace(stripTags(mTitleP[1]))
		} else {
			title = hgTitle
		}
		body = body[:loc[0]] + body[loc[1]:]
	} else if loc := reXHTMLH1.FindStringSubmatchIndex(body); len(loc) >= 4 {
		title = strings.TrimSpace(stripTags(body[loc[2]:loc[3]]))
		body = body[:loc[0]] + body[loc[1]:]
	} else if loc := reXHTMLH2.FindStringSubmatchIndex(body); len(loc) >= 4 {
		title = strings.TrimSpace(stripTags(body[loc[2]:loc[3]]))
		body = body[:loc[0]] + body[loc[1]:]
	}
	if headTitle != "" && (title == "" || len(title) <= 4) {
		title = headTitle
	}
	title = strings.TrimPrefix(title, "Chapter ")

	// Convert MathML equations into native LaTeX tokens before any tag stripping or inline regexes
	var mathTokens []string
	body = reXHTMLMath.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLMath.FindStringSubmatch(m)
		tex := strings.TrimSpace(convertMathMLToLatex(sub[2]))
		if tex == "" {
			return ""
		}
		rendered := "$" + tex + "$"
		if strings.Contains(strings.ToLower(sub[1]), `display="block"`) || strings.Contains(tex, `\begin{`) {
			rendered = "\n\n$$\n" + tex + "\n$$\n\n"
		}
		idx := len(mathTokens)
		mathTokens = append(mathTokens, rendered)
		return fmt.Sprintf("\x00MATH%d\x00", idx)
	})

	// Convert <pre> blocks first, preserving language class if present
	body = reXHTMLPre.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLPre.FindStringSubmatch(m)
		lang := ""
		if lm := reLangAttr.FindStringSubmatch(sub[1] + " " + sub[2]); len(lm) > 1 {
			lang = strings.ToLower(lm[1])
		}
		code := html.UnescapeString(reAllTags.ReplaceAllString(sub[2], ""))
		return "\n\n```" + lang + "\n" + strings.TrimSpace(code) + "\n```\n\n"
	})

	// Convert <table> blocks into Markdown tables or clean dramatic dialogue / script lines
	body = reXHTMLTable.ReplaceAllStringFunc(body, parseHTMLTable)

	// Convert <ol> ordered lists first so items keep numbered ordering (important for endnotes!)
	body = reXHTMLOL.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLOL.FindStringSubmatch(m)
		var items []string
		idx := 0
		for _, li := range reXHTMLLI.FindAllStringSubmatch(sub[1], -1) {
			txt := cleanInlineHTML(li[1])
			if txt == "" {
				continue
			}
			idx++
			items = append(items, fmt.Sprintf("%d. %s", idx, txt))
		}
		return "\n\n" + strings.Join(items, "\n") + "\n\n"
	})

	// Convert <dl> definition lists (dramatis personae, glossaries)
	body = reXHTMLDL.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLDL.FindStringSubmatch(m)
		inner := reXHTMLDT.ReplaceAllStringFunc(sub[1], func(dtm string) string {
			d := reXHTMLDT.FindStringSubmatch(dtm)
			return "\n- **" + cleanInlineHTML(d[1]) + "** — "
		})
		inner = reXHTMLDD.ReplaceAllStringFunc(inner, func(ddm string) string {
			d := reXHTMLDD.FindStringSubmatch(ddm)
			return cleanInlineHTML(d[1]) + "\n"
		})
		return "\n\n" + inner + "\n\n"
	})

	// Convert <img> elements to Markdown image syntax
	body = reXHTMLImg.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLImg.FindStringSubmatch(m)
		src := filepath.Base(sub[1])
		alt := ""
		if am := reXHTMLAlt.FindStringSubmatch(m); len(am) > 1 {
			alt = strings.TrimSpace(am[1])
		}
		return fmt.Sprintf("\n\n![%s](%s)\n\n", alt, src)
	})

	// Convert <blockquote> elements (preserving verse linebreaks)
	body = reXHTMLBQ.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLBQ.FindStringSubmatch(m)
		txt := cleanBlockHTML(sub[1])
		if txt == "" {
			return ""
		}
		var lines []string
		for _, ln := range strings.Split(txt, "\n") {
			ln = strings.TrimSpace(ln)
			if ln != "" {
				lines = append(lines, "> "+ln)
			}
		}
		return "\n\n" + strings.Join(lines, "\n") + "\n\n"
	})

	// Convert headings
	body = reXHTMLH1.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLH1.FindStringSubmatch(m)
		return "\n\n# " + cleanInlineHTML(sub[1]) + "\n\n"
	})
	body = reXHTMLH2.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLH2.FindStringSubmatch(m)
		return "\n\n## " + cleanInlineHTML(sub[1]) + "\n\n"
	})
	body = reXHTMLH3.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLH3.FindStringSubmatch(m)
		return "\n\n### " + cleanInlineHTML(sub[1]) + "\n\n"
	})
	body = reXHTMLH456.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLH456.FindStringSubmatch(m)
		return "\n\n#### " + cleanInlineHTML(sub[1]) + "\n\n"
	})

	// Convert remaining unordered list items and paragraphs
	body = reXHTMLLI.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLLI.FindStringSubmatch(m)
		return "\n- " + cleanInlineHTML(sub[1])
	})
	body = reXHTMLP.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLP.FindStringSubmatch(m)
		txt := cleanBlockHTML(sub[1])
		return "\n\n" + txt + "\n\n"
	})
	body = formatVerseLinegroups(body)
	body = reXHTMLDivClose.ReplaceAllString(body, "\n")

	lines := strings.Split(body, "\n")
	var out []string
	inFence := false
	for _, l := range lines {
		tr := strings.TrimSpace(l)
		if strings.HasPrefix(tr, "```") {
			inFence = !inFence
			out = append(out, tr)
			continue
		}
		if inFence {
			out = append(out, l)
			continue
		}
		cleaned := strings.TrimSpace(reAllTags.ReplaceAllString(l, ""))
		cleaned = html.UnescapeString(cleaned)
		if strings.HasPrefix(cleaned, "Previous topic:") || strings.HasPrefix(cleaned, "Next topic:") {
			continue
		}
		if cleaned != "" {
			out = append(out, cleaned)
		} else {
			out = append(out, "")
		}
	}

	res := strings.TrimSpace(strings.Join(out, "\n"))
	if len(mathTokens) > 0 {
		reRestoreMath := regexp.MustCompile(`\x00MATH(\d+)\x00`)
		res = reRestoreMath.ReplaceAllStringFunc(res, func(m string) string {
			sub := reRestoreMath.FindStringSubmatch(m)
			idx, _ := strconv.Atoi(sub[1])
			if idx >= 0 && idx < len(mathTokens) {
				return mathTokens[idx]
			}
			return ""
		})
	}
	title = strings.Join(strings.Fields(title), " ")
	return title, strings.TrimSpace(res)
}

func parseHTMLTable(tblMatch string) string {
	sub := reXHTMLTable.FindStringSubmatch(tblMatch)
	inner := sub[1]

	caption := ""
	if cm := reXHTMLCaption.FindStringSubmatch(inner); len(cm) > 1 {
		caption = cleanInlineHTML(cm[1])
		inner = reXHTMLCaption.ReplaceAllString(inner, "")
	}

	hasThead := reXHTMLThead.MatchString(inner) || strings.Contains(strings.ToLower(inner), "<th")
	isDrama := strings.Contains(inner, "z3998:stage-direction") || strings.Contains(inner, "z3998:persona")

	trMatches := reXHTMLTR.FindAllStringSubmatch(inner, -1)
	if len(trMatches) == 0 {
		return ""
	}

	if isDrama {
		var sb strings.Builder
		sb.WriteString("\n\n")
		for _, tr := range trMatches {
			cells := reXHTMLCell.FindAllStringSubmatch(tr[1], -1)
			if len(cells) >= 2 {
				col1 := cleanInlineHTML(cells[0][3])
				col2 := cleanBlockHTML(cells[1][3])
				if col1 == "" && col2 != "" {
					// Stage direction row
					sb.WriteString("> " + strings.ReplaceAll(col2, "\n", "  \n> ") + "\n\n")
				} else if col1 != "" {
					// Speaker + speech lines
					lines := strings.Split(strings.TrimSpace(col2), "\n")
					if len(lines) <= 1 {
						sb.WriteString(fmt.Sprintf("**%s:** %s\n\n", col1, col2))
					} else {
						sb.WriteString(fmt.Sprintf("**%s:**\n> %s\n\n", col1, strings.Join(lines, "  \n> ")))
					}
				}
			} else if len(cells) == 1 {
				c := cleanBlockHTML(cells[0][3])
				if c != "" {
					sb.WriteString("> " + strings.ReplaceAll(c, "\n", "\n> ") + "\n\n")
				}
			}
		}
		return sb.String()
	}

	type htmlRow struct {
		cells []string
	}
	var rows []htmlRow
	maxCols := 0
	for _, tr := range trMatches {
		cellMatches := reXHTMLCell.FindAllStringSubmatch(tr[1], -1)
		if len(cellMatches) == 0 {
			continue
		}
		var rowCells []string
		for _, cm := range cellMatches {
			attrs := cm[2]
			content := cm[3]
			span := 1
			if sm := reColspanAttr.FindStringSubmatch(attrs); len(sm) > 1 {
				if v, err := strconv.Atoi(sm[1]); err == nil && v > 1 {
					span = v
				}
			}
			val := strings.ReplaceAll(cleanInlineHTML(content), "|", `\|`)
			rowCells = append(rowCells, val)
			for s := 1; s < span; s++ {
				rowCells = append(rowCells, "")
			}
		}
		if len(rowCells) > maxCols {
			maxCols = len(rowCells)
		}
		rows = append(rows, htmlRow{cells: rowCells})
	}
	if len(rows) == 0 || maxCols == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n")
	if caption != "" {
		sb.WriteString("*" + caption + "*\n\n")
	}
	if !hasThead {
		sb.WriteString("<!-- nohead -->\n")
	}
	for rIdx, row := range rows {
		for len(row.cells) < maxCols {
			row.cells = append(row.cells, "")
		}
		sb.WriteString("| " + strings.Join(row.cells, " | ") + " |\n")
		if rIdx == 0 {
			seps := make([]string, maxCols)
			for i := range seps {
				seps[i] = "---"
			}
			sb.WriteString("| " + strings.Join(seps, " | ") + " |\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// cleanBlockHTML preserves explicit <br/> and leaf <div> line breaks inside verse, poetry, or drama without breaking wrapped HTML prose.
func cleanBlockHTML(s string) string {
	const brTok = "\x00BR\x00"
	hasExplicitBreaks := reXHTMLBR.MatchString(s) || strings.Contains(strings.ToLower(s), "<div")
	if !hasExplicitBreaks {
		return cleanInlineHTML(s)
	}
	s = reXHTMLBR.ReplaceAllString(s, brTok)
	s = formatSimpleVerseDivs(s, brTok)
	segs := strings.Split(s, brTok)
	var out []string
	for _, seg := range segs {
		cl := cleanInlineHTML(seg)
		if cl != "" {
			out = append(out, cl)
		}
	}
	return strings.Join(out, "  \n")
}

// formatSimpleVerseDivs collapses each innermost <div>...</div> into a single cleaned line.
func formatSimpleVerseDivs(s string, lineSep string) string {
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "<div") {
		return s
	}
	for {
		lower = strings.ToLower(s)
		changed := false
		searchFrom := 0
		for {
			idx := strings.Index(lower[searchFrom:], "<div")
			if idx < 0 {
				break
			}
			openStart := searchFrom + idx
			gt := strings.IndexByte(lower[openStart:], '>')
			if gt < 0 {
				break
			}
			contentStart := openStart + gt + 1
			closeRel := strings.Index(lower[contentStart:], "</div>")
			if closeRel < 0 {
				break
			}
			closeStart := contentStart + closeRel
			// Only convert innermost <div> (contains no nested <div)
			if strings.Contains(lower[contentStart:closeStart], "<div") {
				searchFrom = contentStart
				continue
			}
			inner := s[contentStart:closeStart]
			var rep string
			if strings.Contains(strings.ToLower(inner), "<blockquote") || strings.Contains(inner, "\n\n") {
				rep = "\n" + strings.TrimSpace(inner) + "\n"
			} else {
				cl := cleanInlineHTML(inner)
				if cl != "" {
					if lineSep == "\x00BR\x00" {
						rep = "\x00BR\x00" + cl + "\x00BR\x00"
					} else {
						rep = "\n" + cl + lineSep
					}
				} else {
					rep = "\n"
				}
			}
			s = s[:openStart] + rep + s[closeStart+len("</div>"):]
			changed = true
			break
		}
		if !changed {
			break
		}
	}
	return s
}

var reLinegroupStart = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*(?:linegroup|stanza|verse|poem)[^"']*["'][^>]*>`)

// formatVerseLinegroups formats <div class="linegroup|stanza"> blocks into stanza-separated Markdown verse lines.
func formatVerseLinegroups(body string) string {
	for {
		loc := reLinegroupStart.FindStringIndex(body)
		if loc == nil {
			break
		}
		lower := strings.ToLower(body)
		depth := 1
		pos := loc[1]
		endStart, endStop := -1, -1
		for pos < len(body) && depth > 0 {
			nextOpen := strings.Index(lower[pos:], "<div")
			nextClose := strings.Index(lower[pos:], "</div>")
			if nextClose < 0 {
				break
			}
			if nextOpen >= 0 && nextOpen < nextClose {
				depth++
				pos += nextOpen + 4
			} else {
				depth--
				if depth == 0 {
					endStart = pos + nextClose
					endStop = endStart + len("</div>")
					break
				}
				pos += nextClose + len("</div>")
			}
		}
		if endStart < 0 {
			body = body[:loc[0]] + "\n\n" + body[loc[1]:]
			continue
		}
		inner := formatSimpleVerseDivs(body[loc[1]:endStart], "  \n")
		body = body[:loc[0]] + "\n\n" + strings.TrimSpace(inner) + "\n\n" + body[endStop:]
	}
	return body
}

var (
	reOpenParenNL  = regexp.MustCompile(`\(\s*[\r\n]+\s+`)
	reClosePunctNL = regexp.MustCompile(`[\r\n]+\s+([),.;:])`)
	reSpanBold     = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*(?:paragraphHead|cmbx|bfseries|bold)[^"']*["'][^>]*>(.*?)</span>`)
)

func cleanInlineHTML(s string) string {
	s = reOpenParenNL.ReplaceAllString(s, "(")
	s = reClosePunctNL.ReplaceAllString(s, "$1")
	s = reBacklink.ReplaceAllString(s, "")
	s = reSpanBold.ReplaceAllStringFunc(s, func(m string) string {
		sub := reSpanBold.FindStringSubmatch(m)
		inner := strings.TrimSpace(reAllTags.ReplaceAllString(sub[1], ""))
		if inner == "" {
			return ""
		}
		return "**" + inner + "** "
	})
	s = reCodeSpan.ReplaceAllStringFunc(s, func(m string) string {
		sub := reCodeSpan.FindStringSubmatch(m)
		return "`" + html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")) + "`"
	})
	s = reNoteref.ReplaceAllStringFunc(s, func(m string) string {
		sub := reNoteref.FindStringSubmatch(m)
		num := strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")))
		num = strings.TrimSpace(strings.Trim(num, "[]"))
		if num == "" {
			return ""
		}
		return "^[" + num + "]"
	})
	s = reSup.ReplaceAllStringFunc(s, func(m string) string {
		sub := reSup.FindStringSubmatch(m)
		val := strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")))
		val = strings.TrimSpace(strings.Trim(val, "[]"))
		if val == "" {
			return ""
		}
		return "^[" + val + "]"
	})
	// Strip inner bold tags nested inside italic tags before applying **...** / *...*
	for i := 0; i < 3; i++ {
		next := reStrong.ReplaceAllStringFunc(s, func(m string) string {
			sub := reStrong.FindStringSubmatch(m)
			inner := strings.TrimSpace(reAllTags.ReplaceAllString(sub[1], ""))
			if inner == "" {
				return ""
			}
			return "**" + inner + "**"
		})
		next = reEm.ReplaceAllStringFunc(next, func(m string) string {
			sub := reEm.FindStringSubmatch(m)
			inner := strings.ReplaceAll(strings.TrimSpace(reAllTags.ReplaceAllString(sub[1], "")), "**", "")
			if inner == "" {
				return ""
			}
			return "*" + inner + "*"
		})
		if next == s {
			break
		}
		s = next
	}
	s = reAllTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func stripTags(s string) string {
	return strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(s, "")))
}

// convertMathMLToLatex recursively translates W3C MathML XML AST nodes into clean LaTeX expressions.
type mathNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []mathNode `xml:",any"`
}

func convertMathMLToLatex(innerXML string) string {
	dec := xml.NewDecoder(bytes.NewBufferString("<mrow>" + innerXML + "</mrow>"))
	dec.Strict = false
	var root mathNode
	if err := dec.Decode(&root); err != nil {
		return stripTags(innerXML)
	}
	return renderMathNode(root)
}

func renderMathNode(n mathNode) string {
	tag := strings.ToLower(n.XMLName.Local)
	childrenTex := func() string {
		var sb strings.Builder
		for _, c := range n.Children {
			sb.WriteString(renderMathNode(c))
		}
		return sb.String()
	}

	switch tag {
	case "mi", "mn", "mo":
		t := strings.TrimSpace(html.UnescapeString(n.Text))
		return mapMathSymbol(t)
	case "mtext":
		t := strings.TrimSpace(html.UnescapeString(n.Text))
		if t == "" {
			return ""
		}
		return fmt.Sprintf(`\text{%s}`, highlight.LatexEscape(t))
	case "mspace":
		return `\ `
	case "mrow", "mstyle", "mpadded", "mphantom", "math":
		if len(n.Children) > 0 {
			return childrenTex()
		}
		return mapMathSymbol(strings.TrimSpace(html.UnescapeString(n.Text)))
	case "msup":
		if len(n.Children) >= 2 {
			return fmt.Sprintf("{%s}^{%s}", renderMathNode(n.Children[0]), renderMathNode(n.Children[1]))
		}
	case "msub":
		if len(n.Children) >= 2 {
			return fmt.Sprintf("{%s}_{%s}", renderMathNode(n.Children[0]), renderMathNode(n.Children[1]))
		}
	case "msubsup":
		if len(n.Children) >= 3 {
			return fmt.Sprintf("{%s}_{%s}^{%s}", renderMathNode(n.Children[0]), renderMathNode(n.Children[1]), renderMathNode(n.Children[2]))
		}
	case "mfrac":
		if len(n.Children) >= 2 {
			return fmt.Sprintf(`\frac{%s}{%s}`, renderMathNode(n.Children[0]), renderMathNode(n.Children[1]))
		}
	case "msqrt":
		return fmt.Sprintf(`\sqrt{%s}`, childrenTex())
	case "mroot":
		if len(n.Children) >= 2 {
			return fmt.Sprintf(`\sqrt[%s]{%s}`, renderMathNode(n.Children[1]), renderMathNode(n.Children[0]))
		}
	case "mover":
		if len(n.Children) >= 2 {
			top := renderMathNode(n.Children[1])
			base := renderMathNode(n.Children[0])
			if top == "" || top == `\bar{}` || top == "-" || top == "¯" || top == "‾" {
				return fmt.Sprintf(`\overline{%s}`, base)
			}
			if top == "^" || top == "ˆ" || top == "\u0302" {
				return fmt.Sprintf(`\hat{%s}`, base)
			}
			if top == `\rightarrow ` || top == `\rightarrow` || top == "→" {
				return fmt.Sprintf(`\vec{%s}`, base)
			}
			return fmt.Sprintf(`\overset{%s}{%s}`, top, base)
		}
	case "munder":
		if len(n.Children) >= 2 {
			return fmt.Sprintf(`\underset{%s}{%s}`, renderMathNode(n.Children[1]), renderMathNode(n.Children[0]))
		}
	case "mfenced":
		open, cl, sep := "(", ")", ", "
		for _, a := range n.Attrs {
			if a.Name.Local == "open" {
				open = strings.TrimSpace(a.Value)
			} else if a.Name.Local == "close" {
				cl = strings.TrimSpace(a.Value)
			} else if a.Name.Local == "separators" {
				sep = " " + strings.TrimSpace(a.Value) + " "
			}
		}
		open = normalizeDelim(open)
		cl = normalizeDelim(cl)
		var parts []string
		for _, c := range n.Children {
			parts = append(parts, renderMathNode(c))
		}
		return fmt.Sprintf(`\left%s %s \right%s`, open, strings.Join(parts, sep), cl)
	case "mtable":
		cls := ""
		for _, a := range n.Attrs {
			if a.Name.Local == "class" {
				cls = strings.ToLower(a.Value)
			}
		}
		var rows []string
		for _, r := range n.Children {
			rows = append(rows, renderMathNode(r))
		}
		if strings.Contains(cls, "align") {
			return `\begin{aligned} ` + strings.Join(rows, ` \\ `) + ` \end{aligned}`
		}
		return `\begin{matrix} ` + strings.Join(rows, ` \\ `) + ` \end{matrix}`
	case "mtr":
		var cols []string
		for _, c := range n.Children {
			cols = append(cols, renderMathNode(c))
		}
		return strings.Join(cols, ` & `)
	case "mtd":
		return childrenTex()
	}
	if len(n.Children) > 0 {
		return childrenTex()
	}
	return mapMathSymbol(strings.TrimSpace(html.UnescapeString(n.Text)))
}

func normalizeDelim(d string) string {
	switch d {
	case "":
		return "."
	case "{":
		return `\{`
	case "}":
		return `\}`
	case "〈", "⟨":
		return `\langle`
	case "〉", "⟩":
		return `\rangle`
	case "‖":
		return `\|`
	}
	return d
}

func mapMathSymbol(s string) string {
	switch s {
	case "ℂ":
		return `\mathbb{C}`
	case "ℝ":
		return `\mathbb{R}`
	case "ℤ":
		return `\mathbb{Z}`
	case "ℕ":
		return `\mathbb{N}`
	case "ℚ":
		return `\mathbb{Q}`
	case "ℒ":
		return `\mathcal{L}`
	case "ℛ":
		return `\mathcal{R}`
	case "ℳ":
		return `\mathcal{M}`
	case "ℰ":
		return `\mathcal{E}`
	case "ℱ":
		return `\mathcal{F}`
	case "ℋ":
		return `\mathcal{H}`
	case "ℬ":
		return `\mathcal{B}`
	case "′":
		return `\prime `
	case "″":
		return `\prime\prime `
	case "ℓ":
		return `\ell `
	case "⋯":
		return `\cdots `
	case "⋮":
		return `\vdots `
	case "⋱":
		return `\ddots `
	case "△":
		return `\triangle `
	case "□":
		return `\square `
	case "■":
		return `\blacksquare `
	case "α":
		return `\alpha `
	case "β":
		return `\beta `
	case "γ":
		return `\gamma `
	case "δ":
		return `\delta `
	case "ε", "ϵ":
		return `\epsilon `
	case "θ":
		return `\theta `
	case "ι":
		return `\iota `
	case "λ":
		return `\lambda `
	case "μ":
		return `\mu `
	case "ρ":
		return `\rho `
	case "σ":
		return `\sigma `
	case "τ":
		return `\tau `
	case "ϕ", "φ":
		return `\phi `
	case "ω":
		return `\omega `
	case "Δ":
		return `\Delta `
	case "Σ":
		return `\Sigma `
	case "Ω":
		return `\Omega `
	case "〈", "⟨":
		return `\langle `
	case "〉", "⟩":
		return `\rangle `
	case "‖":
		return `\|`
	case "∈":
		return `\in `
	case "∉":
		return `\notin `
	case "⊂":
		return `\subset `
	case "⊆":
		return `\subseteq `
	case "⊈":
		return `\nsubseteq `
	case "⊊":
		return `\subsetneq `
	case "⊃":
		return `\supset `
	case "⊇":
		return `\supseteq `
	case "⊉":
		return `\nsupseteq `
	case "⊋":
		return `\supsetneq `
	case "∪":
		return `\cup `
	case "∩":
		return `\cap `
	case "∅":
		return `\emptyset `
	case "⊕":
		return `\oplus `
	case "⊠":
		return `\boxtimes `
	case "→":
		return `\rightarrow `
	case "←":
		return `\leftarrow `
	case "↔":
		return `\leftrightarrow `
	case "⇒":
		return `\Rightarrow `
	case "⇐":
		return `\Leftarrow `
	case "⇔":
		return `\Leftrightarrow `
	case "↦":
		return `\mapsto `
	case "∀":
		return `\forall `
	case "∃":
		return `\exists `
	case "¬":
		return `\neg `
	case "∧":
		return `\wedge `
	case "∨":
		return `\vee `
	case "⊤":
		return `\top `
	case "⊥":
		return `\perp `
	case "≤":
		return `\le `
	case "≥":
		return `\ge `
	case "≠":
		return `\neq `
	case "≈":
		return `\approx `
	case "∼":
		return `\sim `
	case "≅":
		return `\cong `
	case "±":
		return `\pm `
	case "×":
		return `\times `
	case "·", "⋅":
		return `\cdot `
	case "∘":
		return `\circ `
	case "∗":
		return `*`
	case "∕":
		return `/`
	case "∞":
		return `\infty `
	case "∑":
		return `\sum `
	case "∏":
		return `\prod `
	case "∫":
		return `\int `
	case "∂":
		return `\partial `
	case "∇":
		return `\nabla `
	case "−":
		return `-`
	case "{":
		return `\{`
	case "}":
		return `\}`
	}
	return strings.NewReplacer(`#`, `\#`, `%`, `\%`, `&`, `\&`, `$`, `\$`, `_`, `\_`).Replace(s)
}
