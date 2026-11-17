package verify

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type OverlapIssue struct {
	Page   int
	Figure string
	WordA  string
	WordB  string
	Width  float64
	Height float64
}

type VerifyReport struct {
	Pages          int
	FiguresChecked int
	Overlaps       []OverlapIssue
	OverfullHbox   int
}

type bboxDoc struct {
	Pages []struct {
		Words []struct {
			XMin float64 `xml:"xMin,attr"`
			YMin float64 `xml:"yMin,attr"`
			XMax float64 `xml:"xMax,attr"`
			YMax float64 `xml:"yMax,attr"`
			Text string  `xml:",chardata"`
		} `xml:"word"`
	} `xml:"body>doc>page"`
}

var (
	reFigNum   = regexp.MustCompile(`^(?:\d+|[A-Z])\.\d+\.$`)
	reDoctype  = regexp.MustCompile(`<!DOCTYPE[^>]*>`)
	reOverfull = regexp.MustCompile(`Overfull \\hbox \(([0-9.]+)pt too wide\)`)
)

func AuditPDF(pdfPath string, logPath string) (*VerifyReport, error) {
	rep := &VerifyReport{}

	if logPath != "" {
		if data, err := os.ReadFile(logPath); err == nil {
			for _, m := range reOverfull.FindAllStringSubmatch(string(data), -1) {
				pts, _ := strconv.ParseFloat(m[1], 64)
				if pts > 2.0 {
					rep.OverfullHbox++
				}
			}
		}
	}

	pdftotextBin, err := exec.LookPath("pdftotext")
	if err != nil {
		return rep, nil
	}

	out, err := exec.Command(pdftotextBin, "-bbox", pdfPath, "-").Output()
	if err != nil {
		return rep, fmt.Errorf("pdftotext -bbox failed: %w", err)
	}

	xmlStr := reDoctype.ReplaceAllString(string(out), "")
	var doc bboxDoc
	if err := xml.Unmarshal([]byte(xmlStr), &doc); err != nil {
		return rep, nil
	}

	rep.Pages = len(doc.Pages)

	type wordBox struct {
		x1, y1, x2, y2 float64
		text           string
	}
	type figCap struct {
		id   string
		yCap float64
	}

	for pIdx, page := range doc.Pages {
		pno := pIdx + 1
		words := make([]wordBox, 0, len(page.Words))
		for _, w := range page.Words {
			words = append(words, wordBox{w.XMin, w.YMin, w.XMax, w.YMax, strings.TrimSpace(w.Text)})
		}

		var caps []figCap
		for i := 0; i+1 < len(words); i++ {
			if words[i].text == "Figure" && reFigNum.MatchString(words[i+1].text) && math.Abs(words[i].y1-words[i+1].y1) < 3.0 {
				yCap := words[i].y1
				hasLeftWord := false
				for _, w := range words {
					if math.Abs(w.y1-yCap) < 3.0 && w.x1 < words[i].x1-5.0 {
						hasLeftWord = true
						break
					}
				}
				if !hasLeftWord {
					figID := "Figure " + strings.TrimSuffix(words[i+1].text, ".")
					caps = append(caps, figCap{id: figID, yCap: yCap})
				}
			}
		}
		if len(caps) == 0 {
			continue
		}

		sort.Slice(caps, func(i, j int) bool { return caps[i].yCap < caps[j].yCap })
		prevY := 45.0
		for _, fc := range caps {
			rep.FiguresChecked++
			minY := math.Max(prevY, fc.yCap-250.0)
			maxY := fc.yCap - 2.0
			prevY = fc.yCap + 16.0

			var fwords []wordBox
			for _, w := range words {
				if w.y1 >= minY && w.y2 <= maxY {
					fwords = append(fwords, w)
				}
			}
			for a := 0; a < len(fwords); a++ {
				for b := a + 1; b < len(fwords); b++ {
					wa, wb := fwords[a], fwords[b]
					ix := math.Min(wa.x2, wb.x2) - math.Max(wa.x1, wb.x1)
					iy := math.Min(wa.y2, wb.y2) - math.Max(wa.y1, wb.y1)
					if ix > 1.5 && iy > 1.5 && math.Abs(wa.y1-wb.y1) > 1.5 {
						rep.Overlaps = append(rep.Overlaps, OverlapIssue{
							Page:   pno,
							Figure: fc.id,
							WordA:  wa.text,
							WordB:  wb.text,
							Width:  math.Round(ix*10) / 10,
							Height: math.Round(iy*10) / 10,
						})
					}
				}
			}
		}
	}

	return rep, nil
}
