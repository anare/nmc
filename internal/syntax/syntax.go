package syntax

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const HighlightLimit = 2 << 20

type Span struct {
	Text       string
	Foreground chroma.Colour
	Bold       bool
}

func Highlight(filename, text, styleName string) ([][]Span, string, bool) {
	if len(text) > HighlightLimit {
		return nil, "", false
	}
	lexer := lexers.Match(filename)
	if lexer == nil {
		return nil, "", false
	}
	lexer = chroma.Coalesce(lexer)
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil, "", false
	}
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	tokens := iterator.Tokens()
	var lines [][]Span
	var current []Span
	for _, token := range tokens {
		entry := style.Get(token.Type)
		if strings.Contains(token.Value, "\n") {
			parts := strings.Split(token.Value, "\n")
			for index, part := range parts {
				if part != "" {
					current = append(current, Span{Text: part, Foreground: entry.Colour, Bold: entry.Bold == chroma.Yes})
				}
				if index < len(parts)-1 {
					lines = append(lines, current)
					current = nil
				}
			}
		} else if token.Value != "" {
			current = append(current, Span{Text: token.Value, Foreground: entry.Colour, Bold: entry.Bold == chroma.Yes})
		}
	}
	if len(current) > 0 || len(lines) == 0 {
		lines = append(lines, current)
	}
	return lines, lexer.Config().Name, true
}
