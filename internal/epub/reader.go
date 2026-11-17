package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Oyetomi/folio/internal/config"
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

	manifestByID := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		fullHref := item.Href
		if opfDir != "" {
			fullHref = path.Join(opfDir, item.Href)
		}
		manifestByID[item.ID] = fullHref

		if strings.HasPrefix(item.MediaType, "image/") {
			if zf, ok := fileMap[fullHref]; ok {
				if imgData, err := readZipEntry(zf); err == nil {
					baseName := filepath.Base(item.Href)
					_ = os.WriteFile(filepath.Join(workDir, baseName), imgData, 0o644)
					if cfg.CoverImage == "" && (strings.Contains(strings.ToLower(item.ID), "cover") || strings.Contains(strings.ToLower(baseName), "cover") || strings.Contains(item.Properties, "cover-image")) {
						cfg.CoverImage = filepath.Join(workDir, baseName)
					}
				}
			}
		}
	}

	var docs []markdown.ChapterDoc
	chIdx := 0
	for _, ref := range pkg.Spine.Itemrefs {
		href, ok := manifestByID[ref.IDRef]
		if !ok {
			continue
		}
		lowerHref := strings.ToLower(href)
		if strings.Contains(lowerHref, "toc") || strings.Contains(lowerHref, "nav") || strings.Contains(lowerHref, "cover") || strings.Contains(lowerHref, "copyright") {
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
		title, mdText := xhtmlToMarkdown(string(rawXHTML))
		if strings.TrimSpace(mdText) == "" {
			continue
		}
		chIdx++
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chIdx)
		}
		partNum := 1
		if strings.Contains(lowerHref, "front") || strings.Contains(lowerHref, "preface") || strings.Contains(lowerHref, "ack") || strings.Contains(lowerHref, "about") {
			partNum = 0
		}
		docs = append(docs, markdown.ChapterDoc{
			IDTuple:  [3]int{partNum, chIdx, 0},
			HasID:    true,
			PartIdx:  partNum,
			Title:    title,
			FilePath: href,
			RawText:  mdText,
		})
	}

	return cfg, docs, nil
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
	reXHTMLBody  = regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
	reXHTMLChDiv = regexp.MustCompile(`(?is)<div[^>]+class=["'][^"']*chapter[^"']*["'][^>]*>.*?</div>`)
	reXHTMLTable = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	reXHTMLTR    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reXHTMLCell  = regexp.MustCompile(`(?is)<(?:th|td)[^>]*>(.*?)</(?:th|td)>`)
	reXHTMLH1    = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	reXHTMLH2    = regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>`)
	reXHTMLH3    = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`)
	reXHTMLPre   = regexp.MustCompile(`(?is)<pre([^>]*)>(.*?)</pre>`)
	reXHTMLImg   = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["'][^>]*>`)
	reXHTMLAlt   = regexp.MustCompile(`(?is)alt=["']([^"']*)["']`)
	reLangAttr   = regexp.MustCompile(`(?is)(?:language-|lang-|brush:\s*)([a-zA-Z0-9_+-]+)`)
	reXHTMLLI    = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	reXHTMLBQ    = regexp.MustCompile(`(?is)<blockquote[^>]*>(.*?)</blockquote>`)
	reXHTMLP     = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	reCodeSpan   = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	reStrong     = regexp.MustCompile(`(?is)<(?:strong|b)[^>]*>(.*?)</(?:strong|b)>`)
	reEm         = regexp.MustCompile(`(?is)<(?:em|i)[^>]*>(.*?)</(?:em|i)>`)
	reAllTags    = regexp.MustCompile(`(?is)<[^>]+>`)
)

func xhtmlToMarkdown(xhtml string) (string, string) {
	body := xhtml
	if m := reXHTMLBody.FindStringSubmatch(xhtml); len(m) > 1 {
		body = m[1]
	}

	body = reXHTMLChDiv.ReplaceAllString(body, "")

	title := ""
	if loc := reXHTMLH1.FindStringSubmatchIndex(body); len(loc) >= 4 {
		title = strings.TrimSpace(stripTags(body[loc[2]:loc[3]]))
		body = body[:loc[0]] + body[loc[1]:]
	} else if loc := reXHTMLH2.FindStringSubmatchIndex(body); len(loc) >= 4 {
		title = strings.TrimSpace(stripTags(body[loc[2]:loc[3]]))
		body = body[:loc[0]] + body[loc[1]:]
	}

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

	// Convert <table> blocks into Markdown pipe tables
	body = reXHTMLTable.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLTable.FindStringSubmatch(m)
		trMatches := reXHTMLTR.FindAllStringSubmatch(sub[1], -1)
		var rows [][]string
		maxCols := 0
		for _, tr := range trMatches {
			cellMatches := reXHTMLCell.FindAllStringSubmatch(tr[1], -1)
			if len(cellMatches) == 0 {
				continue
			}
			row := make([]string, 0, len(cellMatches))
			for _, cm := range cellMatches {
				val := strings.ReplaceAll(cleanInlineHTML(cm[1]), "|", `\|`)
				row = append(row, val)
			}
			if len(row) > maxCols {
				maxCols = len(row)
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 || maxCols == 0 {
			return ""
		}
		var sb strings.Builder
		sb.WriteString("\n\n")
		for rIdx, row := range rows {
			for len(row) < maxCols {
				row = append(row, "")
			}
			sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
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

	// Convert <blockquote> elements
	body = reXHTMLBQ.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLBQ.FindStringSubmatch(m)
		txt := cleanInlineHTML(sub[1])
		if txt == "" {
			return ""
		}
		return "\n\n> " + txt + "\n\n"
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

	// Convert list items and paragraphs
	body = reXHTMLLI.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLLI.FindStringSubmatch(m)
		return "\n- " + cleanInlineHTML(sub[1])
	})
	body = reXHTMLP.ReplaceAllStringFunc(body, func(m string) string {
		sub := reXHTMLP.FindStringSubmatch(m)
		return "\n\n" + cleanInlineHTML(sub[1]) + "\n\n"
	})

	lines := strings.Split(body, "\n")
	var out []string
	inFence := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			out = append(out, strings.TrimSpace(l))
			continue
		}
		if inFence {
			out = append(out, l)
			continue
		}
		cleaned := strings.TrimSpace(reAllTags.ReplaceAllString(l, ""))
		cleaned = html.UnescapeString(cleaned)
		if cleaned != "" {
			out = append(out, cleaned)
		} else {
			out = append(out, "")
		}
	}

	return title, strings.TrimSpace(strings.Join(out, "\n"))
}

func cleanInlineHTML(s string) string {
	s = reCodeSpan.ReplaceAllStringFunc(s, func(m string) string {
		sub := reCodeSpan.FindStringSubmatch(m)
		return "`" + html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")) + "`"
	})
	s = reStrong.ReplaceAllStringFunc(s, func(m string) string {
		sub := reStrong.FindStringSubmatch(m)
		return "**" + html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")) + "**"
	})
	s = reEm.ReplaceAllStringFunc(s, func(m string) string {
		sub := reEm.FindStringSubmatch(m)
		return "*" + html.UnescapeString(reAllTags.ReplaceAllString(sub[1], "")) + "*"
	})
	s = reAllTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func stripTags(s string) string {
	return strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(s, "")))
}
