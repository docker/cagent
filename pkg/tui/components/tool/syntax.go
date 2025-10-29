package tool

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/tui/styles"
)

// syntaxHighlight applies syntax highlighting to code and returns styled text
func syntaxHighlight(code, filePath string) []chromaToken {
	lexer := lexers.Match(filePath)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := chromastyles.Get("monokai")
	if style == nil {
		style = chromastyles.Fallback
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return []chromaToken{{Text: code, Style: lipgloss.NewStyle()}}
	}

	var tokens []chromaToken
	for _, token := range iterator.Tokens() {
		if token.Value == "" {
			continue
		}

		lipStyle := chromaToLipgloss(token.Type, style)
		tokens = append(tokens, chromaToken{
			Text:  token.Value,
			Style: lipStyle,
		})
	}

	return tokens
}

type chromaToken struct {
	Text  string
	Style lipgloss.Style
}

func chromaToLipgloss(tokenType chroma.TokenType, style *chroma.Style) lipgloss.Style {
	entry := style.Get(tokenType)
	lipStyle := lipgloss.NewStyle()

	if entry.Colour.IsSet() {
		color := entry.Colour.String()
		lipStyle = lipStyle.Foreground(lipgloss.Color(color))
	}

	if entry.Background.IsSet() {
		color := entry.Background.String()
		lipStyle = lipStyle.Background(lipgloss.Color(color))
	}

	if entry.Bold == chroma.Yes {
		lipStyle = lipStyle.Bold(true)
	}

	if entry.Italic == chroma.Yes {
		lipStyle = lipStyle.Italic(true)
	}

	if entry.Underline == chroma.Yes {
		lipStyle = lipStyle.Underline(true)
	}

	return lipStyle
}

func renderDiffWithSyntaxHighlight(diff []*udiff.Hunk, filePath string, width int) string {
	fullWidth := min(120, width)
	const tabWidth = 4
	var output strings.Builder

	for _, hunk := range diff {
		for _, line := range hunk.Lines {
			var lineStyle lipgloss.Style

			switch line.Kind {
			case udiff.Delete:
				lineStyle = styles.DiffRemoveStyle
			case udiff.Insert:
				lineStyle = styles.DiffAddStyle
			case udiff.Equal:
				lineStyle = styles.DiffUnchangedStyle
			}

			expandedContent := strings.ReplaceAll(line.Content, "\t", strings.Repeat(" ", tabWidth))
			expandedContent = strings.TrimRight(expandedContent, "\n")

			contentWidth := runewidth.StringWidth(expandedContent)
			if contentWidth > fullWidth {
				expandedContent = runewidth.Truncate(expandedContent, fullWidth-3, "...")
			}

			tokens := syntaxHighlight(expandedContent, filePath)

			var lineBuilder strings.Builder

			contentLength := 0
			for i := range tokens {
				contentLength += runewidth.StringWidth(tokens[i].Text)
			}

			paddingNeeded := 0
			if contentLength < fullWidth {
				paddingNeeded = fullWidth - contentLength
			}

			if line.Kind != udiff.Equal {
				var contentBuilder strings.Builder
				for i := range tokens {
					token := &tokens[i]
					styledToken := token.Style.Background(lineStyle.GetBackground())
					contentBuilder.WriteString(styledToken.Render(token.Text))
				}

				if paddingNeeded > 0 {
					contentBuilder.WriteString(lineStyle.Render(strings.Repeat(" ", paddingNeeded)))
				}

				lineBuilder.WriteString(contentBuilder.String())
			} else {
				for i := range tokens {
					token := &tokens[i]
					lineBuilder.WriteString(token.Style.Render(token.Text))
				}
				if paddingNeeded > 0 {
					lineBuilder.WriteString(strings.Repeat(" ", paddingNeeded))
				}
			}

			output.WriteString(lineBuilder.String())
			output.WriteString("\n")
		}
	}

	return strings.TrimSuffix(output.String(), "\n")
}
