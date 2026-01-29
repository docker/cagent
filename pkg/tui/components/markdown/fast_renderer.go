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
// Flags track formatting attributes to avoid string scanning in hot paths.
type ansiStyle struct {
	prefix    string // ANSI codes to start the style
	suffix    string // ANSI codes to end the style (reset)
	hasBold   bool   // Whether this style includes bold
	hasStrike bool   // Whether this style includes strikethrough
	hasItalic bool   // Whether this style includes italic
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

// withBold returns a new style with bold formatting added
// Bold is applied first, then the parent color, to prevent "bright bold" terminals
// from overriding the color when bold is enabled.
func (s ansiStyle) withBold() ansiStyle {
	if s.prefix == "" {
		return ansiStyle{prefix: "\x1b[1m", suffix: "\x1b[m", hasBold: true}
	}
	return ansiStyle{
		prefix:    "\x1b[1m" + s.prefix,  // Bold first, then color (prevents bright-bold color override)
		suffix:    "\x1b[22m" + s.prefix, // Turn off bold, re-apply parent style
		hasBold:   true,
		hasStrike: s.hasStrike,
		hasItalic: s.hasItalic,
	}
}

// withItalic returns a new style with italic formatting added
// Format attribute applied first, then color, for consistency with withBold.
func (s ansiStyle) withItalic() ansiStyle {
	if s.prefix == "" {
		return ansiStyle{prefix: "\x1b[3m", suffix: "\x1b[m", hasItalic: true}
	}
	return ansiStyle{
		prefix:    "\x1b[3m" + s.prefix,  // Italic first, then color
		suffix:    "\x1b[23m" + s.prefix, // Turn off italic, re-apply parent style
		hasBold:   s.hasBold,
		hasStrike: s.hasStrike,
		hasItalic: true,
	}
}

// withBoldItalic returns a new style with bold and italic formatting added
// Format attributes applied first, then color, to prevent "bright bold" terminals
// from overriding the color when bold is enabled.
func (s ansiStyle) withBoldItalic() ansiStyle {
	if s.prefix == "" {
		return ansiStyle{prefix: "\x1b[1;3m", suffix: "\x1b[m", hasBold: true, hasItalic: true}
	}
	return ansiStyle{
		prefix:    "\x1b[1;3m" + s.prefix,   // Bold+italic first, then color
		suffix:    "\x1b[22;23m" + s.prefix, // Turn off bold and italic, re-apply parent style
		hasBold:   true,
		hasStrike: s.hasStrike,
		hasItalic: true,
	}
}

// withStrikethrough returns a new style with strikethrough formatting added
// Format attribute applied first, then color, for consistency with withBold.
func (s ansiStyle) withStrikethrough() ansiStyle {
	if s.prefix == "" {
		return ansiStyle{prefix: "\x1b[9m", suffix: "\x1b[m", hasStrike: true}
	}
	return ansiStyle{
		prefix:    "\x1b[9m" + s.prefix,  // Strikethrough first, then color
		suffix:    "\x1b[29m" + s.prefix, // Turn off strikethrough, re-apply parent style
		hasBold:   s.hasBold,
		hasStrike: true,
		hasItalic: s.hasItalic,
	}
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
	ansiBold       ansiStyle
	ansiItalic     ansiStyle
	ansiBoldItal   ansiStyle
	ansiStrike     ansiStyle
	ansiCode       ansiStyle
	ansiLink       ansiStyle
	ansiLinkText   ansiStyle
	ansiText       ansiStyle    // base document text style
	ansiHeadings   [6]ansiStyle // heading styles for inline restoration
	ansiBlockquote ansiStyle    // blockquote style for inline restoration
	ansiFootnote   ansiStyle    // footnote reference style
	ansiCodeBg     ansiStyle    // code block background (cached to avoid repeated buildAnsiStyle)

	styleTaskTicked  string
	styleTaskUntick  string
	listIndent       int
	blockquoteIndent int
	chromaStyle      *chroma.Style

	// Pre-rendered table chrome
	styledTableSep string // styled " │ " for table columns
}

var (
	globalStyles     *cachedStyles
	globalStylesOnce sync.Once
	globalStylesMu   sync.Mutex
)

// ResetStyles resets the cached markdown styles so they will be rebuilt on next use.
// Call this when the theme changes to pick up new colors.
func ResetStyles() {
	globalStylesMu.Lock()
	globalStyles = nil
	globalStylesOnce = sync.Once{}
	globalStylesMu.Unlock()

	// Also clear chroma syntax highlighting cache
	chromaStyleCacheMu.Lock()
	chromaStyleCache = make(map[chroma.TokenType]ansiStyle)
	chromaStyleCacheMu.Unlock()
}

func getGlobalStyles() *cachedStyles {
	globalStylesMu.Lock()
	defer globalStylesMu.Unlock()

	globalStylesOnce.Do(func() {
		mdStyle := styles.MarkdownStyle()

		styleBold := buildStylePrimitive(mdStyle.Strong)
		styleItalic := buildStylePrimitive(mdStyle.Emph)

		textStyle := buildStylePrimitive(mdStyle.Document.StylePrimitive)

		// Build heading lipgloss styles - always include bold for consistency
		headingLipStyles := [6]lipgloss.Style{
			buildStylePrimitive(mdStyle.H1.StylePrimitive).Bold(true),
			buildStylePrimitive(mdStyle.H2.StylePrimitive).Bold(true),
			buildStylePrimitive(mdStyle.H3.StylePrimitive).Bold(true),
			buildStylePrimitive(mdStyle.H4.StylePrimitive).Bold(true),
			buildStylePrimitive(mdStyle.H5.StylePrimitive).Bold(true),
			buildStylePrimitive(mdStyle.H6.StylePrimitive).Bold(true),
		}

		// Build blockquote lipgloss style
		blockquoteLipStyle := buildStylePrimitive(mdStyle.BlockQuote.StylePrimitive)

		globalStyles = &cachedStyles{
			headingStyles:   headingLipStyles,
			headingPrefixes: [6]string{"## ", "## ", "### ", "#### ", "##### ", "###### "},
			styleBlockquote: blockquoteLipStyle,
			styleHR:         buildStylePrimitive(mdStyle.HorizontalRule),
			styleCodeBg:     lipgloss.NewStyle(),
			ansiBold:        buildAnsiStyle(styleBold),
			ansiItalic:      buildAnsiStyle(styleItalic),
			ansiBoldItal:    buildAnsiStyle(styleBold.Inherit(styleItalic)),
			ansiStrike:      buildAnsiStyle(buildStylePrimitive(mdStyle.Strikethrough)),
			ansiCode:        buildAnsiStyle(buildStylePrimitive(mdStyle.Code.StylePrimitive)),
			ansiLink:        buildAnsiStyle(buildStylePrimitive(mdStyle.Link)),
			ansiLinkText:    buildAnsiStyle(buildStylePrimitive(mdStyle.LinkText)),
			ansiText:        buildAnsiStyle(textStyle),
			ansiHeadings: [6]ansiStyle{
				buildAnsiStyle(headingLipStyles[0]),
				buildAnsiStyle(headingLipStyles[1]),
				buildAnsiStyle(headingLipStyles[2]),
				buildAnsiStyle(headingLipStyles[3]),
				buildAnsiStyle(headingLipStyles[4]),
				buildAnsiStyle(headingLipStyles[5]),
			},
			ansiBlockquote:   buildAnsiStyle(blockquoteLipStyle),
			ansiFootnote:     buildAnsiStyle(lipgloss.NewStyle().Foreground(styles.TextSecondary).Italic(true)),
			styleTaskTicked:  mdStyle.Task.Ticked,
			styleTaskUntick:  mdStyle.Task.Unticked,
			listIndent:       int(mdStyle.List.LevelIndent),
			blockquoteIndent: 1,
			chromaStyle:      styles.ChromaStyle(),
		}
		for i := range globalStyles.ansiHeadings {
			globalStyles.ansiHeadings[i].hasBold = true
		}
		if mdStyle.BlockQuote.Indent != nil {
			globalStyles.blockquoteIndent = int(*mdStyle.BlockQuote.Indent)
		}
		if mdStyle.CodeBlock.BackgroundColor != nil {
			globalStyles.styleCodeBg = globalStyles.styleCodeBg.Background(lipgloss.Color(*mdStyle.CodeBlock.BackgroundColor))
		}
		// Cache ANSI version of code background style (must be after styleCodeBg is fully configured)
		globalStyles.ansiCodeBg = buildAnsiStyle(globalStyles.styleCodeBg)
		// Cache styled table separator
		globalStyles.styledTableSep = globalStyles.ansiText.render(" │ ")
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
	// Reuse lines slice capacity to avoid allocation
	p.lines = p.lines[:0]
	for line := range strings.SplitSeq(input, "\n") {
		p.lines = append(p.lines, line)
	}
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
		case p.tryFootnoteDefinition(line):
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

	// Build code directly into a builder to avoid slice + Join allocation
	var code strings.Builder
	first := true
	for p.lineIdx < len(p.lines) {
		codeLine := p.lines[p.lineIdx]
		if strings.HasPrefix(strings.TrimSpace(codeLine), fence) {
			p.lineIdx++
			break
		}
		if !first {
			code.WriteByte('\n')
		}
		code.WriteString(codeLine)
		first = false
		p.lineIdx++
	}

	p.renderCodeBlock(code.String(), lang)
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
	ansiStyle := p.headingAnsiStyle(level)
	prefix := p.headingPrefix(level)

	// Headings are bold by default. If the entire heading is wrapped in emphasis,
	// strip the wrapper and only apply italics when requested.
	headingItalic := false
	switch {
	case strings.HasPrefix(content, "***") && strings.HasSuffix(content, "***") && len(content) > 6:
		content = content[3 : len(content)-3]
		headingItalic = true
	case strings.HasPrefix(content, "**") && strings.HasSuffix(content, "**") && len(content) > 4:
		content = content[2 : len(content)-2]
	case strings.HasPrefix(content, "__") && strings.HasSuffix(content, "__") && len(content) > 4:
		content = content[2 : len(content)-2]
	case strings.HasPrefix(content, "*") && strings.HasSuffix(content, "*") && len(content) > 2:
		content = content[1 : len(content)-1]
		headingItalic = true
	case strings.HasPrefix(content, "_") && strings.HasSuffix(content, "_") && len(content) > 2:
		content = content[1 : len(content)-1]
		headingItalic = true
	}
	if headingItalic {
		ansiStyle = ansiStyle.withItalic()
	}

	// Use heading-aware inline rendering so styled elements restore to heading style
	rendered := p.renderInlineWithStyle(content, ansiStyle)
	// Calculate available width for content (accounting for prefix)
	// Note: prefix is always ASCII (e.g., "## "), so len() == visual width
	prefixWidth := len(prefix)
	contentWidth := p.width - prefixWidth
	if contentWidth < 10 {
		contentWidth = p.width
	}

	// Wrap the rendered content and style each line
	// The content already has ANSI codes from renderInlineWithStyle, which uses
	// the heading's ansiStyle for restoration. We only need to style the prefix.
	wrapped := p.wrapText(rendered, contentWidth)
	styledPrefix := style.Render(prefix)
	// Lazy-compute continuation indent (only computed if we have multiple lines)
	var styledContinuationIndent string
	first := true
	for l := range strings.SplitSeq(wrapped, "\n") {
		if first {
			p.out.WriteString(styledPrefix)
			p.out.WriteString(l)
			p.out.WriteByte('\n')
			first = false
		} else {
			// Continuation lines get indented to align with content
			if styledContinuationIndent == "" {
				styledContinuationIndent = style.Render(spaces(prefixWidth))
			}
			p.out.WriteString(styledContinuationIndent)
			p.out.WriteString(l)
			p.out.WriteByte('\n')
		}
	}
	p.out.WriteByte('\n')
	p.lineIdx++
	return true
}

func (p *parser) headingStyle(level int) lipgloss.Style {
	if level >= 1 && level <= 6 {
		return p.styles.headingStyles[level-1]
	}
	return p.styles.headingStyles[0]
}

func (p *parser) headingAnsiStyle(level int) ansiStyle {
	if level >= 1 && level <= 6 {
		return p.styles.ansiHeadings[level-1]
	}
	return p.styles.ansiHeadings[0]
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
	// Render rule with consistent spacing: content + blank line after
	// Previous elements already end with \n\n, so we get one blank line before
	p.out.WriteString(p.styles.styleHR.Render("--------") + "\n\n")
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
	indent := spaces(p.styles.blockquoteIndent)
	availableWidth := p.width - p.styles.blockquoteIndent
	p.renderBlockquoteContent(quoteLines, indent, availableWidth)
	p.out.WriteString("\n")
	return true
}

// renderBlockquoteContent renders the content of a blockquote, handling fenced code blocks and nested blockquotes
func (p *parser) renderBlockquoteContent(lines []string, indent string, availableWidth int) {
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check for nested blockquote (line starts with >)
		if strings.HasPrefix(trimmed, ">") {
			// Collect all consecutive nested blockquote lines
			var nestedLines []string
			for i < len(lines) {
				l := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(l, ">") {
					break
				}
				// Strip the > and optional space
				content := strings.TrimPrefix(l, ">")
				content = strings.TrimPrefix(content, " ")
				nestedLines = append(nestedLines, content)
				i++
			}

			// Render the nested blockquote with additional indentation
			nestedIndent := indent + spaces(p.styles.blockquoteIndent)
			nestedWidth := availableWidth - p.styles.blockquoteIndent
			if nestedWidth < 10 {
				nestedWidth = 10 // Minimum content width
			}
			p.renderBlockquoteContent(nestedLines, nestedIndent, nestedWidth)
			continue
		}

		// Check for fenced code block start
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence := trimmed[:3]
			lang := strings.TrimSpace(trimmed[3:])
			i++

			// Collect code lines until fence end
			var codeLines []string
			for i < len(lines) {
				codeLine := lines[i]
				if strings.HasPrefix(strings.TrimSpace(codeLine), fence) {
					i++
					break
				}
				codeLines = append(codeLines, codeLine)
				i++
			}

			// Render the code block within blockquote context
			code := strings.Join(codeLines, "\n")
			p.renderBlockquoteCodeBlock(code, lang, indent, availableWidth)
			continue
		}

		// Regular line - render with blockquote-aware inline styling
		rendered := p.renderInlineWithStyle(line, p.styles.ansiBlockquote)
		wrapped := p.wrapText(rendered, availableWidth)
		for wl := range strings.SplitSeq(wrapped, "\n") {
			p.out.WriteString(indent + p.styles.styleBlockquote.Render(wl) + "\n")
		}
		i++
	}
}

// renderBlockquoteCodeBlock renders a fenced code block within a blockquote
func (p *parser) renderBlockquoteCodeBlock(code, lang, indent string, availableWidth int) {
	// Add spacing before code block
	p.out.WriteString("\n")

	if code == "" {
		return
	}

	p.renderCodeBlockWithIndent(code, lang, indent, availableWidth)
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
	// Use ansiText style for table chrome (separators, dividers) to match content
	textStyle := p.styles.ansiText

	// Pre-render the styled separator line (computed once per table)
	styledSepLine := textStyle.render(sepLine)

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
			// Add padding - plain spaces don't need styling
			padding := colWidths[i] - cell.width
			if padding > 0 {
				p.out.WriteString(spaces(padding))
			}
			if i < len(row)-1 {
				p.out.WriteString(p.styles.styledTableSep)
			}
		}
		p.out.WriteByte('\n')

		// Add separator after header (styled to match content)
		if rowIdx == 0 {
			p.out.WriteString(styledSepLine)
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
		if i < len(line) && line[i] != '|' {
			continue
		}
		// Extract and trim the cell
		cellText := strings.TrimSpace(line[start:i])
		var rendered string
		var width int
		// Fast path: if cell has no markdown, skip full inline rendering
		if !hasInlineMarkdown(cellText) {
			// Apply base text style directly
			rendered = p.styles.ansiText.render(cellText)
			width = textWidth(cellText)
		} else {
			// Use renderInlineWithWidth for markdown content
			rendered, width = p.renderInlineWithWidth(cellText)
		}
		cells = append(cells, tableCell{
			rendered: rendered,
			width:    width,
		})
		start = i + 1
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

	// Track the current list item's bullet width for continuation content (code blocks)
	var currentBulletWidth int

	for p.lineIdx < len(p.lines) {
		l := p.lines[p.lineIdx]
		ltrimmed := strings.TrimLeft(l, " \t")
		lindent := len(l) - len(ltrimmed)

		item, isListItem := parseListItem(ltrimmed)

		// Check for fenced code block within list context
		if !isListItem && (strings.HasPrefix(ltrimmed, "```") || strings.HasPrefix(ltrimmed, "~~~")) {
			// This is a code block - check if it's indented (part of list)
			if lindent > 0 || currentBulletWidth > 0 {
				// Render the code block with list indentation
				p.renderListCodeBlock(lindent, currentBulletWidth)
				continue
			}
			// Not indented, break out of list
			break
		}

		// Check for blockquote within list context
		if !isListItem && strings.HasPrefix(ltrimmed, ">") {
			// This is a blockquote - check if it's indented (part of list)
			if lindent > 0 || currentBulletWidth > 0 {
				// Render the blockquote with list indentation
				p.renderListBlockquote(currentBulletWidth)
				continue
			}
			// Not indented, break out of list
			break
		}

		// Empty line handling
		if !isListItem && strings.TrimSpace(l) == "" {
			if p.lineIdx+1 < len(p.lines) {
				nextLine := p.lines[p.lineIdx+1]
				nextTrimmed := strings.TrimLeft(nextLine, " \t")
				nextIndent := len(nextLine) - len(nextTrimmed)
				// Continue if next line is a list item OR indented content (like code block)
				if !isListStart(nextTrimmed) && nextIndent == 0 {
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
		bulletIndent := spaces(level * p.styles.listIndent)

		var bullet string
		switch {
		case item.task && item.checked:
			bullet = p.styles.styleTaskTicked
		case item.task:
			bullet = p.styles.styleTaskUntick
		default:
			// Use consistent bullet for both ordered and unordered lists
			bullet = "- "
		}

		// Calculate the width available for content (after bullet and indentation)
		// bulletIndent is always ASCII spaces, bullet may contain unicode for task items
		bulletWidth := len(bulletIndent) + textWidth(bullet)
		contentWidth := max(p.width-bulletWidth, 10) // Minimum content width of 10

		// Store current list item's bullet width for code blocks
		currentBulletWidth = bulletWidth

		rendered := p.renderInline(item.content)
		wrapped := p.wrapText(rendered, contentWidth)

		// Pre-compute continuation indent
		continuationIndent := spaces(bulletWidth)
		first := true
		for l := range strings.SplitSeq(wrapped, "\n") {
			if first {
				// Write first line with bullet
				p.out.WriteString(bulletIndent)
				p.out.WriteString(bullet)
				p.out.WriteString(l)
				p.out.WriteByte('\n')
				first = false
			} else {
				// Write continuation lines with proper indentation
				p.out.WriteString(continuationIndent)
				p.out.WriteString(l)
				p.out.WriteByte('\n')
			}
		}

		p.lineIdx++
	}

	p.out.WriteString("\n")
	return true
}

// tryFootnoteDefinition checks for footnote definitions [^id]: content
func (p *parser) tryFootnoteDefinition(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "[^") {
		return false
	}

	// Find the closing ]
	closeBracket := strings.Index(trimmed, "]")
	if closeBracket == -1 || closeBracket < 3 {
		return false
	}

	// Must be followed by :
	if len(trimmed) <= closeBracket+1 || trimmed[closeBracket+1] != ':' {
		return false
	}

	// Extract footnote ID and content
	footnoteID := trimmed[1:closeBracket] // includes the ^
	content := strings.TrimSpace(trimmed[closeBracket+2:])

	p.lineIdx++

	// Collect continuation lines (indented)
	var contentLines []string
	if content != "" {
		contentLines = append(contentLines, content)
	}
	for p.lineIdx < len(p.lines) {
		nextLine := p.lines[p.lineIdx]
		// Check if it's a continuation (indented or empty)
		if strings.TrimSpace(nextLine) == "" {
			// Empty line might continue the footnote
			if p.lineIdx+1 < len(p.lines) {
				followingLine := p.lines[p.lineIdx+1]
				if followingLine != "" && (followingLine[0] == ' ' || followingLine[0] == '\t') {
					contentLines = append(contentLines, "")
					p.lineIdx++
					continue
				}
			}
			break
		}
		if nextLine[0] != ' ' && nextLine[0] != '\t' {
			break
		}
		contentLines = append(contentLines, strings.TrimSpace(nextLine))
		p.lineIdx++
	}

	// Render the footnote definition
	// Format: [^id]: styled as footnote marker, then content
	fullContent := strings.Join(contentLines, " ")
	renderedContent := p.renderInline(fullContent)
	wrapped := p.wrapText(renderedContent, p.width-len(footnoteID)-3) // account for "[^id]: "

	marker := p.styles.ansiFootnote.render("[" + footnoteID + "]:")
	indent := spaces(len(footnoteID) + 3)
	first := true
	for l := range strings.SplitSeq(wrapped, "\n") {
		if first {
			p.out.WriteString(marker)
			p.out.WriteByte(' ')
			p.out.WriteString(l)
			p.out.WriteByte('\n')
			first = false
		} else {
			p.out.WriteString(indent)
			p.out.WriteString(l)
			p.out.WriteByte('\n')
		}
	}
	p.out.WriteByte('\n')
	return true
}

// renderListCodeBlock renders a fenced code block within a list context
func (p *parser) renderListCodeBlock(codeIndent, bulletWidth int) {
	// Add spacing before code block
	p.out.WriteString("\n")

	line := p.lines[p.lineIdx]
	ltrimmed := strings.TrimLeft(line, " \t")

	fence := ltrimmed[:3]
	lang := strings.TrimSpace(ltrimmed[3:])
	p.lineIdx++

	// Collect code lines
	var codeLines []string
	for p.lineIdx < len(p.lines) {
		codeLine := p.lines[p.lineIdx]
		codeTrimmed := strings.TrimLeft(codeLine, " \t")
		if strings.HasPrefix(codeTrimmed, fence) {
			p.lineIdx++
			break
		}
		// Remove the list indentation from code lines
		if len(codeLine) >= codeIndent {
			codeLines = append(codeLines, codeLine[codeIndent:])
		} else {
			codeLines = append(codeLines, strings.TrimLeft(codeLine, " \t"))
		}
		p.lineIdx++
	}

	code := strings.Join(codeLines, "\n")
	if code == "" {
		return
	}

	indent := spaces(bulletWidth)
	availableWidth := p.width - bulletWidth
	p.renderCodeBlockWithIndent(code, lang, indent, availableWidth)
}

// renderListBlockquote renders a blockquote within a list context
func (p *parser) renderListBlockquote(bulletWidth int) {
	// Collect all blockquote lines
	var quoteLines []string
	for p.lineIdx < len(p.lines) {
		line := p.lines[p.lineIdx]
		ltrimmed := strings.TrimLeft(line, " \t")

		// Check if this line is part of the blockquote
		if !strings.HasPrefix(ltrimmed, ">") {
			break
		}

		// Remove the > and optional space
		content := strings.TrimPrefix(ltrimmed, ">")
		content = strings.TrimPrefix(content, " ")
		quoteLines = append(quoteLines, content)
		p.lineIdx++
	}

	if len(quoteLines) == 0 {
		return
	}

	// Calculate the indentation for the blockquote (align with list content)
	indent := spaces(bulletWidth)

	// Calculate available width for blockquote content
	availableWidth := p.width - bulletWidth - p.styles.blockquoteIndent
	if availableWidth < 10 {
		availableWidth = 10
	}

	// Use renderBlockquoteContent for full support including nested code blocks
	fullIndent := indent + spaces(p.styles.blockquoteIndent)
	p.renderListBlockquoteContent(quoteLines, fullIndent, availableWidth)
}

// renderListBlockquoteContent renders blockquote content within a list, including code blocks and nested blockquotes
func (p *parser) renderListBlockquoteContent(lines []string, indent string, contentWidth int) {
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check for nested blockquote (line starts with >)
		if strings.HasPrefix(trimmed, ">") {
			// Collect all consecutive nested blockquote lines
			var nestedLines []string
			for i < len(lines) {
				l := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(l, ">") {
					break
				}
				// Strip the > and optional space
				content := strings.TrimPrefix(l, ">")
				content = strings.TrimPrefix(content, " ")
				nestedLines = append(nestedLines, content)
				i++
			}

			// Render the nested blockquote with additional indentation
			nestedIndent := indent + spaces(p.styles.blockquoteIndent)
			nestedWidth := contentWidth - p.styles.blockquoteIndent
			if nestedWidth < 10 {
				nestedWidth = 10 // Minimum content width
			}
			p.renderListBlockquoteContent(nestedLines, nestedIndent, nestedWidth)
			continue
		}

		// Check for fenced code block start
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence := trimmed[:3]
			lang := strings.TrimSpace(trimmed[3:])
			i++

			// Collect code lines until fence end
			var codeLines []string
			for i < len(lines) {
				codeLine := lines[i]
				if strings.HasPrefix(strings.TrimSpace(codeLine), fence) {
					i++
					break
				}
				codeLines = append(codeLines, codeLine)
				i++
			}

			// Render the code block within blockquote context
			code := strings.Join(codeLines, "\n")
			p.renderListBlockquoteCodeBlock(code, lang, indent, contentWidth)
			continue
		}

		// Regular line - render with blockquote-aware inline styling
		rendered := p.renderInlineWithStyle(line, p.styles.ansiBlockquote)
		wrapped := p.wrapText(rendered, contentWidth)
		for wl := range strings.SplitSeq(wrapped, "\n") {
			p.out.WriteString(indent + p.styles.styleBlockquote.Render(wl) + "\n")
		}
		i++
	}
}

// renderListBlockquoteCodeBlock renders a code block within a blockquote within a list
func (p *parser) renderListBlockquoteCodeBlock(code, lang, indent string, availableWidth int) {
	// Add spacing before code block
	p.out.WriteString("\n")

	if code == "" {
		return
	}
	p.renderCodeBlockWithIndent(code, lang, indent, availableWidth)
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
// It uses the document's base text style for restoring after styled elements.
func (p *parser) renderInline(text string) string {
	return p.renderInlineWithStyle(text, p.styles.ansiText)
}

// renderInlineWithWidth renders inline markdown and returns both the rendered string and visual width.
// This avoids a separate width calculation pass for cases like table cells.
func (p *parser) renderInlineWithWidth(text string) (string, int) {
	var out strings.Builder
	out.Grow(len(text) + 64)
	width := p.renderInlineWithStyleTo(&out, text, p.styles.ansiText)
	return out.String(), width
}

// renderInlineWithStyle processes inline markdown with a custom restore style.
// The restoreStyle is applied to plain text after styled elements (code, bold, etc.)
// to maintain proper styling context (e.g., within headings or blockquotes).
func (p *parser) renderInlineWithStyle(text string, restoreStyle ansiStyle) string {
	if text == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(text) + 64)
	p.renderInlineWithStyleTo(&out, text, restoreStyle)
	return out.String()
}

// renderInlineWithStyleTo writes inline markdown to the provided builder and returns the visual width.
// This is the core implementation that avoids intermediate string allocations in recursive calls.
func (p *parser) renderInlineWithStyleTo(out *strings.Builder, text string, restoreStyle ansiStyle) int {
	if text == "" {
		return 0
	}

	// Fast path: check if text contains any markdown characters
	// If not, apply the restore style directly and return
	firstMarker := strings.IndexAny(text, inlineMarkdownChars)
	if firstMarker == -1 {
		restoreStyle.renderTo(out, text)
		return textWidth(text)
	}

	width := 0

	// Optimization: write any leading plain text in one batch
	if firstMarker > 0 {
		plain := text[:firstMarker]
		restoreStyle.renderTo(out, plain)
		width += textWidth(plain)
		text = text[firstMarker:]
	}

	i := 0
	n := len(text)

	for i < n {
		// Check for escaped characters
		if text[i] == '\\' && i+1 < n {
			out.WriteByte(text[i+1])
			width += runewidth.RuneWidth(rune(text[i+1]))
			i += 2
			continue
		}

		// Check for inline code
		if text[i] == '`' {
			end := strings.Index(text[i+1:], "`")
			if end != -1 {
				code := text[i+1 : i+1+end]
				// Use flags to check if parent has formatting attributes that should carry to code
				if restoreStyle.hasStrike || restoreStyle.hasBold {
					// Write code style prefix, then inherited formatting, then code, then suffix
					out.WriteString(p.styles.ansiCode.prefix)
					if restoreStyle.hasBold {
						out.WriteString("\x1b[1m")
					}
					if restoreStyle.hasStrike {
						out.WriteString("\x1b[9m")
					}
					out.WriteString(code)
					out.WriteString(p.styles.ansiCode.suffix)
				} else {
					p.styles.ansiCode.renderTo(out, code)
				}
				// Restore parent style after code (since ansiCode.suffix resets everything)
				out.WriteString(restoreStyle.prefix)
				width += textWidth(code)
				i = i + 1 + end + 1
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
					if restoreStyle.hasBold {
						// Heading (or already-bold) context: bold is redundant, keep italic
						combinedStyle := restoreStyle.withItalic()
						width += p.renderInlineWithStyleTo(out, innerText, combinedStyle)
					} else {
						// Add bold+italic formatting while preserving parent color (e.g., heading)
						combinedStyle := restoreStyle.withBoldItalic()
						width += p.renderInlineWithStyleTo(out, innerText, combinedStyle)
					}
				} else {
					if restoreStyle.hasBold {
						// Bold is redundant in bold contexts (e.g., headings)
						width += p.renderInlineWithStyleTo(out, inner, restoreStyle)
					} else {
						// Add bold formatting while preserving parent color (e.g., heading)
						combinedStyle := restoreStyle.withBold()
						width += p.renderInlineWithStyleTo(out, inner, combinedStyle)
					}
				}
				i = i + 2 + end + 2
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
				// Add italic formatting while preserving parent color (e.g., heading)
				combinedStyle := restoreStyle.withItalic()
				width += p.renderInlineWithStyleTo(out, inner, combinedStyle)
				i = end + 1
				continue
			}
		}

		// Check for strikethrough (~~text~~)
		if i+1 < n && text[i] == '~' && text[i+1] == '~' {
			end := strings.Index(text[i+2:], "~~")
			if end != -1 {
				inner := text[i+2 : i+2+end]
				// Add strikethrough formatting while preserving parent color (e.g., heading)
				combinedStyle := restoreStyle.withStrikethrough()
				width += p.renderInlineWithStyleTo(out, inner, combinedStyle)
				i = i + 2 + end + 2
				continue
			}
		}

		// Check for footnote references [^1] or [^name]
		if text[i] == '[' && i+2 < n && text[i+1] == '^' {
			// Find closing bracket
			closeBracket := strings.Index(text[i:], "]")
			if closeBracket != -1 {
				footnoteRef := text[i : i+closeBracket+1]
				// Validate it looks like a footnote (not empty after ^)
				if closeBracket > 2 {
					p.styles.ansiFootnote.renderTo(out, footnoteRef)
					width += textWidth(footnoteRef)
					i = i + closeBracket + 1
					continue
				}
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
					if linkText != url {
						p.styles.ansiLinkText.renderTo(out, linkText)
						out.WriteByte(' ')
						out.WriteString(p.styles.ansiLink.prefix)
						out.WriteByte('(')
						out.WriteString(url)
						out.WriteByte(')')
						out.WriteString(p.styles.ansiLink.suffix)
						width += textWidth(linkText) + 1 + textWidth(url) + 2 // +1 for space, +2 for parens
					} else {
						p.styles.ansiLink.renderTo(out, linkText)
						width += textWidth(linkText)
					}
					i = i + closeBracket + 2 + closeParen + 1
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
			// Always apply restore style to plain text for consistent coloring
			plainText := text[start:i]
			restoreStyle.renderTo(out, plainText)
			width += textWidth(plainText)
		}
	}

	return width
}

// textWidth calculates the visual width of plain text (no ANSI codes).
// Optimized for ASCII-only strings which are common.
func textWidth(s string) int {
	// Fast path for ASCII-only strings
	isASCII := true
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			isASCII = false
			break
		}
	}
	if isASCII {
		return len(s)
	}
	// Slow path for unicode
	width := 0
	for _, r := range s {
		width += runewidth.RuneWidth(r)
	}
	return width
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

// inlineMarkdownChars contains all characters that trigger inline markdown processing.
const inlineMarkdownChars = "\\`*_~["

// hasInlineMarkdown checks if text contains any markdown formatting characters.
// This allows a fast path to skip processing plain text.
// Uses strings.ContainsAny which is highly optimized in the Go standard library.
func hasInlineMarkdown(text string) bool {
	return strings.ContainsAny(text, inlineMarkdownChars)
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
	p.renderCodeBlockWithIndent(code, lang, "", p.width)
}

// renderCodeBlockWithIndent renders a fenced code block with indentation and width constraints.
func (p *parser) renderCodeBlockWithIndent(code, lang, indent string, availableWidth int) {
	// Get syntax highlighting tokens
	tokens := p.syntaxHighlight(code, lang)

	// Calculate content width with adaptive padding
	// Only apply padding if we have enough width to make it worthwhile
	paddingLeft := 2
	paddingRight := 2
	const minWidthForPadding = 24

	if availableWidth < minWidthForPadding {
		// Disable padding for narrow widths to avoid exceeding available width
		paddingLeft = 0
		paddingRight = 0
	}

	contentWidth := availableWidth - paddingLeft - paddingRight
	if contentWidth < 1 {
		contentWidth = 1 // Minimum content width
	}

	// Pre-compute padding strings (avoids repeated strings.Repeat calls)
	paddingLeftStr := spaces(paddingLeft)
	fullWidthPad := spaces(availableWidth)

	// Use cached background style
	bgStyle := p.styles.ansiCodeBg

	// Render empty line at the top (use sequential writes instead of concat)
	p.out.WriteString(indent)
	bgStyle.renderTo(&p.out, fullWidthPad)
	p.out.WriteByte('\n')

	// Process tokens line by line for better performance
	var lineBuilder strings.Builder
	lineBuilder.Grow(contentWidth + 32)
	lineWidth := 0

	flushLine := func() {
		// Add left padding with background
		p.out.WriteString(indent)
		bgStyle.renderTo(&p.out, paddingLeftStr)
		// Write line content
		p.out.WriteString(lineBuilder.String())
		// Pad to full width (including right padding)
		padWidth := contentWidth - lineWidth + paddingRight
		if padWidth > 0 {
			bgStyle.renderTo(&p.out, spaces(padWidth))
		}
		p.out.WriteByte('\n')
		lineBuilder.Reset()
		lineWidth = 0
	}

	// writeSegmentWrapped prefers breaking at whitespace for code readability.
	// Falls back to character-level breaking only when no whitespace exists.
	writeSegmentWrapped := func(segment string, style ansiStyle) {
		for segment != "" {
			remaining := contentWidth - lineWidth
			if remaining <= 0 {
				flushLine()
				remaining = contentWidth
			}

			// Single pass: track width and last whitespace within remaining
			lastSpacePos := -1
			lastSpaceBytePos := -1
			lastSpaceWidth := 0
			pos := 0
			width := 0
			exceeded := false

			for pos < len(segment) {
				r, size := utf8.DecodeRuneInString(segment[pos:])
				rw := runewidth.RuneWidth(r)

				if width+rw > remaining {
					exceeded = true
					break
				}

				if r == ' ' || r == '\t' {
					lastSpacePos = pos
					lastSpaceBytePos = pos + size
					lastSpaceWidth = width + rw
				}

				width += rw
				pos += size
			}

			if !exceeded {
				style.renderTo(&lineBuilder, segment)
				lineWidth += width
				return
			}

			switch {
			case lastSpacePos >= 0:
				// Found whitespace - break there (preferred)
				part := segment[:lastSpacePos+1] // include the space
				style.renderTo(&lineBuilder, part)
				lineWidth += lastSpaceWidth
				segment = segment[lastSpaceBytePos:]
				flushLine()
			case lineWidth > 0:
				// No whitespace found and we're mid-line - flush and retry with full width
				flushLine()
				// Don't consume segment, let next iteration try with full line width
			case pos > 0:
				// No whitespace, at line start, but we measured some chars that fit
				// Break at character boundary as last resort
				style.renderTo(&lineBuilder, segment[:pos])
				lineWidth += width
				segment = segment[pos:]
				flushLine()
			default:
				// Nothing fits (remaining width too small) - write one char and continue
				r, size := utf8.DecodeRuneInString(segment)
				style.renderTo(&lineBuilder, string(r))
				lineWidth += runewidth.RuneWidth(r)
				segment = segment[size:]
				flushLine()
			}
		}
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
					writeSegmentWrapped(segment, tok.style)
				}
				flushLine()
				start = i + 1
			}
		}
		// Render remaining text
		if start < len(text) {
			segment := text[start:]
			segment = expandTabs(segment, lineWidth)
			writeSegmentWrapped(segment, tok.style)
		}
	}

	// Flush remaining content
	if lineBuilder.Len() > 0 {
		flushLine()
	}

	// Render empty line at the bottom (use pre-computed padding)
	p.out.WriteString(indent)
	bgStyle.renderTo(&p.out, fullWidthPad)
	p.out.WriteByte('\n')

	p.out.WriteByte('\n')
}

// spacesBuffer is a pre-allocated buffer of spaces for padding needs.
// Slicing this is much faster than strings.Repeat for small amounts.
const spacesBuffer = "                                                                                                                                "

// spaces returns a string of n spaces, using the pre-allocated buffer when possible.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(spacesBuffer) {
		return spacesBuffer[:n]
	}
	return strings.Repeat(" ", n)
}

// writeSpaces writes n spaces to the builder without allocations.
func writeSpaces(b *strings.Builder, n int) {
	if n <= 0 {
		return
	}
	if n <= len(spacesBuffer) {
		b.WriteString(spacesBuffer[:n])
		return
	}
	for n > 0 {
		chunk := n
		if chunk > len(spacesBuffer) {
			chunk = len(spacesBuffer)
		}
		b.WriteString(spacesBuffer[:chunk])
		n -= chunk
	}
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
			n := 4 - (width % 4)
			result.WriteString(spaces(n))
			width += n
		} else {
			result.WriteRune(r)
			width += runewidth.RuneWidth(r)
		}
	}
	return result.String()
}

// ansiStringWidth calculates display width while skipping ANSI escape sequences.
func ansiStringWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			// Skip CSI sequences (e.g., \x1b[...m)
			if i+1 < len(s) && s[i+1] == '[' {
				i += 2
				for i < len(s) && (s[i] < '@' || s[i] > '~') {
					i++
				}
				if i < len(s) {
					i++
				}
				continue
			}
			i++
			continue
		}
		if s[i] < utf8.RuneSelf {
			start := i
			for i < len(s) && s[i] < utf8.RuneSelf && s[i] != '\x1b' {
				i++
			}
			width += i - start
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		width += runewidth.RuneWidth(r)
		i += size
	}
	return width
}

// padAllLines pads each line to the target width with trailing spaces.
func padAllLines(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}

	if !strings.Contains(s, "\n") {
		lineWidth := ansiStringWidth(s)
		if lineWidth >= width {
			return s
		}
		var result strings.Builder
		result.Grow(len(s) + width - lineWidth)
		result.WriteString(s)
		writeSpaces(&result, width-lineWidth)
		return result.String()
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

		lineWidth := ansiStringWidth(line)
		if lineWidth < width {
			// Pad with spaces
			writeSpaces(&result, width-lineWidth)
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
// It tracks active ANSI styles and re-applies them on continuation lines.
func (p *parser) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	// Fast path: if the text fits in one line, return as-is
	// This avoids expensive word splitting for short strings
	textVisualWidth := ansiStringWidth(text)
	if textVisualWidth <= width {
		return text
	}

	// Fast path: no spaces means we can only break the word directly
	if !strings.ContainsAny(text, " \t") {
		broken := breakWord(text, width)
		if len(broken) == 1 {
			return broken[0]
		}
		return strings.Join(broken, "\n")
	}

	var result strings.Builder
	result.Grow(len(text) + len(text)/40) // estimate for newlines

	var currentLine strings.Builder
	currentLine.Grow(width + 32) // typical line length + ANSI codes
	currentWidth := 0

	// Track active ANSI sequences that should be re-applied after line breaks
	// We use a single slice and only snapshot when we actually need to wrap
	var activeStyles []string

	words := splitWordsWithStyles(text)
	for i := range words {
		ws := &words[i]
		wordWidth := ws.width

		// Determine if we need to wrap - only then do we need the previous styles
		needsWrap := false
		if wordWidth > width {
			needsWrap = currentLine.Len() > 0
		} else {
			spaceWidth := 0
			if currentWidth > 0 {
				spaceWidth = 1
			}
			needsWrap = currentWidth+spaceWidth+wordWidth > width
		}

		// Only snapshot styles when we actually need to wrap
		var stylesForWrap []string
		if needsWrap && len(activeStyles) > 0 {
			stylesForWrap = make([]string, len(activeStyles))
			copy(stylesForWrap, activeStyles)
		}

		// Update active styles based on ANSI sequences in this word
		activeStyles = updateActiveStyles(activeStyles, ws.ansiCodes)

		if wordWidth > width {
			if currentLine.Len() > 0 {
				// Close any active styles before line break
				if len(stylesForWrap) > 0 {
					currentLine.WriteString("\x1b[m")
				}
				result.WriteString(currentLine.String())
				result.WriteByte('\n')
				currentLine.Reset()
				currentWidth = 0
				// Re-apply styles that were active before this word
				for _, s := range stylesForWrap {
					currentLine.WriteString(s)
				}
			}

			broken := breakWord(ws.word, width)
			for j, part := range broken {
				if j > 0 {
					result.WriteByte('\n')
					// Re-apply styles for continuation within long word
					for _, s := range stylesForWrap {
						result.WriteString(s)
					}
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
			// Close any active styles before line break
			if len(stylesForWrap) > 0 {
				currentLine.WriteString("\x1b[m")
			}
			result.WriteString(currentLine.String())
			result.WriteByte('\n')
			currentLine.Reset()
			currentWidth = 0
			spaceWidth = 0
			// Re-apply styles that were active before this word
			for _, s := range stylesForWrap {
				currentLine.WriteString(s)
			}
		}

		if spaceWidth > 0 {
			currentLine.WriteByte(' ')
			currentWidth++
		}

		currentLine.WriteString(ws.word)
		currentWidth += wordWidth
	}

	if currentLine.Len() > 0 {
		result.WriteString(currentLine.String())
	}

	return result.String()
}

// styledWord represents a word along with any ANSI codes it contains
type styledWord struct {
	word      string   // The full word including ANSI codes (slice of original)
	ansiCodes []string // ANSI sequences found in this word
	width     int      // Precomputed visual width
}

// splitWordsWithStyles splits text into words while tracking ANSI sequences.
// Words are slices of the original string (no copying), and visual width is precomputed.
func splitWordsWithStyles(text string) []styledWord {
	// Count words to pre-allocate
	wordCount := 1
	for i := range len(text) {
		if text[i] == ' ' || text[i] == '\t' {
			wordCount++
		}
	}

	words := make([]styledWord, 0, wordCount)
	wordStart := -1 // Start index of current word (-1 means no word started)
	wordWidth := 0  // Visual width of current word
	var currentAnsi []string
	inAnsi := false
	ansiStart := 0

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			// Start of ANSI sequence
			if wordStart == -1 {
				wordStart = i
			}
			inAnsi = true
			ansiStart = i
			i++
			continue
		}
		if inAnsi {
			if (text[i] >= 'a' && text[i] <= 'z') || (text[i] >= 'A' && text[i] <= 'Z') {
				// End of ANSI sequence - capture it
				currentAnsi = append(currentAnsi, text[ansiStart:i+1])
				inAnsi = false
			}
			i++
			continue
		}

		if text[i] == ' ' || text[i] == '\t' {
			// End of word
			if wordStart >= 0 {
				words = append(words, styledWord{
					word:      text[wordStart:i],
					ansiCodes: currentAnsi,
					width:     wordWidth,
				})
				wordStart = -1
				wordWidth = 0
				currentAnsi = nil
			}
			i++
			continue
		}

		// Regular character - decode and measure
		if wordStart == -1 {
			wordStart = i
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		wordWidth += runewidth.RuneWidth(r)
		i += size
	}

	// Don't forget the last word
	if wordStart >= 0 {
		words = append(words, styledWord{
			word:      text[wordStart:],
			ansiCodes: currentAnsi,
			width:     wordWidth,
		})
	}

	return words
}

// updateActiveStyles updates the list of active ANSI styles based on new codes
func updateActiveStyles(active, newCodes []string) []string {
	for _, code := range newCodes {
		// Check if this is a reset sequence
		if code == "\x1b[m" || code == "\x1b[0m" {
			// Clear all active styles
			active = active[:0]
		} else {
			// Add this style to active list
			active = append(active, code)
		}
	}
	return active
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
