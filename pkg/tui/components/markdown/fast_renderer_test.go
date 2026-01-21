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

func TestFastRendererInlineCode(t *testing.T) {
	t.Parallel()

	input := "Use `fmt.Println` to print"
	r := NewFastRenderer(80)
	result, err := r.Render(input)
	require.NoError(t, err)
	assert.Contains(t, result, "fmt.Println")
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
	assert.Contains(t, result, "•")
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
	assert.Contains(t, result, "*not italic*")
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
	assert.Contains(t, lines[0], "•", "First line should contain bullet")

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
	assert.True(t, strings.HasPrefix(lines[0], "•"), "First line should start with bullet")
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

	// We should have:
	// 1. Code style (foreground + background for inline code)
	// 2. Reset
	// 3. Base text style restoration (document foreground color)
	require.GreaterOrEqual(t, len(seqs), 3, "Should have at least 3 ANSI sequences")

	// First sequence should be the code style (has RGB foreground and background)
	assert.Contains(t, seqs[0], "38;2;", "Code style should have RGB foreground")
	assert.Contains(t, seqs[0], "48;2;", "Code style should have RGB background")

	// Second sequence should be reset
	assert.Equal(t, "\x1b[m", seqs[1], "Second sequence should be reset")

	// Third sequence should be the base text style (document color 252)
	assert.Contains(t, seqs[2], "38;5;252", "Third sequence should restore document text color")
}

func TestInlineCodeTextContent(t *testing.T) {
	t.Parallel()

	r := NewFastRenderer(80)
	result, err := r.Render("Hello `there` beautiful")
	require.NoError(t, err)

	plain := ansi.Strip(result)
	require.Contains(t, plain, "Hello there beautiful")
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
