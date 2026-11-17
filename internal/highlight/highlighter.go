package highlight

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func LatexEscape(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	for _, ch := range text {
		switch ch {
		case '\\':
			b.WriteString(`\textbackslash{}`)
		case '&':
			b.WriteString(`\&`)
		case '%':
			b.WriteString(`\%`)
		case '$':
			b.WriteString(`\$`)
		case '#':
			b.WriteString(`\#`)
		case '_':
			b.WriteString(`\_`)
		case '{':
			b.WriteString(`\{`)
		case '}':
			b.WriteString(`\}`)
		case '~':
			b.WriteString(`\textasciitilde{}`)
		case '^':
			b.WriteString(`\textasciicircum{}`)
		case '<':
			b.WriteString(`\textless{}`)
		case '>':
			b.WriteString(`\textgreater{}`)
		case '→':
			b.WriteString(`$\rightarrow$`)
		case '←':
			b.WriteString(`$\leftarrow$`)
		case '↔':
			b.WriteString(`$\leftrightarrow$`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func CodeEscapeInline(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	for _, ch := range text {
		switch ch {
		case '\\':
			b.WriteString(`\textbackslash{}`)
		case '{':
			b.WriteString(`\{`)
		case '}':
			b.WriteString(`\}`)
		case '$':
			b.WriteString(`\$`)
		case '&':
			b.WriteString(`\&`)
		case '#':
			b.WriteString(`\#`)
		case '^':
			b.WriteString(`\textasciicircum{}`)
		case '_':
			b.WriteString(`\_`)
		case '%':
			b.WriteString(`\%`)
		case '~':
			b.WriteString(`\textasciitilde{}`)
		case '<':
			b.WriteString(`\textless{}`)
		case '>':
			b.WriteString(`\textgreater{}`)
		case '`':
			b.WriteString(`\textasciigrave{}`)
		case '\'':
			b.WriteString(`\textquotesingle{}`)
		case '"':
			b.WriteString(`\textquotedbl{}`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func VerbEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, ch := range s {
		switch ch {
		case '\\':
			b.WriteString(`\textbackslash{}`)
		case '{':
			b.WriteString(`\{`)
		case '}':
			b.WriteString(`\}`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func isPascalIdentifier(s string) bool {
	if len(s) < 2 {
		return false
	}
	runes := []rune(s)
	if !unicode.IsUpper(runes[0]) {
		return false
	}
	hasLower := false
	for _, r := range runes[1:] {
		if unicode.IsLower(r) {
			hasLower = true
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return hasLower
}

func styleToken(tt chroma.TokenType, val string, nextNonWS string) string {
	if val == "" {
		return ""
	}
	esc := VerbEscape(val)
	switch {
	case tt == chroma.Comment || tt.InCategory(chroma.Comment):
		return fmt.Sprintf(`\textit{\textcolor{cmcol}{%s}}`, esc)
	case tt == chroma.KeywordType || tt == chroma.KeywordConstant || tt == chroma.NameBuiltinPseudo || tt == chroma.NameClass || tt == chroma.NameException:
		return fmt.Sprintf(`\textcolor{typecol}{%s}`, esc)
	case tt == chroma.Keyword || tt.InCategory(chroma.Keyword) || tt == chroma.OperatorWord:
		return fmt.Sprintf(`\textbf{\textcolor{kwcol}{%s}}`, esc)
	case tt == chroma.NameTag:
		return fmt.Sprintf(`\textbf{\textcolor{kwcol}{%s}}`, esc)
	case tt == chroma.NameBuiltin || tt == chroma.NameFunction || tt == chroma.NameFunctionMagic || tt == chroma.NameNamespace || tt == chroma.NameDecorator || tt == chroma.NameAttribute:
		return fmt.Sprintf(`\textcolor{fncol}{%s}`, esc)
	case tt.InSubCategory(chroma.LiteralString):
		return fmt.Sprintf(`\textcolor{strcol}{%s}`, esc)
	case tt.InSubCategory(chroma.LiteralNumber):
		return fmt.Sprintf(`\textcolor{numcol}{%s}`, esc)
	case tt == chroma.NameOther || tt == chroma.Name:
		if strings.HasPrefix(nextNonWS, "(") {
			return fmt.Sprintf(`\textcolor{fncol}{%s}`, esc)
		}
		if isPascalIdentifier(val) {
			return fmt.Sprintf(`\textcolor{typecol}{%s}`, esc)
		}
		return esc
	default:
		return esc
	}
}

var (
	reInferSQL    = regexp.MustCompile(`(?im)^\s*(SELECT|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|CREATE\s+(TABLE|INDEX|VIEW|DATABASE|EXTENSION|TYPE)|ALTER\s+TABLE|DROP\s+TABLE|BEGIN|COMMIT|WITH\s+\w+\s+AS)\b`)
	reInferGo     = regexp.MustCompile(`(?m)(^\s*package\s+\w+|^\s*import\s+[\("]|\bfunc\s+[\(\w]|\bdefer\s+|\bgo\s+func\b|:=|\bfmt\.|\bhttp\.|\bnet\.|\bcontext\.|\bsync\.|\bstruct\s*\{|\binterface\s*\{|^\s*(type|var|const)\s+[A-Za-z_]\w*\s+[A-Za-z_\*\[\]])`)
	reInferGoMod  = regexp.MustCompile(`(?m)^\s*module\s+\S+`)
	reInferRust   = regexp.MustCompile(`(?m)(\bfn\s+\w+\s*\(|\bpub\s+(fn|struct|enum|mod|trait|use)\b|\blet\s+mut\b|\bimpl\b|\buse\s+std::|println!\()`)
	reInferPython = regexp.MustCompile(`(?m)(^\s*def\s+\w+\s*\(|^\s*from\s+[\w.]+\s+import\s+|\belif\s+|\bself\.|if\s+__name__\s*==)`)
	reInferTS     = regexp.MustCompile(`(?m)(^\s*(export\s+)?(const|let|async\s+function|function|interface)\s+\w+|\bconsole\.log\(|\b=>\s*[\{\w])`)
	reInferCPP    = regexp.MustCompile(`(?m)(^\s*#include\s+[<"]|\bstd::\w+|\bint\s+main\s*\()`)
	reInferJava   = regexp.MustCompile(`(?m)(\bpublic\s+(class|interface|enum|static\s+void\s+main)\b|\bSystem\.out\.print)`)
	reInferDocker = regexp.MustCompile(`(?m)^\s*(FROM|WORKDIR|RUN|COPY|ENTRYPOINT|CMD|EXPOSE|ENV|ARG)\s+`)
	reInferYAML   = regexp.MustCompile(`(?m)^\s*[a-zA-Z0-9_-]+:\s*(\n|\r|$)`)
	reInferShell  = regexp.MustCompile(`(?m)^(\s*\$\s+\S+|\s*(curl|mkdir|cd|ls|cat|touch|rm|cp|mv|chmod|sudo|brew|apt|yum|apk|docker|kubectl|git|go\s+(run|build|test|mod|get|install|version|vet|fmt|doc|env|tool)|npm|yarn|pnpm|pip|python3?|cargo|rustc|make|psql|sqlite3|redis-cli)\b)`)
	reShellToken  = regexp.MustCompile(`("(?:\\.|[^"\\])*")|('(?:\\.|[^'\\])*')|(https?://\S+|localhost:\d+\S*)|(\s--?[a-zA-Z0-9_-]+(?:=\S+)?)|(\b\d+(?:\.\d+)*\b)`)
)

var shellCmdVerbs = map[string]bool{
	"curl": true, "mkdir": true, "cd": true, "ls": true, "cat": true, "touch": true,
	"rm": true, "cp": true, "mv": true, "chmod": true, "chown": true, "sudo": true,
	"brew": true, "apt": true, "apt-get": true, "yum": true, "apk": true, " winget": true,
	"docker": true, "kubectl": true, "helm": true, "terraform": true, "git": true,
	"go": true, "gofmt": true, "npm": true, "npx": true, "yarn": true, "pnpm": true,
	"pip": true, "pip3": true, "python": true, "python3": true, "pytest": true,
	"cargo": true, "rustc": true, "rustup": true, "make": true, "cmake": true,
	"psql": true, "mysql": true, "sqlite3": true, "redis-cli": true, "mongosh": true,
	"echo": true, "export": true, "source": true, "env": true, "grep": true, "rg": true,
	"awk": true, "sed": true, "tar": true, "unzip": true, "zip": true, "ssh": true,
	"scp": true, "openssl": true, "nc": true, "netstat": true, "lsof": true, "dig": true,
	"nslookup": true, "ping": true, "traceroute": true, "nmap": true, "tcpdump": true,
	"hey": true, "ab": true, "wrk": true, " migrate": true,
}

var shellSubVerbs = map[string]bool{
	"run": true, "build": true, "test": true, "mod": true, "init": true, "tidy": true,
	"get": true, "install": true, "version": true, "vet": true, "fmt": true, "doc": true,
	"clean": true, "list": true, "work": true, "tool": true, "generate": true,
	"clone": true, "add": true, "commit": true, "push": true, "pull": true, "fetch": true,
	"checkout": true, "branch": true, "merge": true, "rebase": true, "status": true,
	"compose": true, "up": true, "down": true, "exec": true, "logs": true, "ps": true,
	"apply": true, "delete": true, "describe": true, "create": true, "update": true,
	"upgrade": true,
}

func inferLanguage(code string) string {
	s := strings.TrimSpace(code)
	if s == "" {
		return ""
	}
	if (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) && strings.Contains(s, `":`) && !strings.Contains(s, "func ") && !strings.Contains(s, "package ") {
		return "json"
	}
	if strings.HasPrefix(s, "<!DOCTYPE") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<?xml") || strings.HasPrefix(s, "<svg") {
		return "html"
	}
	if reInferGoMod.MatchString(s) {
		return "gomod"
	}
	if reInferSQL.MatchString(s) {
		return "sql"
	}
	if reInferGo.MatchString(s) {
		return "go"
	}
	if reInferRust.MatchString(s) {
		return "rust"
	}
	if reInferPython.MatchString(s) {
		return "python"
	}
	if reInferCPP.MatchString(s) {
		return "cpp"
	}
	if reInferJava.MatchString(s) {
		return "java"
	}
	if reInferTS.MatchString(s) {
		return "typescript"
	}
	if reInferDocker.MatchString(s) {
		return "dockerfile"
	}
	if reInferShell.MatchString(s) {
		return "sh"
	}
	if reInferYAML.MatchString(s) && (strings.Contains(s, "services:") || strings.Contains(s, "version:") || strings.Contains(s, "image:") || strings.Contains(s, "name:")) {
		return "yaml"
	}
	if strings.Contains(s, "//") {
		return "go"
	}
	if l := lexers.Analyse(s); l != nil {
		return l.Config().Name
	}
	return ""
}

func isShellLang(lang string) bool {
	switch strings.ToLower(lang) {
	case "sh", "bash", "shell", "zsh", "console", "terminal":
		return true
	}
	return false
}

func highlightShellBlock(codeText string) string {
	lines := strings.Split(codeText, "\n")
	hasDollarPrompt := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "$ ") {
			hasDollarPrompt = true
			break
		}
	}

	var out []string
	inContinuation := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			inContinuation = false
			continue
		}

		// Full-line comment
		if strings.HasPrefix(trimmed, "#") {
			indent := line[:strings.Index(line, "#")]
			out = append(out, VerbEscape(indent)+fmt.Sprintf(`\textit{\textcolor{cmcol}{%s}}`, VerbEscape(trimmed)))
			continue
		}

		// HTTP status / header response lines inside curl transcripts
		if strings.HasPrefix(trimmed, "HTTP/1.") || strings.HasPrefix(trimmed, "HTTP/2") {
			out = append(out, fmt.Sprintf(`\textbf{\textcolor{typecol}{%s}}`, VerbEscape(line)))
			continue
		}

		// If the block uses `$ ` prompts and this line is neither `$ ` nor a `\` continuation, it's command output
		if hasDollarPrompt && !strings.HasPrefix(trimmed, "$ ") && !strings.HasPrefix(trimmed, "> ") && !inContinuation {
			// If the output is JSON-like (`"key":`), highlight keys and values lightly
			if strings.HasPrefix(trimmed, `"`) && strings.Contains(trimmed, `":`) {
				colonIdx := strings.Index(line, `":`) + 2
				kPart := line[:colonIdx]
				vPart := line[colonIdx:]
				out = append(out, fmt.Sprintf(`\textcolor{typecol}{%s}%s`, VerbEscape(kPart), highlightShellArgs(vPart)))
			} else {
				out = append(out, fmt.Sprintf(`\textcolor{cmcol}{%s}`, VerbEscape(line)))
			}
			continue
		}

		inContinuation = strings.HasSuffix(trimmed, `\`)
		out = append(out, highlightShellCommandLine(line))
	}

	return "\\begin{Verbatim}[commandchars=\\\\\\{\\}]\n" + strings.Join(out, "\n") + "\n\\end{Verbatim}\n"
}

func highlightShellCommandLine(line string) string {
	// Split trailing `# comment` if present
	commentPart := ""
	codePart := line
	if idx := strings.Index(line, " # "); idx >= 0 {
		codePart = line[:idx]
		commentPart = " " + fmt.Sprintf(`\textit{\textcolor{cmcol}{%s}}`, VerbEscape(line[idx+1:]))
	}

	var b strings.Builder
	rest := codePart

	// Preserve leading whitespace
	leadWS := 0
	for leadWS < len(rest) && (rest[leadWS] == ' ' || rest[leadWS] == '\t') {
		leadWS++
	}
	if leadWS > 0 {
		b.WriteString(VerbEscape(rest[:leadWS]))
		rest = rest[leadWS:]
	}

	// Optional `$ ` prompt
	if strings.HasPrefix(rest, "$ ") {
		b.WriteString(`\textbf{\textcolor{cmcol}{\$}} `)
		rest = rest[2:]
	}

	// Extract first word (command verb)
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return b.String() + commentPart
	}
	firstWord := fields[0]
	if shellCmdVerbs[firstWord] || strings.HasPrefix(firstWord, "./") {
		idx := strings.Index(rest, firstWord)
		b.WriteString(VerbEscape(rest[:idx]))
		b.WriteString(fmt.Sprintf(`\textbf{\textcolor{kwcol}{%s}}`, VerbEscape(firstWord)))
		rest = rest[idx+len(firstWord):]

		// Check if second word is a known subcommand (e.g. `go run`, `git clone`, `docker compose`)
		if len(fields) >= 2 && shellSubVerbs[fields[1]] {
			subWord := fields[1]
			sIdx := strings.Index(rest, subWord)
			if sIdx >= 0 {
				b.WriteString(VerbEscape(rest[:sIdx]))
				b.WriteString(fmt.Sprintf(`\textcolor{fncol}{%s}`, VerbEscape(subWord)))
				rest = rest[sIdx+len(subWord):]
			}
		}
	}

	b.WriteString(highlightShellArgs(rest))
	b.WriteString(commentPart)
	return b.String()
}

func highlightShellArgs(s string) string {
	matches := reShellToken.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return VerbEscape(s)
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		if m[0] > last {
			b.WriteString(VerbEscape(s[last:m[0]]))
		}
		tok := s[m[0]:m[1]]
		switch {
		case m[2] >= 0 || m[4] >= 0 || m[6] >= 0: // string or URL
			b.WriteString(fmt.Sprintf(`\textcolor{strcol}{%s}`, VerbEscape(tok)))
		case m[8] >= 0: // flag
			b.WriteString(fmt.Sprintf(`\textcolor{typecol}{%s}`, VerbEscape(tok)))
		case m[10] >= 0: // number
			b.WriteString(fmt.Sprintf(`\textcolor{numcol}{%s}`, VerbEscape(tok)))
		default:
			b.WriteString(VerbEscape(tok))
		}
		last = m[1]
	}
	if last < len(s) {
		b.WriteString(VerbEscape(s[last:]))
	}
	return b.String()
}

// HighlightBlock uses Chroma's universal 250+ language lexer registry (plus automatic
// language inference for unlabeled blocks and rich CLI/shell highlighting) to syntax-highlight
// any programming, markup, query, shell, or configuration snippet into LaTeX Verbatim.
func HighlightBlock(lang string, codeText string) string {
	lang = strings.TrimSpace(lang)
	expanded := strings.ReplaceAll(codeText, "\t", "    ")
	trimmed := strings.TrimRight(expanded, "\n")

	if lang == "" || strings.EqualFold(lang, "text") || strings.EqualFold(lang, "plaintext") {
		inferred := inferLanguage(trimmed)
		if inferred != "" {
			lang = inferred
		}
	}

	if isShellLang(lang) {
		return highlightShellBlock(trimmed)
	}

	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Analyse(trimmed)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iter, err := lexer.Tokenise(nil, trimmed)
	if err != nil {
		return "\\begin{Verbatim}[commandchars=\\\\\\{\\}]\n" + VerbEscape(trimmed) + "\n\\end{Verbatim}\n"
	}

	toks := iter.Tokens()
	var out strings.Builder
	out.WriteString("\\begin{Verbatim}[commandchars=\\\\\\{\\}]\n")
	for i, tok := range toks {
		// Look ahead for the next non-whitespace token value to detect function calls `ident(`
		nextNonWS := ""
		for j := i + 1; j < len(toks); j++ {
			tVal := strings.TrimSpace(toks[j].Value)
			if tVal != "" {
				nextNonWS = tVal
				break
			}
		}

		parts := strings.Split(tok.Value, "\n")
		for idx, p := range parts {
			if p != "" {
				out.WriteString(styleToken(tok.Type, p, nextNonWS))
			}
			if idx < len(parts)-1 {
				out.WriteByte('\n')
			}
		}
	}
	out.WriteString("\n\\end{Verbatim}\n")
	return out.String()
}
