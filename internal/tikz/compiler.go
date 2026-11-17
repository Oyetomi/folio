package tikz

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/Oyetomi/folio/internal/highlight"
)

func GetPreamble() string {
	return `
\usetikzlibrary{arrows.meta,positioning,fit,backgrounds,shapes.geometric,decorations.pathreplacing,calc}

% --- Technical Diagram Palette ---
\definecolor{figborder}{HTML}{BCC4CC}
\definecolor{diagslate}{HTML}{3B5B78}
\definecolor{diagslatefill}{HTML}{EBF1F6}
\definecolor{diaggray}{HTML}{5A636A}
\definecolor{diaggrayfill}{HTML}{F5F6F7}
\definecolor{diagdashed}{HTML}{83919F}
\definecolor{diagsage}{HTML}{3E7048}
\definecolor{diagsagefill}{HTML}{EAF3EC}
\definecolor{diagamber}{HTML}{9E6316}
\definecolor{diagamberfill}{HTML}{FBF4E8}
\definecolor{diagcrimson}{HTML}{8E0012}
\definecolor{diagcrimsonfill}{HTML}{FBEAEA}
\definecolor{diagpurple}{HTML}{5A467A}
\definecolor{diagpurplefill}{HTML}{F2EFF8}

% Uniform full-\textwidth hairline figure frame with smart width capping
\newsavebox{\foliofigbox}
\newcommand{\renderdiagramframe}[1]{%
  \savebox{\foliofigbox}{#1}%
  \ifdim\wd\foliofigbox>5.05in
    \savebox{\foliofigbox}{\resizebox{5.05in}{!}{\usebox{\foliofigbox}}}%
  \fi
  \ifdim\ht\foliofigbox>5.5in
    \savebox{\foliofigbox}{\resizebox{!}{5.5in}{\usebox{\foliofigbox}}}%
  \fi
  \noindent\begin{tikzpicture}
    \node[draw=figborder,line width=0.45pt,rounded corners=2pt,fill=white,inner xsep=8pt,inner ysep=9pt,minimum width=\textwidth,align=center] {%
      \usebox{\foliofigbox}%
    };
  \end{tikzpicture}%
}

\tikzset{
  folioarrow/.style={->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.65pt,draw=diaggray},
  foliobotharrow/.style={<->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.65pt,draw=diagslate},
  folioptr/.style={->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.7pt,draw=diagcrimson},
  folioslatearrow/.style={->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.65pt,draw=diagslate},
  foliosagearrow/.style={->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.65pt,draw=diagsage},
  folioamberarrow/.style={->,>={Latex[length=4.5pt,width=3.2pt]},line width=0.65pt,draw=diagamber},
  foliobox/.style={draw=diagslate,fill=diagslatefill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxgray/.style={draw=diaggray,fill=diaggrayfill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxsage/.style={draw=diagsage,fill=diagsagefill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxamber/.style={draw=diagamber,fill=diagamberfill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxcrimson/.style={draw=diagcrimson,fill=diagcrimsonfill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxpurple/.style={draw=diagpurple,fill=diagpurplefill,rounded corners=2.5pt,line width=0.6pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont},
  folioboxdashed/.style={draw=diagdashed,dashed,fill=white,rounded corners=2.5pt,line width=0.55pt,align=center,inner sep=4.5pt,font=\fontsize{8pt}{9.6pt}\selectfont}
}
`
}

func sf(text, size string, bold, italic bool, color string) string {
	face := `\sanscond`
	if bold {
		face = `\sanssemiboldcond`
	}
	it := ""
	if italic {
		it = `\itshape`
	}
	norm := strings.ReplaceAll(text, "<br/>", "\n")
	norm = strings.ReplaceAll(norm, "<br>", "\n")
	norm = strings.ReplaceAll(norm, `\n`, "\n")
	parts := strings.Split(norm, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, fmt.Sprintf(`{%s%s\fontsize{%s}{9.8pt}\selectfont\textcolor{%s}{%s}}`, face, it, size, color, highlight.LatexEscape(strings.TrimSpace(p))))
	}
	return strings.Join(out, `\\`)
}

func tt(text, size, color string) string {
	norm := strings.ReplaceAll(text, "<br/>", "\n")
	norm = strings.ReplaceAll(norm, "<br>", "\n")
	norm = strings.ReplaceAll(norm, `\n`, "\n")
	parts := strings.Split(norm, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, fmt.Sprintf(`{\ttfamily\fontsize{%s}{9.6pt}\selectfont\textcolor{%s}{%s}}`, size, color, highlight.CodeEscapeInline(strings.TrimSpace(p))))
	}
	return strings.Join(out, `\\`)
}

func wrapTikZ(body string) string {
	return "\\begin{tikzpicture}\n" + body + "\n\\end{tikzpicture}"
}

func WrapFigure(tikzCode string, caption string) string {
	if caption == "" {
		return WrapFigureTex(tikzCode, "")
	}
	return WrapFigureTex(tikzCode, highlight.LatexEscape(caption))
}

func WrapFigureTex(tikzCode string, captionTex string) string {
	var b strings.Builder
	b.WriteString("\\par\\medskip\\noindent\\begin{minipage}{\\textwidth}\\centering\n")
	b.WriteString(fmt.Sprintf("\\renderdiagramframe{%s}\n", tikzCode))
	if captionTex != "" {
		b.WriteString(fmt.Sprintf("\\par\\vspace{4pt}{\\itshape\\fontsize{8.8pt}{11pt}\\selectfont{\\color{diaggray}%s}}\n", captionTex))
	}
	b.WriteString("\\end{minipage}\\par\\medskip\n")
	return b.String()
}

// ===========================================================================
// Sequence Diagram Compiler
// ===========================================================================

type seqEvent struct {
	kind   string // "loop_start", "loop_end", "msg", "self_msg", "note", "loop_box"
	src    string
	dst    string
	dashed bool
	text   string
	pList  []string
	y      float64
	yTop   float64
	yBot   float64
}

var (
	reSeqPart = regexp.MustCompile(`^participant\s+([A-Za-z0-9_]+)(?:\s+as\s+(.+))?$`)
	reSeqLoop = regexp.MustCompile(`^loop\s+(.+)$`)
	reSeqNote = regexp.MustCompile(`(?i)^Note\s+over\s+([^:]+):\s*(.+)$`)
	reSeqMsg  = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*(-->>|->>|-->|->)\s*([A-Za-z0-9_]+)\s*:\s*(.+)$`)
)

func RenderSequenceDiagram(mermaidBody string) string {
	var participants []string
	partLabels := make(map[string]string)

	ensurePart := func(pid, lbl string) {
		if _, ok := partLabels[pid]; !ok {
			participants = append(participants, pid)
			if lbl != "" {
				partLabels[pid] = lbl
			} else {
				partLabels[pid] = pid
			}
		} else if lbl != "" {
			partLabels[pid] = lbl
		}
	}

	var rawEvents []seqEvent
	for _, rawLine := range strings.Split(mermaidBody, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || line == "sequenceDiagram" {
			continue
		}
		if m := reSeqPart.FindStringSubmatch(line); len(m) > 0 {
			ensurePart(m[1], strings.TrimSpace(m[2]))
			continue
		}
		if m := reSeqLoop.FindStringSubmatch(line); len(m) > 0 {
			rawEvents = append(rawEvents, seqEvent{kind: "loop_start", text: strings.TrimSpace(m[1])})
			continue
		}
		if line == "end" {
			rawEvents = append(rawEvents, seqEvent{kind: "loop_end"})
			continue
		}
		if m := reSeqNote.FindStringSubmatch(line); len(m) > 0 {
			var pList []string
			for _, p := range strings.Split(m[1], ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					ensurePart(p, "")
					pList = append(pList, p)
				}
			}
			rawEvents = append(rawEvents, seqEvent{kind: "note", pList: pList, text: strings.TrimSpace(m[2])})
			continue
		}
		if m := reSeqMsg.FindStringSubmatch(line); len(m) > 0 {
			src, arr, dst, msg := m[1], m[2], m[3], strings.TrimSpace(m[4])
			ensurePart(src, "")
			ensurePart(dst, "")
			rawEvents = append(rawEvents, seqEvent{
				kind:   "msg",
				src:    src,
				dst:    dst,
				dashed: strings.HasPrefix(arr, "--"),
				text:   msg,
			})
		}
	}

	n := len(participants)
	if n == 0 {
		return wrapTikZ("")
	}

	dx := 2.65
	switch n {
	case 2:
		dx = 5.6
	case 3:
		dx = 4.3
	case 4:
		dx = 3.2
	}
	boxW := math.Min(2.45, dx-0.35)
	xPos := make(map[string]float64)
	for i, pid := range participants {
		xPos[pid] = float64(i) * dx
	}

	var L []string
	palette := []string{"foliobox", "folioboxsage", "folioboxamber", "folioboxpurple", "folioboxgray"}
	for i, pid := range participants {
		st := palette[i%len(palette)]
		lbl := partLabels[pid]
		if len(lbl) > 18 && strings.Contains(lbl, " (") {
			lbl = strings.Replace(lbl, " (", "\n(", 1)
		}
		L = append(L, fmt.Sprintf(`\node[%s,minimum width=%.2fcm,minimum height=0.78cm] (p_%s) at (%.2f,0) {%s};`,
			st, boxW, pid, xPos[pid], sf(lbl, "8.0pt", true, false, "bodyblack")))
	}

	yCur := -0.58
	var rendered []seqEvent
	var loopStack []seqEvent

	for _, ev := range rawEvents {
		switch ev.kind {
		case "loop_start":
			yCur -= 0.42
			loopStack = append(loopStack, seqEvent{text: ev.text, yTop: yCur + 0.22})
		case "loop_end":
			if len(loopStack) > 0 {
				top := loopStack[len(loopStack)-1]
				loopStack = loopStack[:len(loopStack)-1]
				rendered = append(rendered, seqEvent{kind: "loop_box", text: top.text, yTop: top.yTop, yBot: yCur - 0.25})
				yCur -= 0.32
			}
		case "msg":
			if ev.src == ev.dst {
				yCur -= 0.45
				ev.kind = "self_msg"
				ev.y = yCur
				rendered = append(rendered, ev)
				yCur -= 0.52
			} else {
				yCur -= 0.68
				ev.y = yCur
				rendered = append(rendered, ev)
			}
		case "note":
			yCur -= 0.68
			ev.y = yCur
			rendered = append(rendered, ev)
			yCur -= 0.12
		}
	}

	yBottom := yCur - 0.45
	for _, pid := range participants {
		L = append(L, fmt.Sprintf(`\draw[diagdashed,dashed,line width=0.55pt] (p_%s.south) -- (%.2f,%.2f);`, pid, xPos[pid], yBottom))
	}

	minX := -boxW/2.0 - 0.35
	maxX := float64(n-1)*dx + boxW/2.0 + 0.35

	for _, item := range rendered {
		switch item.kind {
		case "loop_box":
			L = append(L, fmt.Sprintf(`\draw[draw=diagslate,dashed,line width=0.55pt,rounded corners=2.5pt] (%.2f,%.2f) rectangle (%.2f,%.2f);`,
				minX, item.yBot, maxX, item.yTop))
			L = append(L, fmt.Sprintf(`\node[fill=diagslatefill,draw=diagslate,rounded corners=1.5pt,anchor=north west,inner xsep=4pt,inner ysep=1.5pt] at (%.2f,%.2f) {%s};`,
				minX, item.yTop, sf("loop: "+item.text, "7.2pt", true, false, "diagslate")))
		case "self_msg":
			sx := xPos[item.src]
			L = append(L, fmt.Sprintf(`\draw[folioslatearrow] (%.2f,%.2f) -- (%.2f,%.2f) -- (%.2f,%.2f) -- (%.2f,%.2f);`,
				sx, item.y+0.16, sx+0.68, item.y+0.16, sx+0.68, item.y-0.18, sx, item.y-0.18))
			L = append(L, fmt.Sprintf(`\node[anchor=west,fill=white,inner sep=1.5pt] at (%.2f,%.2f) {%s};`,
				sx+0.74, item.y-0.01, sf(item.text, "7.5pt", false, false, "bodyblack")))
		case "msg":
			sx, dxP := xPos[item.src], xPos[item.dst]
			style := "folioslatearrow"
			if item.dashed {
				style = "foliosagearrow,dashed"
			}
			L = append(L, fmt.Sprintf(`\draw[%s] (%.2f,%.2f) -- (%.2f,%.2f);`, style, sx, item.y, dxP, item.y))
			midX := (sx + dxP) / 2.0
			lblTex := sf(item.text, "7.6pt", false, false, "bodyblack")
			if strings.Contains(item.text, "{") || strings.Contains(item.text, "/") {
				lblTex = tt(item.text, "7.3pt", "bodyblack")
			}
			L = append(L, fmt.Sprintf(`\node[anchor=south,fill=white,inner sep=1.5pt] at (%.2f,%.2f) {%s};`, midX, item.y+0.04, lblTex))
		case "note":
			var xs []float64
			for _, p := range item.pList {
				if v, ok := xPos[p]; ok {
					xs = append(xs, v)
				}
			}
			if len(xs) > 0 {
				nx1, nx2 := xs[0], xs[0]
				for _, v := range xs {
					if v < nx1 {
						nx1 = v
					}
					if v > nx2 {
						nx2 = v
					}
				}
				nx1 -= 1.15
				nx2 += 1.15
				ncx := (nx1 + nx2) / 2.0
				nw := math.Max(3.6, nx2-nx1)
				L = append(L, fmt.Sprintf(`\node[folioboxamber,minimum width=%.2fcm,minimum height=0.52cm,inner sep=3.5pt] at (%.2f,%.2f) {%s};`,
					nw, ncx, item.y, sf(item.text, "7.5pt", false, true, "diagamber")))
			}
		}
	}

	return wrapTikZ(strings.Join(L, "\n"))
}

// ===========================================================================
// Automatic Layered Flowchart & Mermaid Graph Compiler
// ===========================================================================

type flowEdge struct {
	from   string
	to     string
	label  string
	dashed bool
	line   bool
}

var (
	reNodeDef = regexp.MustCompile(`([A-Za-z0-9_]+)\s*(?:\["([^"]+)"\]|\[([^\]]+)\]|\(\(([^)]+)\)\)|\("([^"]+)"\)|\(([^)]+)\)|\{([^}]+)\})`)
	reEdgePipe = regexp.MustCompile(`([A-Za-z0-9_]+)\s*(-\.->|-->|---|==>)\s*(?:\|"([^"]+)"\||\|([^|]+)\|)?\s*([A-Za-z0-9_]+)`)
	reEdgeMid  = regexp.MustCompile(`([A-Za-z0-9_]+)\s*--\s*([^-~>]+?)\s*-->\s*([A-Za-z0-9_]+)`)
)

func RenderMermaid(mermaidBody string) string {
	body := strings.TrimSpace(mermaidBody)
	if strings.HasPrefix(body, "sequenceDiagram") {
		return RenderSequenceDiagram(body)
	}
	return renderFlowchart(body)
}

func renderFlowchart(body string) string {
	isLR := false
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && (strings.Contains(lines[0], "LR") || strings.Contains(lines[0], "RL")) {
		isLR = true
	}

	var nodeOrder []string
	nodeLabels := make(map[string]string)
	ensureNode := func(id, lbl string) {
		if id == "subgraph" || id == "end" || id == "flowchart" || id == "graph" {
			return
		}
		if _, ok := nodeLabels[id]; !ok {
			nodeOrder = append(nodeOrder, id)
			if lbl != "" {
				nodeLabels[id] = lbl
			} else {
				nodeLabels[id] = id
			}
		} else if lbl != "" && nodeLabels[id] == id {
			nodeLabels[id] = lbl
		}
	}

	var edges []flowEdge
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "flowchart") || strings.HasPrefix(line, "graph") || line == "end" {
			continue
		}
		if strings.HasPrefix(line, "subgraph") {
			continue
		}

		// Extract all inline node definitions like A["Client"] or B[Router]
		for _, m := range reNodeDef.FindAllStringSubmatch(line, -1) {
			id := m[1]
			lbl := firstNonEmpty(m[2], m[3], m[4], m[5], m[6], m[7])
			ensureNode(id, lbl)
		}

		// Strip node bracket definitions so edge regexes see clean IDs
		cleanLine := reNodeDef.ReplaceAllString(line, "$1")

		if m := reEdgeMid.FindStringSubmatch(cleanLine); len(m) > 0 {
			u, lbl, v := m[1], strings.TrimSpace(m[2]), m[3]
			ensureNode(u, "")
			ensureNode(v, "")
			edges = append(edges, flowEdge{from: u, to: v, label: lbl})
			continue
		}
		if m := reEdgePipe.FindStringSubmatch(cleanLine); len(m) > 0 {
			u, arr, l1, l2, v := m[1], m[2], m[3], m[4], m[5]
			lbl := strings.TrimSpace(firstNonEmpty(l1, l2))
			ensureNode(u, "")
			ensureNode(v, "")
			edges = append(edges, flowEdge{
				from:   u,
				to:     v,
				label:  lbl,
				dashed: arr == "-.->",
				line:   arr == "---",
			})
		}
	}

	if len(nodeOrder) == 0 {
		return wrapTikZ("")
	}

	// Assign longest-path topological ranks (with cycle guard)
	rank := make(map[string]int)
	inDeg := make(map[string]int)
	outDeg := make(map[string]int)
	edgePair := make(map[string]bool)
	firstPairDir := make(map[string]bool)
	dagAdj := make(map[string][]string)
	isBackEdge := make([]bool, len(edges))

	var canReach func(src, dst string, visited map[string]bool) bool
	canReach = func(src, dst string, visited map[string]bool) bool {
		if src == dst {
			return true
		}
		visited[src] = true
		for _, nxt := range dagAdj[src] {
			if !visited[nxt] && canReach(nxt, dst, visited) {
				return true
			}
		}
		return false
	}

	for idx, e := range edges {
		inDeg[e.to]++
		outDeg[e.from]++
		fwd := e.from + "->" + e.to
		rev := e.to + "->" + e.from
		edgePair[fwd] = true
		if !firstPairDir[fwd] && !firstPairDir[rev] {
			firstPairDir[fwd] = true
		}
		if e.from == e.to || canReach(e.to, e.from, make(map[string]bool)) {
			isBackEdge[idx] = true
		} else {
			dagAdj[e.from] = append(dagAdj[e.from], e.to)
		}
	}
	for iter := 0; iter < len(nodeOrder); iter++ {
		changed := false
		for idx, e := range edges {
			if isBackEdge[idx] {
				continue
			}
			if rank[e.from]+1 > rank[e.to] && rank[e.from]+1 < len(nodeOrder) {
				rank[e.to] = rank[e.from] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Group nodes by rank, wrapping ranks wider than 4 nodes onto sub-ranks
	byRank := make(map[int][]string)
	maxRank := 0
	for _, id := range nodeOrder {
		r := rank[id]
		byRank[r] = append(byRank[r], id)
		if r > maxRank {
			maxRank = r
		}
	}

	hasBidirLabels := false
	for _, e := range edges {
		if e.label != "" && edgePair[e.to+"->"+e.from] {
			hasBidirLabels = true
			break
		}
	}

	type coord struct{ x, y float64 }
	pos := make(map[string]coord)
	var L []string
	palette := []string{"foliobox", "folioboxsage", "folioboxamber", "folioboxpurple", "folioboxgray"}

	if isLR {
		dx := 3.45
		dy := 1.50
		for r := 0; r <= maxRank; r++ {
			nodes := byRank[r]
			for idx, id := range nodes {
				x := float64(r) * dx
				y := (float64(len(nodes)-1)/2.0 - float64(idx)) * dy
				pos[id] = coord{x, y}
			}
		}
	} else {
		// Top-to-bottom layered layout (split any layer > 4 nodes into multiple rows)
		var rows [][]string
		for r := 0; r <= maxRank; r++ {
			nodes := byRank[r]
			for start := 0; start < len(nodes); start += 4 {
				end := start + 4
				if end > len(nodes) {
					end = len(nodes)
				}
				rows = append(rows, nodes[start:end])
			}
		}
		dy := 1.75
		if hasBidirLabels {
			dy = 2.55
		}
		for rIdx, rowNodes := range rows {
			dx := 3.25
			if hasBidirLabels {
				dx = 3.55
			}
			if len(rowNodes) >= 4 {
				dx = 2.85
			}
			for cIdx, id := range rowNodes {
				x := (float64(cIdx) - float64(len(rowNodes)-1)/2.0) * dx
				y := -float64(rIdx) * dy
				pos[id] = coord{x, y}
			}
		}
	}

	for i, id := range nodeOrder {
		c := pos[id]
		st := palette[i%len(palette)]
		if inDeg[id] == 0 {
			st = "foliobox"
		} else if outDeg[id] == 0 {
			st = "folioboxsage"
		}
		lbl := nodeLabels[id]
		bw := 2.35
		if isLR {
			bw = 2.15
		}
		L = append(L, fmt.Sprintf(`\node[%s,minimum width=%.2fcm,minimum height=0.80cm] (n_%s) at (%.2f,%.2f) {%s};`,
			st, bw, id, c.x, c.y, sf(lbl, "7.9pt", true, false, "bodyblack")))
	}

	// Render edges cleanly
	for idx, e := range edges {
		fwd := e.from + "->" + e.to
		rev := e.to + "->" + e.from
		isBidir := edgePair[rev]
		isFirstDir := firstPairDir[fwd]

		style := "folioslatearrow"
		if e.dashed {
			style = "folioptr,dashed"
		} else if e.line {
			style = "diagslate,line width=0.65pt"
		}

		elbl := e.label
		if len(elbl) > 14 && strings.Contains(elbl, " ") {
			words := strings.Fields(elbl)
			mid := len(words) / 2
			elbl = strings.Join(words[:mid], " ") + "\n" + strings.Join(words[mid:], " ")
		}

		if isBidir {
			if isLR {
				posOpt := "midway,above=1.5pt"
				if !isFirstDir {
					posOpt = "midway,below=1.5pt"
				}
				lblNode := ""
				if elbl != "" {
					lblNode = fmt.Sprintf(` node[%s,fill=white,inner sep=1.2pt,align=center] {%s}`, posOpt, sf(elbl, "7.0pt", false, false, "diagslate"))
				}
				L = append(L, fmt.Sprintf(`\draw[%s] (n_%s) to[bend left=16]%s (n_%s);`, style, e.from, lblNode, e.to))
			} else {
				posVal := "0.27"
				if !isFirstDir {
					posVal = "0.58"
				}
				lblNode := ""
				if elbl != "" {
					lblNode = fmt.Sprintf(` node[pos=%s,fill=white,inner sep=1.2pt,align=center] {%s}`, posVal, sf(elbl, "7.0pt", false, false, "diagslate"))
				}
				L = append(L, fmt.Sprintf(`\draw[%s] (n_%s) to[bend right=14]%s (n_%s);`, style, e.from, lblNode, e.to))
			}
		} else if isBackEdge[idx] {
			lblNode := ""
			if isLR {
				if elbl != "" {
					lblNode = fmt.Sprintf(` node[midway,below=1.5pt,fill=white,inner sep=1.2pt,align=center] {%s}`, sf(elbl, "7.0pt", false, false, "diagslate"))
				}
				L = append(L, fmt.Sprintf(`\draw[%s] (n_%s.south) to[bend left=24]%s (n_%s.south);`, style, e.from, lblNode, e.to))
			} else {
				if elbl != "" {
					lblNode = fmt.Sprintf(` node[midway,fill=white,inner sep=1.2pt,align=center] {%s}`, sf(elbl, "7.0pt", false, false, "diagslate"))
				}
				L = append(L, fmt.Sprintf(`\draw[%s] (n_%s) to[bend right=22]%s (n_%s);`, style, e.from, lblNode, e.to))
			}
		} else {
			lblNode := ""
			if elbl != "" {
				lblNode = fmt.Sprintf(` node[midway,fill=white,inner sep=1.5pt,align=center] {%s}`, sf(elbl, "7.1pt", false, false, "diagslate"))
			}
			L = append(L, fmt.Sprintf(`\draw[%s] (n_%s) -- (n_%s)%s;`, style, e.from, e.to, lblNode))
		}
	}

	return wrapTikZ(strings.Join(L, "\n"))
}

// ===========================================================================
// General ASCII Diagram Detector & Vector Compiler
// ===========================================================================

var (
	reASCIISeqLine  = regexp.MustCompile(`^\[([^\]]+)\]\s*([<=-]+)\(?([^)\n-]*)\)?([>=-]+)\s*\[([^\]]+)\]$`)
	reASCIIChainBox = regexp.MustCompile(`\[([^\]]+)\]`)
	reASCIILayerBox = regexp.MustCompile(`^\|\s*([0-9]+\.\s*[^|]+?)\s*\|$`)
)

func IsASCIIDiagram(body string) bool {
	s := strings.TrimSpace(body)
	if strings.Contains(s, "package ") || strings.Contains(s, "func ") || strings.Contains(s, "SELECT ") || strings.Contains(s, "FROM ") || strings.Contains(s, "def ") || strings.Contains(s, "fn ") || strings.Contains(s, "├──") || strings.Contains(s, "└──") {
		return false
	}
	if strings.Contains(s, "+---") && strings.Contains(s, "|") {
		return true
	}
	if strings.Contains(s, "[") && strings.Contains(s, "]") && (strings.Contains(s, "-->") || strings.Contains(s, "<--") || strings.Contains(s, "<==") || strings.Contains(s, "==>") || strings.Contains(s, "---") || strings.Contains(s, "->") || strings.Contains(s, "\n   |") || strings.Contains(s, "\n    |")) {
		return true
	}
	if strings.HasPrefix(s, "[8 bytes:") {
		return true
	}
	return false
}

func RenderASCIIDiagram(body string) string {
	s := strings.TrimSpace(body)
	lines := strings.Split(s, "\n")

	// 1. Check if this is an ASCII protocol stack table (+---+ | 7. Application | +---+)
	var stackLayers []string
	for _, l := range lines {
		if m := reASCIILayerBox.FindStringSubmatch(strings.TrimSpace(l)); len(m) > 0 {
			stackLayers = append(stackLayers, strings.TrimSpace(m[1]))
		}
	}
	if len(stackLayers) >= 3 {
		var L []string
		pal := []string{"folioboxsage", "folioboxsage", "folioboxamber", "foliobox", "folioboxpurple", "folioboxgray"}
		for idx, name := range stackLayers {
			y := -float64(idx) * 0.68
			st := pal[idx%len(pal)]
			L = append(L, fmt.Sprintf(`\node[%s,minimum width=4.4cm,minimum height=0.56cm] at (0,%.2f) {%s};`,
				st, y, sf(name, "8.1pt", true, false, "bodyblack")))
		}
		return wrapTikZ(strings.Join(L, "\n"))
	}

	// 2. Check if this is a multi-step ASCII sequence exchange ([Client] --SYN--> [Server])
	seqCount := 0
	var seqSB strings.Builder
	seqSB.WriteString("sequenceDiagram\n")
	for _, l := range lines {
		tl := strings.TrimSpace(l)
		if m := reASCIISeqLine.FindStringSubmatch(tl); len(m) > 0 {
			left, lArr, mid, rArr, right := strings.TrimSpace(m[1]), m[2], strings.TrimSpace(m[3]), m[4], strings.TrimSpace(m[5])
			msg := strings.Trim(mid, "-=<> ")
			if msg == "" {
				msg = "Packet"
			}
			p1 := sanitizeID(left)
			p2 := sanitizeID(right)
			seqSB.WriteString(fmt.Sprintf("participant %s as %s\n", p1, left))
			seqSB.WriteString(fmt.Sprintf("participant %s as %s\n", p2, right))
			if strings.Contains(lArr, "<") && !strings.Contains(rArr, ">") {
				seqSB.WriteString(fmt.Sprintf("%s-->>%s: %s\n", p2, p1, msg))
			} else {
				seqSB.WriteString(fmt.Sprintf("%s->>%s: %s\n", p1, p2, msg))
			}
			seqCount++
		} else if strings.HasPrefix(tl, "(") && strings.HasSuffix(tl, ")") && seqCount > 0 {
			seqSB.WriteString(fmt.Sprintf("Note over %s: %s\n", "Client", strings.Trim(tl, "()")))
		}
	}
	if seqCount >= 2 {
		return RenderSequenceDiagram(seqSB.String())
	}

	// 3. Extract all [Box] nodes in reading order and render a clean vector chain/grid
	var boxes []string
	seen := make(map[string]bool)
	for _, m := range reASCIIChainBox.FindAllStringSubmatch(s, -1) {
		lbl := strings.TrimSpace(m[1])
		if lbl != "" && !seen[lbl] {
			seen[lbl] = true
			boxes = append(boxes, lbl)
		}
	}
	if len(boxes) == 0 {
		return wrapTikZ("")
	}

	var L []string
	pal := []string{"foliobox", "folioboxsage", "folioboxamber", "folioboxpurple", "folioboxgray"}
	cols := len(boxes)
	if cols > 4 {
		cols = 4
	}
	dx := 3.35
	if cols == 2 {
		dx = 5.4
	} else if cols == 3 {
		dx = 4.2
	}
	dy := 1.45

	for idx, lbl := range boxes {
		r := idx / cols
		c := idx % cols
		x := float64(c) * dx
		y := -float64(r) * dy
		st := pal[idx%len(pal)]
		L = append(L, fmt.Sprintf(`\node[%s,minimum width=2.35cm,minimum height=0.78cm] (b%d) at (%.2f,%.2f) {%s};`,
			st, idx, x, y, sf(lbl, "7.9pt", true, false, "bodyblack")))
		if idx > 0 {
			prevR := (idx - 1) / cols
			if prevR == r {
				L = append(L, fmt.Sprintf(`\draw[folioslatearrow] (b%d.east) -- (b%d.west);`, idx-1, idx))
			} else {
				L = append(L, fmt.Sprintf(`\draw[folioslatearrow] (b%d.south) -- ++(0,-0.35) -| (b%d.north);`, idx-1, idx))
			}
		}
	}

	return wrapTikZ(strings.Join(L, "\n"))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sanitizeID(s string) string {
	res := regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(s, "")
	if res == "" {
		return "Node"
	}
	return res
}
