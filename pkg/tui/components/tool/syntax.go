package tool

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	tabWidth     = 4
	lineNumWidth = 5
	minWidth     = 80
)

type chromaToken struct {
	Text  string
	Style lipgloss.Style
}

type linePair struct {
	old        *udiff.Line
	new        *udiff.Line
	oldLineNum int
	newLineNum int
}

// syntaxHighlight applies syntax highlighting to code and returns styled text
func syntaxHighlight(code, filePath string) []chromaToken {
	lexer := lexers.Match(filePath)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.ChromaStyle()
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return []chromaToken{{Text: code, Style: lipgloss.NewStyle()}}
	}

	var tokens []chromaToken
	for _, token := range iterator.Tokens() {
		if token.Value == "" {
			continue
		}
		tokens = append(tokens, chromaToken{
			Text:  token.Value,
			Style: chromaToLipgloss(token.Type, style),
		})
	}

	return tokens
}

func chromaToLipgloss(tokenType chroma.TokenType, style *chroma.Style) lipgloss.Style {
	entry := style.Get(tokenType)
	lipStyle := lipgloss.NewStyle()

	if entry.Colour.IsSet() {
		lipStyle = lipStyle.Foreground(lipgloss.Color(entry.Colour.String()))
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

// renderDiffWithSyntaxHighlight renders a unified diff view
func renderDiffWithSyntaxHighlight(diff []*udiff.Hunk, filePath string, width int) string {
	var output strings.Builder
	contentWidth := width - lineNumWidth

	for _, hunk := range diff {
		oldLineNum := hunk.FromLine
		newLineNum := hunk.ToLine

		for _, line := range hunk.Lines {
			lineNum := getDisplayLineNumber(&line, &oldLineNum, &newLineNum)
			content := prepareContent(line.Content, contentWidth)

			lineNumStr := styles.LineNumberStyle.Render(fmt.Sprintf("%4d ", lineNum))
			styledLine := renderLine(content, line.Kind, filePath, contentWidth)

			output.WriteString(lineNumStr + styledLine + "\n")
		}
	}

	return strings.TrimSuffix(output.String(), "\n")
}

// renderSplitDiffWithSyntaxHighlight renders a split diff view with old/new side-by-side
func renderSplitDiffWithSyntaxHighlight(diff []*udiff.Hunk, filePath string, width int) string {
	// Fall back to unified diff if terminal is too narrow
	separator := styles.SeparatorStyle.Render(" │ ")
	separatorWidth := ansi.StringWidth(separator)
	contentWidth := (width - separatorWidth - (lineNumWidth * 2)) / 2

	if width < minWidth || contentWidth < 10 {
		return renderDiffWithSyntaxHighlight(diff, filePath, width)
	}

	var output strings.Builder

	for _, hunk := range diff {
		for _, pair := range pairDiffLines(hunk.Lines, hunk.FromLine, hunk.ToLine) {
			leftSide := renderSplitSide(pair.old, pair.oldLineNum, filePath, contentWidth)
			rightSide := renderSplitSide(pair.new, pair.newLineNum, filePath, contentWidth)

			line := leftSide + separator + rightSide
			line = ensureWidth(line, width)

			output.WriteString(line + "\n")
		}
	}

	return strings.TrimSuffix(output.String(), "\n")
}

// getDisplayLineNumber returns the appropriate line number and updates counters
func getDisplayLineNumber(line *udiff.Line, oldLineNum, newLineNum *int) int {
	switch line.Kind {
	case udiff.Delete:
		num := *oldLineNum
		*oldLineNum++
		return num
	case udiff.Insert:
		num := *newLineNum
		*newLineNum++
		return num
	case udiff.Equal:
		num := *oldLineNum
		*oldLineNum++
		*newLineNum++
		return num
	}
	return 0
}

// prepareContent normalizes content for display
func prepareContent(content string, maxWidth int) string {
	content = strings.ReplaceAll(content, "\t", strings.Repeat(" ", tabWidth))
	content = strings.TrimRight(content, "\n")
	if runewidth.StringWidth(content) > maxWidth {
		content = runewidth.Truncate(content, maxWidth-3, "...")
	}
	return content
}

// renderLine renders a line with syntax highlighting and appropriate styling
func renderLine(content string, kind udiff.OpKind, filePath string, width int) string {
	tokens := syntaxHighlight(content, filePath)
	lineStyle := getLineStyle(kind)

	rendered := renderTokensWithStyle(tokens, lineStyle)

	return padToWidth(rendered, width, lineStyle)
}

// renderSplitSide renders one side of a split diff
func renderSplitSide(line *udiff.Line, lineNum int, filePath string, width int) string {
	lineNumStr := formatLineNum(line, lineNum)

	if line == nil {
		emptySpace := styles.DiffUnchangedStyle.Render(strings.Repeat(" ", width))
		return styles.LineNumberStyle.Render(lineNumStr) + emptySpace
	}

	content := prepareContent(line.Content, width)
	styledContent := renderLine(content, line.Kind, filePath, width)

	return styles.LineNumberStyle.Render(lineNumStr) + styledContent
}

// renderTokensWithStyle applies consistent styling to tokens
func renderTokensWithStyle(tokens []chromaToken, lineStyle lipgloss.Style) string {
	var output strings.Builder

	for _, token := range tokens {
		styledToken := token.Style.Background(lineStyle.GetBackground())
		output.WriteString(styledToken.Render(token.Text))
	}

	return output.String()
}

// padToWidth adds padding to reach the desired width
func padToWidth(content string, width int, style lipgloss.Style) string {
	currentWidth := ansi.StringWidth(content)
	if paddingNeeded := width - currentWidth; paddingNeeded > 0 {
		padding := strings.Repeat(" ", paddingNeeded)
		return content + style.Render(padding)
	}
	return content
}

// ensureWidth ensures a line has consistent width
func ensureWidth(line string, width int) string {
	if lineWidth := ansi.StringWidth(line); lineWidth < width {
		padding := styles.DiffUnchangedStyle.Render(strings.Repeat(" ", width-lineWidth))
		return line + padding
	}
	return line
}

// getLineStyle returns the style for a diff line type
func getLineStyle(kind udiff.OpKind) lipgloss.Style {
	switch kind {
	case udiff.Delete:
		return styles.DiffRemoveStyle
	case udiff.Insert:
		return styles.DiffAddStyle
	default:
		return styles.DiffUnchangedStyle
	}
}

// formatLineNum formats a line number or returns empty space
func formatLineNum(line *udiff.Line, lineNum int) string {
	if line == nil {
		return strings.Repeat(" ", lineNumWidth)
	}
	return fmt.Sprintf("%4d ", lineNum)
}

// pairDiffLines pairs old and new lines for split view rendering
func pairDiffLines(lines []udiff.Line, fromLine, toLine int) []linePair {
	var pairs []linePair
	oldLineNum, newLineNum := fromLine, toLine

	for i := 0; i < len(lines); i++ {
		line := &lines[i]

		switch line.Kind {
		case udiff.Equal:
			pairs = append(pairs, linePair{
				old:        line,
				new:        line,
				oldLineNum: oldLineNum,
				newLineNum: newLineNum,
			})
			oldLineNum++
			newLineNum++

		case udiff.Delete:
			// Check if next line is an insert to pair them
			if i+1 < len(lines) && lines[i+1].Kind == udiff.Insert {
				pairs = append(pairs, linePair{
					old:        line,
					new:        &lines[i+1],
					oldLineNum: oldLineNum,
					newLineNum: newLineNum,
				})
				oldLineNum++
				newLineNum++
				i++ // Skip the paired insert
			} else {
				// Unpaired delete
				pairs = append(pairs, linePair{
					old:        line,
					new:        nil,
					oldLineNum: oldLineNum,
				})
				oldLineNum++
			}

		case udiff.Insert:
			// Unpaired insert (paired inserts are handled above)
			pairs = append(pairs, linePair{
				old:        nil,
				new:        line,
				newLineNum: newLineNum,
			})
			newLineNum++
		}
	}

	return pairs
}
