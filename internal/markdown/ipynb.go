package markdown

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var reANSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

type ipynbDoc struct {
	Metadata struct {
		Kernelspec struct {
			Language string `json:"language"`
		} `json:"kernelspec"`
		LanguageInfo struct {
			Name string `json:"name"`
		} `json:"language_info"`
	} `json:"metadata"`
	Cells []struct {
		CellType string `json:"cell_type"`
		Metadata struct {
			Tags []string `json:"tags"`
		} `json:"metadata"`
		Source  json.RawMessage `json:"source"`
		Outputs []struct {
			OutputType string                     `json:"output_type"`
			Text       json.RawMessage            `json:"text"`
			Data       map[string]json.RawMessage `json:"data"`
		} `json:"outputs"`
	} `json:"cells"`
}

func ipynbJoin(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var lines []string
	if json.Unmarshal(raw, &lines) == nil {
		return strings.Join(lines, "")
	}
	return ""
}

func hasCellTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

// ExtractIpynb converts a Jupyter .ipynb notebook into clean Markdown plus any decoded inline PNG images.
func ExtractIpynb(fp string) (string, string, map[string][]byte, error) {
	data, err := os.ReadFile(fp)
	if err != nil {
		return "", "", nil, err
	}
	var nb ipynbDoc
	if err := json.Unmarshal(data, &nb); err != nil {
		return "", "", nil, err
	}

	lang := strings.ToLower(strings.TrimSpace(nb.Metadata.Kernelspec.Language))
	if lang == "" {
		lang = strings.ToLower(strings.TrimSpace(nb.Metadata.LanguageInfo.Name))
	}
	if lang == "" {
		lang = "python"
	}

	stem := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
	title := strings.ReplaceAll(stem, "_", " ")
	titleFound := false
	inlineImgs := make(map[string][]byte)
	var parts []string

	for _, cell := range nb.Cells {
		if hasCellTag(cell.Metadata.Tags, "remove-cell") {
			continue
		}
		src := ipynbJoin(cell.Source)
		switch cell.CellType {
		case "markdown", "raw":
			tr := strings.TrimSpace(src)
			if tr == "" {
				continue
			}
			if !titleFound {
				if m := reH1Title.FindStringSubmatch(tr); len(m) > 1 {
					title = strings.TrimSpace(m[1])
					titleFound = true
				} else {
					lines := strings.Split(tr, "\n")
					if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[1]), "===") {
						title = strings.TrimSpace(lines[0])
						titleFound = true
					}
				}
			}
			parts = append(parts, tr)

		case "code":
			tr := strings.TrimRight(src, " \t\n\r")
			if tr != "" && !hasCellTag(cell.Metadata.Tags, "remove-input") {
				parts = append(parts, "```"+lang+"\n"+tr+"\n```")
			}
			if hasCellTag(cell.Metadata.Tags, "remove-output") {
				continue
			}
			for _, out := range cell.Outputs {
				if rawPNG, ok := out.Data["image/png"]; ok {
					b64 := strings.ReplaceAll(strings.TrimSpace(ipynbJoin(rawPNG)), "\n", "")
					if decoded, dErr := base64.StdEncoding.DecodeString(b64); dErr == nil && len(decoded) > 64 {
						h := fnv.New64a()
						_, _ = h.Write(decoded)
						imgName := fmt.Sprintf("nb_%016x.png", h.Sum64())
						inlineImgs[imgName] = decoded
						parts = append(parts, fmt.Sprintf("![](%s)", imgName))
						continue
					}
				}
				if out.OutputType == "stream" || out.OutputType == "execute_result" {
					txt := ipynbJoin(out.Text)
					if txt == "" {
						if rawPlain, ok := out.Data["text/plain"]; ok {
							txt = ipynbJoin(rawPlain)
						}
					}
					txt = reANSI.ReplaceAllString(txt, "")
					txt = strings.TrimSpace(txt)
					if txt == "" || strings.HasPrefix(txt, "<") || strings.Contains(txt, "Figure size") || strings.Contains(txt, "<matplotlib.") {
						continue
					}
					lines := strings.Split(txt, "\n")
					if len(lines) > 12 {
						lines = append(lines[:10], "... ("+fmt.Sprintf("%d more lines", len(lines)-10)+")")
					}
					clipped := strings.Join(lines, "\n")
					if len(clipped) <= 900 {
						parts = append(parts, "```text\n"+clipped+"\n```")
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n\n"), title, inlineImgs, nil
}
