// Package markdown provides a high-performance markdown renderer for terminal output.
// This is a custom implementation optimized for speed, replacing glamour for TUI rendering.
package markdown

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/glamour/v2/ansi"
	runewidth "github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/tui/styles"
)

// ansiStyle holds pre-computed ANSI escape sequences for fast rendering.
// This avoids the overhead of lipgloss.Style.Render() which copies large structs.
type ansiStyle struct {
	prefix string // ANSI codes to start the style
	suffix string // ANSI codes to end the style (reset)
}

func (s ansiStyle) render(text string) string {
	if s.prefix == "" {
		return text
	}
	return s.prefix + text + s.suffix
}

// renderTo writes styled text directly to a builder, avoiding intermediate allocations
func (s ansiStyle) renderTo(b *strings.Builder, text string) {
	if s.prefix == "" {
		b.WriteString(text)
		return
	}
	b.WriteString(s.prefix)
	b.WriteString(text)
	b.WriteString(s.suffix)
}

// buildAnsiStyle extracts ANSI codes from a lipgloss style by rendering an empty marker.
func buildAnsiStyle(style lipgloss.Style) ansiStyle {
	// Render a marker to extract the ANSI prefix/suffix
	const marker = "\x00"
	rendered := style.Render(marker)
	before, after, ok := strings.Cut(rendered, marker)
	if !ok {
		return ansiStyle{}
	}

	return ansiStyle{
		prefix: before,
		suffix: after,
	}
}

// cachedStyles holds pre-computed styles to avoid repeated MarkdownStyle() calls.
type cachedStyles struct {
	// lipgloss styles (for complex rendering like headings)
	headingStyles   [6]lipgloss.Style
	headingPrefixes [6]string
	styleHR         lipgloss.Style
	styleBlockquote lipgloss.Style
	styleCodeBg     lipgloss.Style

	// ANSI styles (for fast inline rendering)
	ansiBold     ansiStyle
	ansiItalic   ansiStyle
	ansiBoldItal ansiStyle
	ansiStrike   ansiStyle
	ansiCode     ansiStyle
	ansiLink     ansiStyle
	ansiLinkText ansiStyle
	ansiText     ansiStyle // base document text style

	styleTaskTicked  string
	styleTaskUntick  string
	listIndent       int
	blockquoteIndent int
	chromaStyle      *chroma.Style
}

var (
	globalStyles     *cachedStyles
	globalStylesOnce sync.Once
)

func getGlobalStyles() *cachedStyles {
	globalStylesOnce.Do(func() {
		mdStyle := styles.MarkdownStyle()

		styleBold := buildStylePrimitive(mdStyle.Strong)
		styleItalic := buildStylePrimitive(mdStyle.Emph)

		textStyle := buildStylePrimitive(mdStyle.Document.StylePrimitive)

		globalStyles = &cachedStyles{
			headingStyles: [6]lipgloss.Style{
				buildStylePrimitive(mdStyle.H1.StylePrimitive),
				buildStylePrimitive(mdStyle.H2.StylePrimitive),
				buildStylePrimitive(mdStyle.H3.StylePrimitive),
				buildStylePrimitive(mdStyle.H4.StylePrimitive),
				buildStylePrimitive(mdStyle.H5.StylePrimitive),
				buildStylePrimitive(mdStyle.H6.StylePrimitive),
			},
			headingPrefixes:  [6]string{"## ", "## ", "### ", "#### ", "##### ", "###### "},
			styleBlockquote:  buildStylePrimitive(mdStyle.BlockQuote.StylePrimitive),
			styleHR:          buildStylePrimitive(mdStyle.HorizontalRule),
			styleCodeBg:      lipgloss.NewStyle(),
			ansiBold:         buildAnsiStyle(styleBold),
			ansiItalic:       buildAnsiStyle(styleItalic),
			ansiBoldItal:     buildAnsiStyle(styleBold.Inherit(styleItalic)),
			ansiStrike:       buildAnsiStyle(buildStylePrimitive(mdStyle.Strikethrough)),
			ansiCode:         buildAnsiStyle(buildStylePrimitive(mdStyle.Code.StylePrimitive)),
			ansiLink:         buildAnsiStyle(buildStylePrimitive(mdStyle.Link)),
			ansiLinkText:     buildAnsiStyle(buildStylePrimitive(mdStyle.LinkText)),
			ansiText:         buildAnsiStyle(textStyle),
			styleTaskTicked:  mdStyle.Task.Ticked,
			styleTaskUntick:  mdStyle.Task.Unticked,
			listIndent:       int(mdStyle.List.LevelIndent),
			blockquoteIndent: 1,
			chromaStyle:      styles.ChromaStyle(),
		}
		if mdStyle.BlockQuote.Indent != nil {
			globalStyles.blockquoteIndent = int(*mdStyle.BlockQuote.Indent)
		}
		if mdStyle.CodeBlock.BackgroundColor != nil {
			globalStyles.styleCodeBg = globalStyles.styleCodeBg.Background(lipgloss.Color(*mdStyle.CodeBlock.BackgroundColor))
		}
	})
	return globalStyles
}

// FastRenderer is a high-performance markdown renderer optimized for terminal output.
// It directly parses and renders markdown without building an intermediate AST.
type FastRenderer struct {
	width int
}

// NewFastRenderer creates a new fast markdown renderer with the given width.
func NewFastRenderer(width int) *FastRenderer {
	return &FastRenderer{width: width}
}

var parserPool = sync.Pool{
	New: func() any {
		return &parser{
			out: strings.Builder{},
		}
	},
}

// Render parses and renders markdown content to styled terminal output.
func (r *FastRenderer) Render(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	input = sanitizeForTerminal(input)

	p := parserPool.Get().(*parser)
	p.reset(input, r.width)
	result := p.parse()
	parserPool.Put(p)
	return padAllLines(result, r.width), nil
}

// parser holds the state for parsing markdown.
type parser struct {
	input   string
	width   int
	styles  *cachedStyles
	out     strings.Builder
	lines   []string
	lineIdx int
}

func (p *parser) reset(input string, width int) {
	p.input = input
	p.width = width
	p.styles = getGlobalStyles()
	p.lines = strings.Split(input, "\n")
	p.lineIdx = 0
	p.out.Reset()
	p.out.Grow(len(input) * 2) // Pre-allocate for styled output
}

func (p *parser) parse() string {
	for p.lineIdx < len(p.lines) {
		line := p.lines[p.lineIdx]

		switch {
		case p.tryCodeBlock(line):
			// handled inside
		case p.tryHeading(line):
			// handled inside
		case p.tryHorizontalRule(line):
			// handled inside
		case p.tryBlockquote(line):
			// handled inside
		case p.tryTable(line):
			// handled inside
		case p.tryList(line):
			// handled inside
		default:
			// Regular paragraph
			p.renderParagraph()
		}
	}

	return strings.TrimRight(p.out.String(), "\n")
}

// tryCodeBlock checks for fenced code blocks (``` or ~~~)
func (p *parser) tryCodeBlock(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~") {
		return false
	}

	fence := trimmed[:3]
	lang := strings.TrimSpace(trimmed[3:])
	p.lineIdx++

	var codeLines []string
	for p.lineIdx < len(p.lines) {
		codeLine := p.lines[p.lineIdx]
		if strings.HasPrefix(strings.TrimSpace(codeLine), fence) {
			p.lineIdx++
			break
		}
		codeLines = append(codeLines, codeLine)
		p.lineIdx++
	}

	code := strings.Join(codeLines, "\n")
	p.renderCodeBlock(code, lang)
	return true
}

// tryHeading checks for ATX-style headings (# through ######)
func (p *parser) tryHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}

	// Count heading level
	level := 0
	for i := 0; i < len(trimmed) && trimmed[i] == '#'; i++ {
		level++
	}
	if level > 6 || level == 0 {
		return false
	}

	// Must have space after #s or be empty
	rest := trimmed[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return false
	}

	content := strings.TrimSpace(rest)
	// Remove trailing #s
	content = strings.TrimRight(content, "# \t")

	style := p.headingStyle(level)
	prefix := p.headingPrefix(level)

	rendered := p.renderInline(content)
	p.out.WriteString(style.Render(prefix+rendered) + "\n\n")
	p.lineIdx++
	return true
}

func (p *parser) headingStyle(level int) lipgloss.Style {
	if level >= 1 && level <= 6 {
		return p.styles.headingStyles[level-1]
	}
	return p.styles.headingStyles[0]
}

func (p *parser) headingPrefix(level int) string {
	if level >= 1 && level <= 6 {
		return p.styles.headingPrefixes[level-1]
	}
	return ""
}

// tryHorizontalRule checks for horizontal rules (---, ***, ___)
func (p *parser) tryHorizontalRule(line string) bool {
	if !isHorizontalRule(line) {
		return false
	}
	p.out.WriteString(p.styles.styleHR.Render("\n--------\n") + "\n")
	p.lineIdx++
	return true
}

// tryBlockquote checks for blockquotes (>)
func (p *parser) tryBlockquote(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, ">") {
		return false
	}

	var quoteLines []string
	for p.lineIdx < len(p.lines) {
		l := strings.TrimLeft(p.lines[p.lineIdx], " \t")
		if !strings.HasPrefix(l, ">") {
			break
		}
		// Remove the > and optional space
		content := strings.TrimPrefix(l, ">")
		content = strings.TrimPrefix(content, " ")
		quoteLines = append(quoteLines, content)
		p.lineIdx++
	}

	// Render blockquote content with indent
	indent := strings.Repeat(" ", p.styles.blockquoteIndent)
	for _, ql := range quoteLines {
		rendered := p.renderInline(ql)
		wrapped := p.wrapText(rendered, p.width-p.styles.blockquoteIndent)
		for wl := range strings.SplitSeq(wrapped, "\n") {
			p.out.WriteString(indent + p.styles.styleBlockquote.Render(wl) + "\n")
		}
	}
	p.out.WriteString("\n")
	return true
}

// tableCell holds pre-rendered cell data to avoid re-rendering
type tableCell struct {
	rendered string // rendered with inline styles
	width    int    // visual width (excluding ANSI codes)
}

// tryTable checks for markdown tables
func (p *parser) tryTable(line string) bool {
	// Tables start with | or have | in them
	if !strings.Contains(line, "|") {
		return false
	}

	// Count table lines first to avoid slice growth
	startIdx := p.lineIdx
	numLines := 0
	for i := p.lineIdx; i < len(p.lines); i++ {
		if !strings.Contains(p.lines[i], "|") {
			break
		}
		numLines++
	}

	if numLines < 2 {
		// Need at least header and separator
		return false
	}

	// Check if second line is a separator (contains only -, |, :, and spaces)
	separator := p.lines[p.lineIdx+1]
	for _, c := range separator {
		if c != '-' && c != '|' && c != ':' && c != ' ' && c != '\t' {
			// Not a valid table
			return false
		}
	}

	// Parse and render cells in one pass
	// Pre-allocate rows slice (numLines - 1 because we skip the separator)
	rows := make([][]tableCell, 0, numLines-1)
	numCols := 0

	for i := range numLines {
		if i == 1 {
			// Skip separator line
			continue
		}
		cells := p.parseAndRenderTableRow(p.lines[p.lineIdx+i])
		if len(cells) > numCols {
			numCols = len(cells)
		}
		rows = append(rows, cells)
	}

	if len(rows) == 0 || numCols == 0 {
		return false
	}

	// Advance line index past all table lines
	p.lineIdx = startIdx + numLines

	// Calculate column widths from pre-computed cell widths
	colWidths := make([]int, numCols)
	for _, row := range rows {
		for i, cell := range row {
			if cell.width > colWidths[i] {
				colWidths[i] = cell.width
			}
		}
	}

	// Pre-calculate separator line (only done once)
	var sepLine string
	{
		// Calculate total separator length
		sepLen := 0
		for i, w := range colWidths {
			sepLen += w
			if i < numCols-1 {
				sepLen += 9 // len("─┼─") in bytes = 9 (3 runes × 3 bytes each)
			}
		}
		var sepBuilder strings.Builder
		sepBuilder.Grow(sepLen)
		for i, w := range colWidths {
			for range w {
				sepBuilder.WriteString("─")
			}
			if i < numCols-1 {
				sepBuilder.WriteString("─┼─")
			}
		}
		sepLine = sepBuilder.String()
	}

	// Render table rows directly to output
	for rowIdx, row := range rows {
		for i, cell := range row {
			if i >= numCols {
				break
			}
			if rowIdx == 0 {
				// Header row - bold, write directly to output
				p.styles.ansiBold.renderTo(&p.out, cell.rendered)
			} else {
				p.out.WriteString(cell.rendered)
			}
			// Add padding
			padding := colWidths[i] - cell.width
			for range padding {
				p.out.WriteByte(' ')
			}
			if i < len(row)-1 {
				p.out.WriteString(" │ ")
			}
		}
		p.out.WriteByte('\n')

		// Add separator after header
		if rowIdx == 0 {
			p.out.WriteString(sepLine)
			p.out.WriteByte('\n')
		}
	}

	p.out.WriteByte('\n')
	return true
}

// parseAndRenderTableRow parses a table row and renders cells in one pass
func (p *parser) parseAndRenderTableRow(line string) []tableCell {
	// Trim leading/trailing whitespace and pipes
	line = strings.TrimSpace(line)
	if line != "" && line[0] == '|' {
		line = line[1:]
	}
	if line != "" && line[len(line)-1] == '|' {
		line = line[:len(line)-1]
	}

	// Count cells first to pre-allocate
	numCells := 1
	for i := range len(line) {
		if line[i] == '|' {
			numCells++
		}
	}

	cells := make([]tableCell, 0, numCells)
	start := 0

	for i := 0; i <= len(line); i++ {
		if i == len(line) || line[i] == '|' {
			// Extract and trim the cell
			cellText := strings.TrimSpace(line[start:i])
			rendered := p.renderInline(cellText)
			cells = append(cells, tableCell{
				rendered: rendered,
				width:    lipgloss.Width(rendered),
			})
			start = i + 1
		}
	}

	return cells
}

type listItem struct {
	content string
	ordered bool
	task    bool
	checked bool
}

// parseListItem attempts to parse a list item from a trimmed line.
// Returns the parsed item and true if successful, or zero value and false otherwise.
func parseListItem(line string) (listItem, bool) {
	if len(line) < 2 {
		return listItem{}, false
	}

	// Check unordered list (-, *, +)
	if (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		content := line[2:]
		item := listItem{content: content}
		if strings.HasPrefix(content, "[ ] ") {
			item.task = true
			item.content = content[4:]
		} else if strings.HasPrefix(content, "[x] ") || strings.HasPrefix(content, "[X] ") {
			item.task = true
			item.checked = true
			item.content = content[4:]
		}
		return item, true
	}

	// Check ordered list (1., 2., etc.)
	dotIdx := strings.Index(line, ".")
	if dotIdx > 0 && dotIdx < 10 && len(line) > dotIdx+1 && line[dotIdx+1] == ' ' {
		for i := range dotIdx {
			if !unicode.IsDigit(rune(line[i])) {
				return listItem{}, false
			}
		}
		return listItem{content: line[dotIdx+2:], ordered: true}, true
	}

	return listItem{}, false
}

// isListStart checks if a line starts a list item
func isListStart(line string) bool {
	_, ok := parseListItem(line)
	return ok
}

// tryList checks for unordered lists (-, *, +) or ordered lists (1., 2., etc.)
func (p *parser) tryList(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if _, ok := parseListItem(trimmed); !ok {
		return false
	}

	for p.lineIdx < len(p.lines) {
		l := p.lines[p.lineIdx]
		ltrimmed := strings.TrimLeft(l, " \t")
		lindent := len(l) - len(ltrimmed)

		item, isListItem := parseListItem(ltrimmed)

		// Empty line handling
		if !isListItem && strings.TrimSpace(l) == "" {
			if p.lineIdx+1 < len(p.lines) {
				nextLine := p.lines[p.lineIdx+1]
				nextTrimmed := strings.TrimLeft(nextLine, " \t")
				if !isListStart(nextTrimmed) {
					break
				}
			} else {
				break
			}
			p.lineIdx++
			continue
		}

		if !isListItem {
			break
		}

		level := lindent / p.styles.listIndent
		bulletIndent := strings.Repeat(" ", level*p.styles.listIndent)

		var bullet string
		switch {
		case item.task && item.checked:
			bullet = p.styles.styleTaskTicked
		case item.task:
			bullet = p.styles.styleTaskUntick
		case item.ordered:
			bullet = ". "
		default:
			bullet = "• "
		}

		// Calculate the width available for content (after bullet and indentation)
		bulletWidth := lipgloss.Width(bulletIndent) + lipgloss.Width(bullet)
		contentWidth := max(p.width-bulletWidth, 10) // Minimum content width of 10

		rendered := p.renderInline(item.content)
		wrapped := p.wrapText(rendered, contentWidth)
		wrappedLines := strings.Split(wrapped, "\n")

		// Write first line with bullet
		if len(wrappedLines) > 0 {
			p.out.WriteString(bulletIndent + bullet + wrappedLines[0] + "\n")
		}

		// Write continuation lines with proper indentation (aligned with content after bullet)
		continuationIndent := strings.Repeat(" ", bulletWidth)
		for i := 1; i < len(wrappedLines); i++ {
			p.out.WriteString(continuationIndent + wrappedLines[i] + "\n")
		}

		p.lineIdx++
	}

	p.out.WriteString("\n")
	return true
}

// renderParagraph collects consecutive non-empty lines and renders them as a paragraph.
func (p *parser) renderParagraph() {
	var paraLines []string
	for p.lineIdx < len(p.lines) {
		line := p.lines[p.lineIdx]
		if strings.TrimSpace(line) == "" {
			p.lineIdx++
			break
		}
		// Check if next line starts a block element
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "```") ||
			strings.HasPrefix(trimmed, "~~~") ||
			strings.HasPrefix(trimmed, ">") ||
			isListStart(trimmed) ||
			isHorizontalRule(trimmed) {
			break
		}
		paraLines = append(paraLines, line)
		p.lineIdx++
	}

	if len(paraLines) == 0 {
		return
	}

	// Join lines and render inline elements
	text := strings.Join(paraLines, " ")
	rendered := p.renderInline(text)
	wrapped := p.wrapText(rendered, p.width)
	p.out.WriteString(wrapped + "\n\n")
}

func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	char := trimmed[0]
	if char != '-' && char != '*' && char != '_' {
		return false
	}
	count := 0
	for _, c := range trimmed {
		if c == rune(char) {
			count++
		} else if !unicode.IsSpace(c) {
			return false
		}
	}
	return count >= 3
}

// renderInline processes inline markdown elements: bold, italic, code, links, etc.
func (p *parser) renderInline(text string) string {
	if text == "" {
		return ""
	}

	// Fast path: check if text contains any markdown characters
	// If not, return as-is without allocating a builder
	if !hasInlineMarkdown(text) {
		return text
	}

	var out strings.Builder
	out.Grow(len(text) + 64) // Pre-allocate with extra space for ANSI codes
	i := 0
	n := len(text)
	needsStyleRestore := false // track if we need to restore text style after styled content

	for i < n {
		// Check for escaped characters
		if text[i] == '\\' && i+1 < n {
			out.WriteByte(text[i+1])
			i += 2
			continue
		}

		// Check for inline code
		if text[i] == '`' {
			end := strings.Index(text[i+1:], "`")
			if end != -1 {
				code := text[i+1 : i+1+end]
				out.WriteString(p.styles.ansiCode.render(code))
				i = i + 1 + end + 1
				needsStyleRestore = true
				continue
			}
		}

		// Check for bold (**text** or __text__)
		if i+1 < n && ((text[i] == '*' && text[i+1] == '*') || (text[i] == '_' && text[i+1] == '_')) {
			delim := text[i : i+2]
			end := strings.Index(text[i+2:], delim)
			if end != -1 {
				inner := text[i+2 : i+2+end]
				// Check for bold+italic (***text***)
				if strings.HasPrefix(inner, "*") && strings.HasSuffix(inner, "*") && len(inner) >= 2 {
					innerText := inner[1 : len(inner)-1]
					out.WriteString(p.styles.ansiBoldItal.render(p.renderInline(innerText)))
				} else {
					out.WriteString(p.styles.ansiBold.render(p.renderInline(inner)))
				}
				i = i + 2 + end + 2
				needsStyleRestore = true
				continue
			}
		}

		// Check for italic (*text* or _text_) - but not in the middle of words for _
		if text[i] == '*' || (text[i] == '_' && (i == 0 || !isWord(text[i-1]))) {
			delim := text[i]
			end := -1
			for j := i + 1; j < n; j++ {
				if text[j] == delim {
					// For underscore, check it's not in the middle of a word
					if delim == '_' && j+1 < n && isWord(text[j+1]) {
						continue
					}
					end = j
					break
				}
			}
			if end != -1 && end > i+1 {
				inner := text[i+1 : end]
				out.WriteString(p.styles.ansiItalic.render(p.renderInline(inner)))
				i = end + 1
				needsStyleRestore = true
				continue
			}
		}

		// Check for strikethrough (~~text~~)
		if i+1 < n && text[i] == '~' && text[i+1] == '~' {
			end := strings.Index(text[i+2:], "~~")
			if end != -1 {
				inner := text[i+2 : i+2+end]
				out.WriteString(p.styles.ansiStrike.render(p.renderInline(inner)))
				i = i + 2 + end + 2
				needsStyleRestore = true
				continue
			}
		}

		// Check for links [text](url)
		switch c := text[i]; c {
		case '[':
			closeBracket := findClosingBracket(text[i:])
			if closeBracket != -1 && i+closeBracket+1 < n && text[i+closeBracket+1] == '(' {
				linkText := text[i+1 : i+closeBracket]
				rest := text[i+closeBracket+2:]
				closeParen := strings.Index(rest, ")")
				if closeParen != -1 {
					url := rest[:closeParen]
					styledText := p.styles.ansiLinkText.render(linkText)
					if linkText != url {
						out.WriteString(styledText + " " + p.styles.ansiLink.render("("+url+")"))
					} else {
						out.WriteString(p.styles.ansiLink.render(linkText))
					}
					i = i + closeBracket + 2 + closeParen + 1
					needsStyleRestore = true
					continue
				}
			}
			fallthrough
		default:
			// Regular character - collect consecutive plain text
			start := i
			for i < n && !isInlineMarker(text[i]) {
				i++
			}
			// If we didn't advance (started on an unmatched marker), consume it as literal
			if i == start {
				i++
			}
			// Only apply text style if we need to restore after styled content
			if needsStyleRestore {
				p.styles.ansiText.renderTo(&out, text[start:i])
				needsStyleRestore = false
			} else {
				out.WriteString(text[start:i])
			}
		}
	}

	return out.String()
}

func findClosingBracket(text string) int {
	depth := 0
	for i, c := range text {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isWord(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// hasInlineMarkdown checks if text contains any markdown formatting characters.
// This allows a fast path to skip processing plain text.
func hasInlineMarkdown(text string) bool {
	for i := range len(text) {
		if isInlineMarker(text[i]) {
			return true
		}
	}
	return false
}

func isInlineMarker(b byte) bool {
	switch b {
	case '\\', '`', '*', '_', '~', '[':
		return true
	}
	return false
}

// renderCodeBlock renders a fenced code block with syntax highlighting.
func (p *parser) renderCodeBlock(code, lang string) {
	if code == "" {
		p.out.WriteString("\n")
		return
	}

	// Get syntax highlighting tokens
	tokens := p.syntaxHighlight(code, lang)

	// Calculate content width with padding
	const paddingLeft = 2
	const paddingRight = 2
	contentWidth := p.width - paddingLeft - paddingRight
	if contentWidth < 20 {
		contentWidth = p.width
	}

	paddingLeftStr := strings.Repeat(" ", paddingLeft)

	// Pre-compute background padding style
	bgStyle := buildAnsiStyle(p.styles.styleCodeBg)

	// Render empty line at the top
	p.out.WriteString(bgStyle.render(strings.Repeat(" ", p.width)) + "\n")

	// Process tokens line by line for better performance
	var lineBuilder strings.Builder
	lineBuilder.Grow(contentWidth + 32)
	lineWidth := 0

	flushLine := func() {
		lineContent := lineBuilder.String()
		// Pad to full width (including right padding)
		padWidth := contentWidth - lineWidth + paddingRight
		if padWidth > 0 {
			lineContent += bgStyle.render(strings.Repeat(" ", padWidth))
		}
		// Add left padding with background
		p.out.WriteString(bgStyle.render(paddingLeftStr))
		p.out.WriteString(lineContent)
		p.out.WriteByte('\n')
		lineBuilder.Reset()
		lineWidth = 0
	}

	for _, tok := range tokens {
		text := tok.text

		// Process text, splitting by newlines and handling tabs
		start := 0
		for i := range len(text) {
			if text[i] == '\n' {
				// Render text before newline
				if i > start {
					segment := text[start:i]
					segment = expandTabs(segment, lineWidth)
					lineBuilder.WriteString(tok.style.render(segment))
					lineWidth += stringDisplayWidth(segment)
				}
				flushLine()
				start = i + 1
			}
		}
		// Render remaining text
		if start < len(text) {
			segment := text[start:]
			segment = expandTabs(segment, lineWidth)
			lineBuilder.WriteString(tok.style.render(segment))
			lineWidth += stringDisplayWidth(segment)
		}
	}

	// Flush remaining content
	if lineBuilder.Len() > 0 {
		flushLine()
	}

	// Render empty line at the bottom
	p.out.WriteString(bgStyle.render(strings.Repeat(" ", p.width)) + "\n")

	p.out.WriteByte('\n')
}

// expandTabs replaces tabs with spaces based on current position
func expandTabs(s string, currentWidth int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var result strings.Builder
	width := currentWidth
	for _, r := range s {
		if r == '\t' {
			spaces := 4 - (width % 4)
			result.WriteString(strings.Repeat(" ", spaces))
			width += spaces
		} else {
			result.WriteRune(r)
			width += runewidth.RuneWidth(r)
		}
	}
	return result.String()
}

// stringDisplayWidth calculates the display width of a string (handling unicode)
func stringDisplayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runewidth.RuneWidth(r)
	}
	return width
}

// padAllLines pads each line to the target width with trailing spaces.
func padAllLines(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}

	// Pre-allocate result buffer - estimate final size
	var result strings.Builder
	result.Grow(len(s) + len(s)/40*width) // rough estimate for padding

	start := 0
	for i := range len(s) + 1 {
		if i != len(s) && s[i] != '\n' {
			continue
		}

		line := s[start:i]
		result.WriteString(line)

		lineWidth := lipgloss.Width(line)
		if lineWidth < width {
			// Pad with spaces
			pad := width - lineWidth
			for range pad {
				result.WriteByte(' ')
			}
		}

		if i < len(s) {
			result.WriteByte('\n')
		}
		start = i + 1
	}

	return result.String()
}

type token struct {
	text  string
	style ansiStyle
}

var (
	lexerCache   = make(map[string]chroma.Lexer)
	lexerCacheMu sync.RWMutex

	// Cache for chroma token type to ansiStyle conversion (with code bg)
	chromaStyleCache   = make(map[chroma.TokenType]ansiStyle)
	chromaStyleCacheMu sync.RWMutex
)

func (p *parser) syntaxHighlight(code, lang string) []token {
	var lexer chroma.Lexer

	if lang != "" {
		// Try cache first
		lexerCacheMu.RLock()
		lexer = lexerCache[lang]
		lexerCacheMu.RUnlock()

		if lexer == nil {
			lexer = lexers.Get(lang)
			if lexer == nil {
				// Try with file extension
				lexer = lexers.Match("file." + lang)
			}
			if lexer != nil {
				lexer = chroma.Coalesce(lexer)
				lexerCacheMu.Lock()
				lexerCache[lang] = lexer
				lexerCacheMu.Unlock()
			}
		}
	}

	if lexer == nil {
		// No highlighting - return plain text with code background
		return []token{{text: code, style: p.getCodeStyle(chroma.None)}}
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return []token{{text: code, style: p.getCodeStyle(chroma.None)}}
	}

	chromaTokens := iterator.Tokens()
	tokens := make([]token, 0, len(chromaTokens))
	for _, tok := range chromaTokens {
		if tok.Value == "" {
			continue
		}
		tokens = append(tokens, token{
			text:  tok.Value,
			style: p.getCodeStyle(tok.Type),
		})
	}

	return tokens
}

func (p *parser) getCodeStyle(tokenType chroma.TokenType) ansiStyle {
	chromaStyleCacheMu.RLock()
	style, ok := chromaStyleCache[tokenType]
	chromaStyleCacheMu.RUnlock()
	if ok {
		return style
	}

	// Build lipgloss style with code background inherited
	lipStyle := chromaToLipgloss(tokenType, p.styles.chromaStyle).Inherit(p.styles.styleCodeBg)
	style = buildAnsiStyle(lipStyle)

	chromaStyleCacheMu.Lock()
	chromaStyleCache[tokenType] = style
	chromaStyleCacheMu.Unlock()
	return style
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

// wrapText wraps text to the given width, respecting ANSI escape sequences.
func (p *parser) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	result.Grow(len(text) + len(text)/40) // estimate for newlines

	var currentLine strings.Builder
	currentLine.Grow(width + 32) // typical line length + ANSI codes
	currentWidth := 0

	words := splitWords(text)
	for _, word := range words {
		wordWidth := lipgloss.Width(word)

		if wordWidth > width {
			if currentLine.Len() > 0 {
				result.WriteString(currentLine.String())
				result.WriteByte('\n')
				currentLine.Reset()
				currentWidth = 0
			}

			broken := breakWord(word, width)
			for i, part := range broken {
				if i > 0 {
					result.WriteByte('\n')
				}
				result.WriteString(part)
			}
			result.WriteByte('\n')
			continue
		}

		spaceWidth := 0
		if currentWidth > 0 {
			spaceWidth = 1
		}

		if currentWidth+spaceWidth+wordWidth > width {
			result.WriteString(currentLine.String())
			result.WriteByte('\n')
			currentLine.Reset()
			currentWidth = 0
			spaceWidth = 0
		}

		if spaceWidth > 0 {
			currentLine.WriteByte(' ')
			currentWidth++
		}

		currentLine.WriteString(word)
		currentWidth += wordWidth
	}

	if currentLine.Len() > 0 {
		result.WriteString(currentLine.String())
	}

	return result.String()
}

func splitWords(text string) []string {
	// Count words to pre-allocate (rough estimate: words are separated by spaces)
	wordCount := 1
	for i := range len(text) {
		if text[i] == ' ' || text[i] == '\t' {
			wordCount++
		}
	}

	words := make([]string, 0, wordCount)
	var current strings.Builder
	current.Grow(32) // Pre-allocate for typical word length
	inAnsi := false

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			// Start of ANSI sequence
			inAnsi = true
			current.WriteByte(text[i])
			i++
			continue
		}
		if inAnsi {
			current.WriteByte(text[i])
			if (text[i] >= 'a' && text[i] <= 'z') || (text[i] >= 'A' && text[i] <= 'Z') {
				inAnsi = false
			}
			i++
			continue
		}

		if text[i] == ' ' || text[i] == '\t' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(text[i:])
		current.WriteRune(r)
		i += size
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

func breakWord(word string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{word}
	}

	var parts []string
	var current strings.Builder
	currentWidth := 0
	inAnsi := false
	var ansiSeq strings.Builder

	for i := 0; i < len(word); {
		if word[i] == '\x1b' {
			inAnsi = true
			ansiSeq.WriteByte(word[i])
			i++
			continue
		}
		if inAnsi {
			ansiSeq.WriteByte(word[i])
			if (word[i] >= 'a' && word[i] <= 'z') || (word[i] >= 'A' && word[i] <= 'Z') {
				inAnsi = false
				current.WriteString(ansiSeq.String())
				ansiSeq.Reset()
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(word[i:])
		rw := runewidth.RuneWidth(r)

		if currentWidth+rw > maxWidth && currentWidth > 0 {
			parts = append(parts, current.String())
			current.Reset()
			currentWidth = 0
		}

		current.WriteRune(r)
		currentWidth += rw
		i += size
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// buildStylePrimitive converts an ansi.StylePrimitive to a lipgloss.Style.
// sanitizeReplacer is a package-level replacer to avoid rebuilding it on every call.
// This is critical for performance - building a replacer is expensive.
var sanitizeReplacer = strings.NewReplacer(
	"\r", "",
	"\b", "",
	"\f", "",
	"\v", "",
)

func sanitizeForTerminal(s string) string {
	if s == "" {
		return s
	}

	// Strip control chars that change cursor position / layout.
	// Keep \n and \t (tab will be expanded later).
	return sanitizeReplacer.Replace(s)
}

func buildStylePrimitive(sp ansi.StylePrimitive) lipgloss.Style {
	style := lipgloss.NewStyle()

	if sp.Color != nil {
		style = style.Foreground(lipgloss.Color(*sp.Color))
	}
	if sp.BackgroundColor != nil {
		style = style.Background(lipgloss.Color(*sp.BackgroundColor))
	}
	if sp.Bold != nil && *sp.Bold {
		style = style.Bold(true)
	}
	if sp.Italic != nil && *sp.Italic {
		style = style.Italic(true)
	}
	if sp.Underline != nil && *sp.Underline {
		style = style.Underline(true)
	}
	if sp.CrossedOut != nil && *sp.CrossedOut {
		style = style.Strikethrough(true)
	}

	return style
}
