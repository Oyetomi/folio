package highlight

import (
	"fmt"
	"strings"

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

func styleToken(tt chroma.TokenType, val string) string {
	if val == "" {
		return ""
	}
	esc := VerbEscape(val)
	switch {
	case tt == chroma.Comment || tt.InCategory(chroma.Comment):
		return fmt.Sprintf(`\textit{\textcolor{cmcol}{%s}}`, esc)
	case tt == chroma.Keyword || tt.InCategory(chroma.Keyword):
		if tt == chroma.KeywordType {
			return fmt.Sprintf(`\textcolor{typecol}{%s}`, esc)
		}
		return fmt.Sprintf(`\textbf{\textcolor{kwcol}{%s}}`, esc)
	case tt == chroma.NameBuiltin || tt == chroma.NameFunction || tt == chroma.NameClass || tt == chroma.NameNamespace:
		return fmt.Sprintf(`\textcolor{fncol}{%s}`, esc)
	case tt.InCategory(chroma.LiteralString):
		return fmt.Sprintf(`\textcolor{strcol}{%s}`, esc)
	case tt.InCategory(chroma.LiteralNumber):
		return fmt.Sprintf(`\textcolor{numcol}{%s}`, esc)
	default:
		return esc
	}
}

// HighlightBlock uses Chroma's universal 250+ language lexer registry to syntax-highlight
// any programming, markup, query, or configuration language into LaTeX Verbatim.
func HighlightBlock(lang string, codeText string) string {
	lang = strings.TrimSpace(lang)
	expanded := strings.ReplaceAll(codeText, "\t", "    ")
	trimmed := strings.TrimRight(expanded, "\n")

	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iter, err := lexer.Tokenise(nil, trimmed)
	if err != nil {
		return "\\begin{Verbatim}[commandchars=\\\\\\{\\}]\n" + VerbEscape(trimmed) + "\n\\end{Verbatim}\n"
	}

	var out strings.Builder
	out.WriteString("\\begin{Verbatim}[commandchars=\\\\\\{\\}]\n")
	for _, tok := range iter.Tokens() {
		// Split multi-line tokens (e.g. multi-line comments or strings) line by line so Verbatim never chokes
		parts := strings.Split(tok.Value, "\n")
		for idx, p := range parts {
			if p != "" {
				out.WriteString(styleToken(tok.Type, p))
			}
			if idx < len(parts)-1 {
				out.WriteByte('\n')
			}
		}
	}
	out.WriteString("\n\\end{Verbatim}\n")
	return out.String()
}
