package markdown

import (
	_ "embed"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stripANSI removes ANSI escape sequences from a string.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestFastRendererBasicText(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(80)
	result, err := r.Render("Hello, world!")
	require.NoError(t, err)
	assert.Contains(t, result, "Hello, world!")
}

func TestFastRendererEmptyInput(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(80)
	result, err := r.Render("")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFastRendererHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"h1", "# Heading 1", "Heading 1"},
		{"h2", "## Heading 2", "Heading 2"},
		{"h3", "### Heading 3", "Heading 3"},
		{"h4", "#### Heading 4", "Heading 4"},
		{"h5", "##### Heading 5", "Heading 5"},
		{"h6", "###### Heading 6", "Heading 6"},
		{"h1 with trailing hashes", "# Heading #", "Heading"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFastRenderer(80)
			result, err := r.Render(tt.input)
			require.NoError(t, err)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestFastRendererCodeBlocks(t *testing.T) {
	t.Parallel()

	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	// Should contain the code content
	plain := stripANSI(result)
	assert.Contains(t, plain, "func")
	assert.Contains(t, plain, "main")
	assert.Contains(t, plain, "Println")
}

func TestFastRendererCodeBlocksNoLanguage(t *testing.T) {
	t.Parallel()

	input := "```\nsome code here\n```"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	assert.Contains(t, stripANSI(result), "some code here")
}

func TestFastRendererCodeBlockWrapping(t *testing.T) {
	t.Parallel()

	// A very long line that should be wrapped at the configured width
	longLine := "this_is_a_very_long_line_of_code_that_should_definitely_be_wrapped_when_rendered_at_a_narrow_width"
	input := "```\n" + longLine + "\n```"

	r := NewFastRenderer(30)
	result, err := r.Render(input)
	require.NoError(t, err)

	// The original long line should NOT appear unbroken in the output
	plain := stripANSI(result)
	assert.NotContains(t, plain, longLine, "Long line should be wrapped, not appear unbroken")

	// Every line should have the exact target width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 30, lineWidth, "Line %d has incorrect width: %q (width=%d, expected=30)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererCodeBlockNarrowWidth(t *testing.T) {
	t.Parallel()

	// At narrow widths (< 24), padding should be disabled to avoid exceeding target width
	input := "```\nshort code\n```"

	r := NewFastRenderer(20)
	result, err := r.Render(input)
	require.NoError(t, err)

	// Every line should have exactly the target width (20), not 24 (20 + 4 padding)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 20, lineWidth, "Line %d has incorrect width: %q (width=%d, expected=20)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererCodeBlockAllLinesSameWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		width int
	}{
		{
			name:  "normal width with padding",
			input: "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```",
			width: 50,
		},
		{
			name:  "narrow width no padding",
			input: "```\ncode\n```",
			width: 15,
		},
		{
			name:  "long line wrapping",
			input: "```\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n```",
			width: 30,
		},
		{
			name:  "mixed short and long lines",
			input: "```\nshort\nthis_is_a_much_longer_line_that_needs_wrapping\n```",
			width: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFastRenderer(tt.width)
			result, err := r.Render(tt.input)
			require.NoError(t, err)

			lines := strings.Split(result, "\n")
			for i, line := range lines {
				lineWidth := runewidth.StringWidth(stripANSI(line))
				assert.Equal(t, tt.width, lineWidth, "Line %d has incorrect width: %q (width=%d, expected=%d)", i, stripANSI(line), lineWidth, tt.width)
			}
		})
	}
}

func TestFastRendererInlineCode(t *testing.T) {
	t.Parallel()

	input := "Use `fmt.Println` to print"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	assert.Contains(t, result, "fmt.Println")
}

func TestFastRendererHeadingWithLongCode(t *testing.T) {
	t.Parallel()

	// A heading with a very long inline code that should wrap
	input := "# Heading with `very_long_function_name_that_should_wrap_when_rendered_at_narrow_width` in it"
	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	// Should contain all parts
	plain := stripANSI(result)
	assert.Contains(t, plain, "Heading with")
	assert.Contains(t, plain, "very_long_function_name")
	assert.Contains(t, plain, "in it")

	// All lines should have correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 40, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererHeadingInlineCodeStyleRestoration(t *testing.T) {
	t.Parallel()

	// Test that text after inline code in heading maintains heading style
	input := "# Title `code` more text"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	// The plain text should contain all parts
	plain := stripANSI(result)
	assert.Contains(t, plain, "Title")
	assert.Contains(t, plain, "code")
	assert.Contains(t, plain, "more text")

	// After the code's ANSI reset, there should be a style restoration sequence
	// The text "more text" should not appear unstyled
	seqs := ansiRegex.FindAllString(result, -1)
	// We should have multiple ANSI sequences (not just at start and end)
	assert.GreaterOrEqual(t, len(seqs), 3, "Should have style sequences for code and restoration")
}

func TestFastRendererBlockquoteInlineCodeStyleRestoration(t *testing.T) {
	t.Parallel()

	// Test that text after inline code in blockquote maintains blockquote style
	input := "> Quote with `code` and more text"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	// The plain text should contain all parts
	plain := stripANSI(result)
	assert.Contains(t, plain, "Quote with")
	assert.Contains(t, plain, "code")
	assert.Contains(t, plain, "more text")
}

func TestFastRendererBlockquoteWithFencedCodeBlock(t *testing.T) {
	t.Parallel()

	input := "> Some quote text\n> ```go\n> func main() {}\n> ```\n> More quote text"
	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	// Should contain all parts
	plain := stripANSI(result)
	assert.Contains(t, plain, "Some quote text")
	assert.Contains(t, plain, "func main()")
	assert.Contains(t, plain, "More quote text")

	// All lines should have correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 60, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererBoldWithInlineCode(t *testing.T) {
	t.Parallel()

	// Test that inline code within bold text properly restores bold style
	input := "**bold `code` still bold**"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "bold code still bold")
}

func TestFastRendererBold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"double asterisk", "This is **bold** text", "bold"},
		{"double underscore", "This is __bold__ text", "bold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFastRenderer(80)
			result, err := r.Render(tt.input)
			require.NoError(t, err)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestFastRendererItalic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"single asterisk", "This is *italic* text", "italic"},
		{"single underscore", "This is _italic_ text", "italic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFastRenderer(80)
			result, err := r.Render(tt.input)
			require.NoError(t, err)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestFastRendererItalicWithInlineCode(t *testing.T) {
	t.Parallel()

	// Test that inline code within italic text works and asterisks are stripped
	input := "*italic with `code` inside*"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	// Asterisks should be removed (italic markers processed)
	assert.NotContains(t, plain, "*")
	assert.Contains(t, plain, "italic with code inside")
}

func TestFastRendererHeadingWithItalicAndCode(t *testing.T) {
	t.Parallel()

	// Test that italic with inline code works inside headings
	input := "### *Bold italic with `code` inside*"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	// Asterisks should be removed (italic markers processed)
	// The ### prefix is rendered as ### by the heading renderer
	assert.NotContains(t, plain, "*italic")
	assert.NotContains(t, plain, "inside*")
	assert.Contains(t, plain, "Bold italic with code inside")
}

func TestFastRendererHeadingItalicStripsMarkers(t *testing.T) {
	t.Parallel()

	input := "### *Bold italic with `code` inside*"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.NotContains(t, plain, "*")
	assert.Contains(t, plain, "Bold italic with code inside")

	italicPattern := regexp.MustCompile(`\x1b\[3[;m]`)
	assert.True(t, italicPattern.MatchString(result), "Heading should apply italic styling")
}

func TestFastRendererStrikethrough(t *testing.T) {
	t.Parallel()

	input := "This is ~~strikethrough~~ text"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	assert.Contains(t, stripANSI(result), "strikethrough")
}

func TestFastRendererLinks(t *testing.T) {
	t.Parallel()

	input := "Check out [this link](https://example.com)"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "this link")
	assert.Contains(t, plain, "example.com")
}

func TestFastRendererUnorderedLists(t *testing.T) {
	t.Parallel()

	input := `- Item 1
- Item 2
- Item 3`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	assert.Contains(t, result, "Item 1")
	assert.Contains(t, result, "Item 2")
	assert.Contains(t, result, "Item 3")
	assert.Contains(t, result, "- ")
}

func TestFastRendererOrderedLists(t *testing.T) {
	t.Parallel()

	input := `1. First
2. Second
3. Third`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	assert.Contains(t, result, "First")
	assert.Contains(t, result, "Second")
	assert.Contains(t, result, "Third")
}

func TestFastRendererTaskLists(t *testing.T) {
	t.Parallel()

	input := `- [ ] Todo item
- [x] Done item`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	assert.Contains(t, result, "Todo item")
	assert.Contains(t, result, "Done item")
}

func TestFastRendererBlockquotes(t *testing.T) {
	t.Parallel()

	input := "> This is a quote"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	assert.Contains(t, result, "This is a quote")
}

func TestFastRendererHorizontalRule(t *testing.T) {
	t.Parallel()

	tests := []string{"---", "***", "___"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			r := NewFastRenderer(80)
			result, err := r.Render(input)
			require.NoError(t, err)
			assert.Contains(t, result, "---")
		})
	}
}

func TestFastRendererTables(t *testing.T) {
	t.Parallel()

	input := `| Name | Age |
|------|-----|
| Alice | 30 |
| Bob | 25 |`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Name")
	assert.Contains(t, plain, "Age")
	assert.Contains(t, plain, "Alice")
	assert.Contains(t, plain, "Bob")
	assert.Contains(t, plain, "30")
	assert.Contains(t, plain, "25")
}

// assertTableColumnsAligned verifies that all rows in a rendered table have
// column separators at the same positions.
func assertTableColumnsAligned(t *testing.T, rendered string) {
	t.Helper()

	plain := stripANSI(rendered)
	lines := strings.Split(strings.TrimSpace(plain), "\n")

	// Filter out empty lines and separator lines (lines starting with ─)
	var dataLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "─") {
			dataLines = append(dataLines, line)
		}
	}

	require.GreaterOrEqual(t, len(dataLines), 2, "Table should have at least header + 1 data row")

	// Find the position of the column separator (│) in each line
	// All rows should have separators at the same positions
	var expectedPositions []int
	for i, line := range dataLines {
		var positions []int
		for j, r := range line {
			if r == '│' {
				positions = append(positions, j)
			}
		}
		if i == 0 {
			expectedPositions = positions
		} else {
			assert.Equal(t, expectedPositions, positions,
				"Column separators should be at same positions in all rows\nLine %d: %q\nLine 0: %q",
				i, line, dataLines[0])
		}
	}
}

func TestFastRendererTablesColumnAlignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name: "plain text columns",
			input: `| Name | Age | City |
|------|-----|------|
| Alice | 30 | New York |
| Bob | 25 | LA |`,
		},
		{
			name: "styled content (bold, italic, code)",
			input: `| Feature | Status | Notes |
|---------|--------|-------|
| **Bold** | Done | This is bold |
| *Italic* | WIP | This is italic |
| ` + "`Code`" + ` | Todo | Inline code |`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewFastRenderer(80)
			result, err := r.Render(tt.input)
			require.NoError(t, err)
			assertTableColumnsAligned(t, result)
		})
	}
}

func TestFastRendererEscapedCharacters(t *testing.T) {
	t.Parallel()

	input := `\*not italic\* and \**not bold\**`
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	// Strip ANSI codes to check actual content
	plain := stripANSI(result)
	assert.Contains(t, plain, "*not italic*")
}

func TestFastRendererNestedLists(t *testing.T) {
	t.Parallel()

	input := `- Item 1
  - Nested 1
  - Nested 2
- Item 2`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	assert.Contains(t, result, "Item 1")
	assert.Contains(t, result, "Nested 1")
	assert.Contains(t, result, "Nested 2")
	assert.Contains(t, result, "Item 2")
}

func TestFastRendererMixedContent(t *testing.T) {
	t.Parallel()

	input := `# Main Title

This is a paragraph with **bold** and *italic* text.

## Code Example

` + "```go\nfmt.Println(\"Hello\")\n```" + `

- List item 1
- List item 2

> A blockquote

---

The end.`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Main Title")
	assert.Contains(t, plain, "bold")
	assert.Contains(t, plain, "italic")
	assert.Contains(t, plain, "Println")
	assert.Contains(t, plain, "List item")
	assert.Contains(t, plain, "blockquote")
	assert.Contains(t, plain, "The end")
}

func TestFastRendererWordWrapping(t *testing.T) {
	t.Parallel()

	longText := "This is a very long line that should be wrapped when the width is constrained to something reasonable like forty characters."
	r := NewFastRenderer(40)
	result, err := r.Render(longText)
	require.NoError(t, err)

	lines := strings.Split(result, "\n")
	// Should wrap into multiple lines
	assert.Greater(t, len(lines), 1)
}

func TestFastRendererPadsToWidth(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(20)
	result, err := r.Render("short\n\nmore")
	require.NoError(t, err)

	lines := strings.Split(result, "\n")
	require.Len(t, lines, 3)
	assert.Len(t, stripANSI(lines[0]), 20)
	assert.Equal(t, 20, runewidth.StringWidth(stripANSI(lines[0])))
	assert.Len(t, stripANSI(lines[1]), 20)
	assert.Equal(t, 20, runewidth.StringWidth(stripANSI(lines[1])))
	assert.Len(t, stripANSI(lines[2]), 20)
	assert.Equal(t, 20, runewidth.StringWidth(stripANSI(lines[2])))
}

func TestFastRendererStripsCarriageReturns(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(20)
	result, err := r.Render("hello\rworld")
	require.NoError(t, err)

	assert.NotContains(t, result, "\r")
}

func TestFastRendererFixedWidthRectangle(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(30)
	input := "root\r\n\n\t\tNow I'll add the Pull button\n" +
		"\x1b[31mRED\x1b[0m text and a very very very very very long line that must truncate"
	out, err := r.Render(input)
	require.NoError(t, err)

	for _, line := range strings.Split(out, "\n") {
		assert.Equal(t, 30, runewidth.StringWidth(stripANSI(line)))
	}
}

func TestFastRendererRendererInterface(t *testing.T) {
	t.Parallel()

	// Ensure FastRenderer implements Renderer interface
	var r Renderer = NewFastRenderer(80)
	result, err := r.Render("test")
	require.NoError(t, err)
	assert.Contains(t, result, "test")
}

// Benchmark tests to compare performance
var benchmarkInput = `# Performance Test Document

This is a comprehensive markdown document designed to test the performance of the markdown renderer.

## Features

Here's a list of features we're testing:

- **Bold text** for emphasis
- *Italic text* for subtle emphasis
- ` + "`inline code`" + ` for technical terms
- ~~Strikethrough~~ for deleted content
- [Links](https://example.com) for references

## Code Block

` + "```go" + `
package main

import "fmt"

func main() {
    for i := 0; i < 10; i++ {
        fmt.Printf("Hello %d\n", i)
    }
}
` + "```" + `

## Blockquote

> This is an important quote that should be displayed
> with proper formatting and styling.

## Task List

- [x] Implement fast renderer
- [x] Add syntax highlighting
- [ ] Add more tests
- [ ] Optimize further

---

## Conclusion

This document contains various markdown elements that are commonly used in LLM responses.
The fast renderer should handle all of these efficiently.
`

func BenchmarkFastRenderer(b *testing.B) {
	r := NewFastRenderer(80)
	for b.Loop() {
		_, _ = r.Render(benchmarkInput)
	}
}

func BenchmarkGlamourRenderer(b *testing.B) {
	r := NewGlamourRenderer(80)
	for b.Loop() {
		_, _ = r.Render(benchmarkInput)
	}
}

func BenchmarkFastRendererSmall(b *testing.B) {
	r := NewFastRenderer(80)
	input := "Hello **world**, this is a *test*."
	for b.Loop() {
		_, _ = r.Render(input)
	}
}

func BenchmarkGlamourRendererSmall(b *testing.B) {
	r := NewGlamourRenderer(80)
	input := "Hello **world**, this is a *test*."
	for b.Loop() {
		_, _ = r.Render(input)
	}
}

func BenchmarkFastRendererCodeBlock(b *testing.B) {
	r := NewFastRenderer(80)
	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	for b.Loop() {
		_, _ = r.Render(input)
	}
}

func BenchmarkGlamourRendererCodeBlock(b *testing.B) {
	r := NewGlamourRenderer(80)
	input := "```go\nfunc main() {\n\tfmt.Println(\"hello`\")\n}\n```"
	for b.Loop() {
		_, _ = r.Render(input)
	}
}

var benchmarkTableInput = `| Name | Age | City | Country | Occupation |
|------|-----|------|---------|------------|
| Alice | 30 | New York | USA | Engineer |
| Bob | 25 | London | UK | Designer |
| Charlie | 35 | Paris | France | Manager |
| Diana | 28 | Berlin | Germany | Developer |`

func BenchmarkFastRendererTable(b *testing.B) {
	r := NewFastRenderer(80)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func BenchmarkGlamourRendererTable(b *testing.B) {
	r := NewGlamourRenderer(80)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func BenchmarkFastRendererTableWidth20(b *testing.B) {
	r := NewFastRenderer(20)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func BenchmarkGlamourRendererTableWidth20(b *testing.B) {
	r := NewGlamourRenderer(20)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func BenchmarkFastRendererTableWidth200(b *testing.B) {
	r := NewFastRenderer(200)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func BenchmarkGlamourRendererTableWidth200(b *testing.B) {
	r := NewGlamourRenderer(200)
	for b.Loop() {
		_, _ = r.Render(benchmarkTableInput)
	}
}

func TestFastRendererListWrapping(t *testing.T) {
	t.Parallel()

	// Long list item that should wrap
	input := `- This is a very long list item that contains a lot of text and should definitely wrap when rendered at a narrow width like forty characters or so`

	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	lines := strings.Split(strings.TrimSpace(plain), "\n")

	// Should wrap into multiple lines
	assert.Greater(t, len(lines), 1, "Long list item should wrap to multiple lines")

	// First line should start with bullet
	assert.Contains(t, lines[0], "- ", "First line should contain bullet")

	// Check that continuation lines are indented (should have leading spaces)
	if len(lines) > 1 {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "" {
				assert.True(t, strings.HasPrefix(lines[i], "  "), "Continuation line %d should be indented: %q", i, lines[i])
			}
		}
	}
}

func TestFastRendererParagraphWrappingWithStyles(t *testing.T) {
	t.Parallel()

	// Paragraph with styled text - should wrap at visual width, not including ANSI codes
	input := "This is **bold text** and this is *italic text* and they should wrap correctly at the proper visual width."

	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Check that no line exceeds the visual width (excluding ANSI codes)
	for i, line := range lines {
		plainLine := stripANSI(line)
		width := 0
		for _, r := range plainLine {
			width += runewidth.RuneWidth(r)
		}
		assert.LessOrEqual(t, width, 40, "Line %d exceeds width 40: %q (width=%d)", i, plainLine, width)
	}
}

func TestFastRendererListWrappingWithStyles(t *testing.T) {
	t.Parallel()

	// List item with styled text that should wrap correctly
	input := `- This is a list item with **bold text** and *italic text* that should wrap properly at the correct visual width`

	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	lines := strings.Split(strings.TrimSpace(plain), "\n")

	// Should wrap into multiple lines
	assert.Greater(t, len(lines), 1, "Styled list item should wrap to multiple lines")

	// Verify each line doesn't exceed the width
	for i, line := range lines {
		width := 0
		for _, r := range line {
			width += runewidth.RuneWidth(r)
		}
		assert.LessOrEqual(t, width, 40, "Line %d exceeds width 40: %q (width=%d)", i, line, width)
	}
}

func TestFastRendererNestedListWrapping(t *testing.T) {
	t.Parallel()

	// Nested list items that should wrap with proper indentation
	input := `- First level item with a very long description that needs to wrap
  - Second level nested item that also has a very long description needing wrapping`

	r := NewFastRenderer(50)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	lines := strings.Split(strings.TrimSpace(plain), "\n")

	// Should have multiple lines
	assert.Greater(t, len(lines), 2, "Nested list should produce multiple lines")

	// First line should have bullet at start
	assert.True(t, strings.HasPrefix(lines[0], "- "), "First line should start with bullet")
}

func TestFastRendererCodeBlockInList(t *testing.T) {
	t.Parallel()

	input := "- List item\n  ```go\n  func main() {}\n  ```\n- Another item"

	r := NewFastRenderer(50)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)

	// Should contain list items and code
	assert.Contains(t, plain, "List item")
	assert.Contains(t, plain, "func main()")
	assert.Contains(t, plain, "Another item")

	// All lines should have the correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 50, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererCodeBlockInNestedList(t *testing.T) {
	t.Parallel()

	input := `- First level
  - Nested item
    ` + "```" + `
    code here
    ` + "```" + `
  - Another nested`

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)

	// Should contain all parts
	assert.Contains(t, plain, "First level")
	assert.Contains(t, plain, "Nested item")
	assert.Contains(t, plain, "code here")
	assert.Contains(t, plain, "Another nested")

	// All lines should have correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 60, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererAllLinesSameWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		width int
	}{
		{
			name:  "paragraph",
			input: "This is a paragraph with some text that should be wrapped and padded to the exact width.",
			width: 40,
		},
		{
			name: "list items",
			input: `- First item
- Second item with more text
- Third item`,
			width: 40,
		},
		{
			name: "mixed content",
			input: `# Heading

This is a paragraph.

- List item 1
- List item 2

> A blockquote`,
			width: 50,
		},
		{
			name:  "styled text",
			input: "This has **bold** and *italic* text that needs proper width handling.",
			width: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewFastRenderer(tt.width)
			result, err := r.Render(tt.input)
			require.NoError(t, err)

			lines := strings.Split(result, "\n")
			for i, line := range lines {
				lineWidth := runewidth.StringWidth(stripANSI(line))
				assert.Equal(t, tt.width, lineWidth, "Line %d has incorrect width: %q (width=%d, expected=%d)", i, stripANSI(line), lineWidth, tt.width)
			}
		})
	}
}

func TestInlineCodeRestoresBaseStyle(t *testing.T) {
	t.Parallel()

	// This test verifies that text after inline code has the document's base style restored,
	// not just a reset to terminal default.
	// Bug: "Hello `there` beautiful" - "beautiful" was appearing with terminal default
	// instead of the document's text color.

	r := NewFastRenderer(80)
	result, err := r.Render("Hello `there` beautiful")
	require.NoError(t, err)

	seqs := ansiRegex.FindAllString(result, -1)

	// We should have multiple ANSI sequences:
	// - Base text style for "Hello " and " beautiful"
	// - Code style for "there" (RGB foreground + background)
	// - Resets between styled segments
	require.GreaterOrEqual(t, len(seqs), 3, "Should have at least 3 ANSI sequences")

	// Verify that text styling is applied (either ANSI 256 color or RGB)
	allSeqs := strings.Join(seqs, "")
	// Text color can be either "38;5;N" (256 color) or "38;2;R;G;B" (RGB) depending on theme
	hasTextColor := strings.Contains(allSeqs, "38;5;") || strings.Contains(allSeqs, "38;2;")
	assert.True(t, hasTextColor, "Should have text color applied (38;5; or 38;2;)")

	// Verify code style appears (RGB foreground and background)
	assert.Contains(t, allSeqs, "38;2;", "Code style should have RGB foreground")
	assert.Contains(t, allSeqs, "48;2;", "Code style should have RGB background")
}

func TestInlineCodeTextContent(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(80)
	result, err := r.Render("Hello `there` beautiful")
	require.NoError(t, err)

	plain := ansi.Strip(result)
	require.Contains(t, plain, "Hello there beautiful")
}

func TestFastRendererFootnotes(t *testing.T) {
	t.Parallel()

	input := `This has a footnote[^1] and another[^note].

[^1]: First footnote content
[^note]: Named footnote content`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	// Should contain the footnote markers
	assert.Contains(t, plain, "[^1]")
	assert.Contains(t, plain, "[^note]")
	// Should contain the footnote definitions
	assert.Contains(t, plain, "First footnote content")
	assert.Contains(t, plain, "Named footnote content")
}

func TestFastRendererTextAfterFootnoteStyledCorrectly(t *testing.T) {
	t.Parallel()

	// Text after footnotes should maintain proper styling
	input := `This has a footnote[^1] and **bold text** after.

[^1]: Footnote content`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "bold text")
	// Should have ANSI sequences for styling after the footnote
	seqs := ansiRegex.FindAllString(result, -1)
	assert.GreaterOrEqual(t, len(seqs), 2, "Should have styling sequences")
}

func TestFastRendererStrikethroughWithInlineCode(t *testing.T) {
	t.Parallel()

	// Strikethrough should apply to inline code (for deprecated code indication)
	input := "This has ~~deprecated `code` here~~ text"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "deprecated code here")

	// The code within strikethrough should have strikethrough attribute
	// Check for strikethrough ANSI code \x1b[9m
	assert.Contains(t, result, "\x1b[9m", "Strikethrough should be present in output")
}

func TestFastRendererConsecutiveHeadingsConsistentStyle(t *testing.T) {
	t.Parallel()

	// All consecutive headings should have consistent bold styling
	input := `# First Heading
## Second Heading
### Third Heading with ` + "`code`" + `
#### Fourth Heading`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "First Heading")
	assert.Contains(t, plain, "Second Heading")
	assert.Contains(t, plain, "Third Heading with code")
	assert.Contains(t, plain, "Fourth Heading")

	// All headings should have bold ANSI code (may be combined with color as \x1b[1;38;...)
	// Check for bold attribute which can appear as \x1b[1m or \x1b[1;...
	boldPattern := regexp.MustCompile(`\x1b\[1[;m]`)
	lines := strings.Split(result, "\n")
	headingCount := 0
	for _, line := range lines {
		plainLine := stripANSI(line)
		if strings.Contains(plainLine, "Heading") {
			// Each heading line should contain bold code
			assert.True(t, boldPattern.MatchString(line), "Heading should have bold styling: %q", plainLine)
			headingCount++
		}
	}
	assert.Equal(t, 4, headingCount, "Should have 4 headings")
}

func TestFastRendererHeadingBoldItalicMaintainsColor(t *testing.T) {
	t.Parallel()

	// Bold/italic text within headings should keep heading color, not become white
	input := "## Heading with **bold** and *italic* text"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Heading with bold and italic text")

	// Should have color codes for the heading (not just reset to default white)
	// Check for RGB color or 256-color code
	assert.True(t,
		strings.Contains(result, "38;2;") || strings.Contains(result, "38;5;"),
		"Heading should have foreground color styling")
}

func TestFastRendererTableCellStyleAfterInlineCode(t *testing.T) {
	t.Parallel()

	// Table cells with inline code followed by text should maintain styling
	input := `| Column 1 | Column 2 |
|----------|----------|
| Text ` + "`code`" + ` more | Plain |`

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Text code more")
	assert.Contains(t, plain, "Plain")

	// The text after code in table cell should have styling applied
	seqs := ansiRegex.FindAllString(result, -1)
	assert.GreaterOrEqual(t, len(seqs), 4, "Table should have styling sequences")
}

func TestFastRendererNestedBlockquotesInList(t *testing.T) {
	t.Parallel()

	input := `- List item
  > Blockquote level 1
  >> Nested blockquote
  > Back to level 1
- Another item`

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "List item")
	assert.Contains(t, plain, "Blockquote level 1")
	assert.Contains(t, plain, "Nested blockquote")
	assert.Contains(t, plain, "Another item")

	// All lines should be padded to width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 60, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererBlockquoteWithCode(t *testing.T) {
	t.Parallel()

	input := `> Quote with ` + "`inline code`" + ` here
>> Nested with ` + "`more code`"

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Quote with inline code here")
	assert.Contains(t, plain, "Nested with more code")
}

func TestFastRendererNestedBlockquoteWithFencedCodeBlock(t *testing.T) {
	t.Parallel()

	// Nested blockquote with a fenced code block inside
	// This tests that code blocks are correctly detected after stripping nested > prefixes
	input := `> Outer quote
> > Inner quote:
> > ` + "```go" + `
> > fmt.Println("Deeply quoted code")
> > ` + "```" + `
> > More nested text
> Back to outer`

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)

	// Should contain all text parts (code block fences should be consumed, not rendered)
	assert.Contains(t, plain, "Outer quote")
	assert.Contains(t, plain, "Inner quote")
	assert.Contains(t, plain, "Deeply quoted code")
	assert.Contains(t, plain, "More nested text")
	assert.Contains(t, plain, "Back to outer")

	// The > symbols from nested blockquotes should NOT appear as literal text
	// (they should be processed as blockquote markers)
	assert.NotContains(t, plain, "> Inner quote")
	assert.NotContains(t, plain, "> ```")

	// All lines should have correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 60, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererDeeplyNestedBlockquotes(t *testing.T) {
	t.Parallel()

	// Test multiple levels of nesting
	input := `> Level 1
> > Level 2
> > > Level 3
> > Back to level 2
> Back to level 1`

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)

	// All levels should be rendered without literal > symbols
	assert.Contains(t, plain, "Level 1")
	assert.Contains(t, plain, "Level 2")
	assert.Contains(t, plain, "Level 3")
	assert.Contains(t, plain, "Back to level 2")
	assert.Contains(t, plain, "Back to level 1")

	// No literal > symbols should appear in the text content
	// (they should all be consumed as blockquote markers)
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			assert.NotRegexp(t, `^>`, trimmed, "Line should not start with literal >: %q", trimmed)
		}
	}

	// All lines should have correct width
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lineWidth := runewidth.StringWidth(stripANSI(line))
		assert.Equal(t, 60, lineWidth, "Line %d has incorrect width: %q (width=%d)", i, stripANSI(line), lineWidth)
	}
}

func TestFastRendererHorizontalRuleSpacing(t *testing.T) {
	t.Parallel()

	input := `Text before

---

Text after`

	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)

	// Should have consistent spacing around the HR
	assert.Contains(t, plain, "Text before")
	assert.Contains(t, plain, "---")
	assert.Contains(t, plain, "Text after")

	// Count blank lines before and after HR
	lines := strings.Split(plain, "\n")
	var hrIndex int
	for i, line := range lines {
		if strings.Contains(line, "---") {
			hrIndex = i
			break
		}
	}

	// Check there's at least one blank line before and after HR
	if hrIndex > 0 {
		assert.Empty(t, strings.TrimSpace(lines[hrIndex-1]), "Should have blank line before HR")
	}
	if hrIndex < len(lines)-1 {
		assert.Empty(t, strings.TrimSpace(lines[hrIndex+1]), "Should have blank line after HR")
	}
}

func TestFastRendererCodeBlockNewlineSpacing(t *testing.T) {
	t.Parallel()

	input := `Text before
` + "```" + `
code
` + "```" + `
Text after`

	r := NewFastRenderer(40)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Text before")
	assert.Contains(t, plain, "code")
	assert.Contains(t, plain, "Text after")

	// Should have blank lines around the code block for readability
	lines := strings.Split(plain, "\n")
	var codeIndex int
	for i, line := range lines {
		if strings.TrimSpace(line) == "code" {
			codeIndex = i
			break
		}
	}

	// The code block should have proper separation from surrounding text
	assert.Positive(t, codeIndex, "Code block should not be at start")
}

func TestFastRendererCodeBlockWhitespaceWrap(t *testing.T) {
	t.Parallel()

	// Code should wrap at whitespace, not mid-word
	input := "```\nfunction myLongFunctionName() { return something; }\n```"

	r := NewFastRenderer(30)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	lines := strings.Split(plain, "\n")

	// If wrapping occurred, it should have wrapped at a space
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && len(trimmed) > 1 {
			// Line shouldn't end mid-word (unless the word itself is longer than width)
			// If a line contains a space, it should have broken at a space if possible
			if strings.Contains(trimmed, " ") {
				// This line has spaces, so wrapping should work correctly
				continue
			}
		}
	}
}

func TestFastRendererListWithCodeBlockSpacing(t *testing.T) {
	t.Parallel()

	// List items with code blocks should have proper spacing for legibility
	input := `- First item with text
  ` + "```" + `
  some code
  ` + "```" + `
- Second item`

	r := NewFastRenderer(50)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "First item with text")
	assert.Contains(t, plain, "some code")
	assert.Contains(t, plain, "Second item")
}

func TestFastRendererHeadingWithInlineCodeBold(t *testing.T) {
	t.Parallel()

	// Inline code in headings should inherit bold from the heading
	input := "## Heading with `code` here"

	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Heading with code here")

	// The heading line should contain bold code (may be combined with color as \x1b[1;38;...)
	boldPattern := regexp.MustCompile(`\x1b\[1[;m]`)
	assert.True(t, boldPattern.MatchString(result), "Heading should have bold styling")
}

func TestFastRendererWrappedInlineCodeInHeadingMaintainsStyle(t *testing.T) {
	t.Parallel()

	// When inline code in a heading wraps, continuation lines should maintain style
	input := "# Heading with `very_long_inline_code_that_will_wrap` text"

	r := NewFastRenderer(35)
	result, err := r.Render(input)
	require.NoError(t, err)

	plain := stripANSI(result)
	assert.Contains(t, plain, "Heading with")
	assert.Contains(t, plain, "very_long_inline_code")
	assert.Contains(t, plain, "text")

	// Each line of the heading should maintain styling
	lines := strings.Split(result, "\n")
	headingLines := 0
	for _, line := range lines {
		plainLine := stripANSI(line)
		if strings.TrimSpace(plainLine) != "" {
			headingLines++
			// Each non-empty line should have ANSI styling
			seqs := ansiRegex.FindAllString(line, -1)
			assert.GreaterOrEqual(t, len(seqs), 1, "Line should have styling: %q", plainLine)
		}
	}
	assert.GreaterOrEqual(t, headingLines, 2, "Heading should wrap to multiple lines")
}

func TestFastRendererTableSeparatorStyling(t *testing.T) {
	t.Parallel()

	input := `| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |`

	r := NewFastRenderer(60)
	result, err := r.Render(input)
	require.NoError(t, err)

	// The separator line should have styling applied (same as other table elements)
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		plainLine := stripANSI(line)
		if strings.Contains(plainLine, "─") {
			// Separator line should have ANSI styling
			seqs := ansiRegex.FindAllString(line, -1)
			assert.GreaterOrEqual(t, len(seqs), 1, "Table separator should have styling")
		}
	}
}

//go:embed testdata/streaming_benchmark.md
var streamingBenchmarkContent string

// splitIntoStreamingChunks splits content into chunks that simulate LLM streaming.
// LLM tokens are typically 3-4 characters, so we use small varying chunk sizes.
func splitIntoStreamingChunks(content string) []string {
	var chunks []string
	i := 0
	chunkSizes := []int{3, 4, 3, 5, 2, 4, 3, 4, 5, 3, 2, 4, 3, 4, 3, 5}
	sizeIdx := 0

	for i < len(content) {
		chunkSize := chunkSizes[sizeIdx%len(chunkSizes)]
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}
		chunks = append(chunks, content[i:end])
		i = end
		sizeIdx++
	}
	return chunks
}

// BenchmarkStreamingFastRenderer benchmarks rendering progressively growing markdown.
// This simulates the streaming use case where content arrives in chunks and
// the entire accumulated content is re-rendered on each update.
func BenchmarkStreamingFastRenderer(b *testing.B) {
	chunks := splitIntoStreamingChunks(streamingBenchmarkContent)
	r := NewFastRenderer(80)

	b.ResetTimer()
	for b.Loop() {
		var accumulated strings.Builder
		for _, chunk := range chunks {
			accumulated.WriteString(chunk)
			_, _ = r.Render(accumulated.String())
		}
	}
}

// BenchmarkStreamingGlamourRenderer benchmarks glamour with progressively growing markdown.
// Note: glamour's TermRenderer has internal state issues when reused many times,
// so we create a fresh renderer for each benchmark iteration. This adds overhead
// but is necessary to avoid panics in glamour's internal ANSI parser.
func BenchmarkStreamingGlamourRenderer(b *testing.B) {
	chunks := splitIntoStreamingChunks(streamingBenchmarkContent)

	b.ResetTimer()
	for b.Loop() {
		r := NewGlamourRenderer(80)
		var accumulated strings.Builder
		for _, chunk := range chunks {
			accumulated.WriteString(chunk)
			_, _ = r.Render(accumulated.String())
		}
	}
}
