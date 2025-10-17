package markdown

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	ast2 "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	textm "github.com/yuin/goldmark/text"
)

type niceWriter struct {
	width              int
	out                *termenv.Output
	theme              *Theme
	supportsHyperlinks bool

	currentLength int
	emptyLines    int
}

type Style struct {
	Hyperlink     string
	Marker        string
	ListOffsets   []int
	Heading       bool
	Bold          bool
	CodeBlock     bool
	CodeSpan      bool
	Italic        bool
	Strikethrough bool
	Glow          bool
}

type Renderer struct {
	parser             parser.Parser
	theme              *Theme
	supportsHyperlinks bool
}

type Liners interface {
	Lines() *textm.Segments
}

func newRenderer() *Renderer {
	return &Renderer{
		parser: goldmark.New(
			goldmark.WithExtensions(
				extension.Strikethrough,
				extension.Table,
			),
		).Parser(),
		theme:              DarkTheme(),
		supportsHyperlinks: supportsHyperlinks(),
	}
}

func (r *Renderer) Render(source []byte, width int) string {
	if width == 0 {
		return ""
	}

	node := r.parser.Parse(textm.NewReader(source))

	var buf bytes.Buffer
	printer := &niceWriter{
		width:              width,
		out:                termenv.NewOutput(&buf, termenv.WithColorCache(true), termenv.WithProfile(termenv.TrueColor)),
		theme:              r.theme,
		supportsHyperlinks: r.supportsHyperlinks,
	}
	printer.Print(node, source, Style{})
	return buf.String()
}

func (w *niceWriter) Print(node ast.Node, source []byte, style Style) {
	w.print(0, node, source, style)
}

func (w *niceWriter) print(depth int, node ast.Node, source []byte, style Style) {
	// Before (update style, print prefix)
	switch node.Kind().String() {
	case ast.KindEmphasis.String():
		if emphasis, ok := node.(*ast.Emphasis); ok {
			if emphasis.Level == 2 {
				style.Bold = true
			}
			if emphasis.Level == 1 {
				style.Italic = true
			}
		}
	case ast.KindLink.String():
		style.Hyperlink = string(node.(*ast.Link).Destination)
	case ast.KindList.String():
		style.Marker = string(node.(*ast.List).Marker)
		style.ListOffsets = append(style.ListOffsets, 0)
	case ast.KindCodeSpan.String():
		style.CodeSpan = true
	case ast.KindHeading.String():
		style.Heading = true
		w.append(strings.Repeat("#", node.(*ast.Heading).Level)+" ", style)
	case ast.KindListItem.String():
		w.newline(false)
		level := len(style.ListOffsets) - 1
		offset := style.ListOffsets[level] + 1
		style.ListOffsets[level] = offset
		before := "  " + strings.Repeat("    ", level)
		if style.Marker == "." {
			w.append(fmt.Sprintf("%s%d. ", before, offset), style)
		} else {
			w.append(fmt.Sprintf("%s• ", before), style)
		}
	case ast2.KindTableCell.String():
		w.append("| ", style)
	case ast2.KindStrikethrough.String():
		style.Strikethrough = true
	}

	// Print text or handle children
	switch node.Kind() {
	case ast.KindText:
		text := string(node.(*ast.Text).Value(source))
		if text == "" {
			w.newline(false)
		} else {
			w.append(text, style)
		}
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		w.newline(false)
		codeBlock := node.(Liners)
		for i := range codeBlock.Lines().Len() {
			// Ugly: Make sure code blocks is indented correctly
			level := len(style.ListOffsets)
			indent := 4 * level
			if level > 0 && style.Marker == "." {
				offset := style.ListOffsets[level-1] + 1
				indent += len(strconv.Itoa(offset))
			}
			w.append(strings.Repeat(" ", indent), Style{})
			w.append(" ", Style{CodeBlock: true, Glow: true})

			segment := codeBlock.Lines().At(i)
			line := strings.TrimRight(string(segment.Value(source)), "\n")
			w.append("  "+line, Style{CodeBlock: true})

			w.newline(true)
		}
		w.newline(true)
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			w.print(depth+1, child, source, style)
		}
	}

	// After (reset style)
	switch node.Kind().String() {
	case ast.KindHeading.String(), ast.KindParagraph.String(), ast.KindList.String():
		if w.emptyLines == 0 {
			w.newline(false)
			w.newline(true)
			w.emptyLines++
		}
		if node.Kind() == ast.KindList {
			style.ListOffsets = style.ListOffsets[:len(style.ListOffsets)-1]
		}
	case ast.KindListItem.String():
		w.newline(false)
	case ast.KindLink.String():
		if !w.supportsHyperlinks {
			w.append(" ("+style.Hyperlink+")", style)
		}
	case ast2.KindTableHeader.String(), ast2.KindTableRow.String():
		w.append("|", style)
		w.newline(true)
	case ast2.KindTableCell.String():
		w.append(" ", style)
	}
}

func (w *niceWriter) available() int {
	return w.width - w.currentLength
}

func (w *niceWriter) append(text string, style Style) {
	for {
		before, after, found := strings.Cut(text, " ")
		w.appendToken(before, style)
		if !found {
			return
		}

		w.appendWord(" ", style)
		text = after
	}
}

func (w *niceWriter) appendToken(token string, style Style) {
	for {
		available := w.available()
		if len(token) <= available {
			w.appendWord(token, style)
			break
		}
		if available > 0 && len(token) > w.width {
			w.appendWord(token[:available], style)
			token = token[available:]
		}
		w.newline(true)
	}
}

func (w *niceWriter) appendWord(token string, style Style) {
	w.emptyLines = 0
	w.currentLength += len(token)

	s := w.out.String(token).Foreground(w.out.Color(w.theme.Text))
	if style.Hyperlink != "" {
		s = s.Foreground(w.out.Color(w.theme.Hyperlink))
	}
	if style.Heading {
		s = s.Foreground(w.out.Color(w.theme.Heading))
	}
	if style.Bold {
		s = s.Bold()
	}
	if style.CodeBlock {
		s = s.Foreground(w.out.Color(w.theme.CodeBlock))
	}
	if style.CodeSpan {
		s = s.Foreground(w.out.Color(w.theme.CodeSpanForeground)).
			Background(w.out.Color(w.theme.CodeSpanBackground))
	}
	if style.Italic {
		s = s.Italic()
	}
	if style.Strikethrough {
		s = s.CrossOut()
	}
	if style.Glow {
		s = s.Background(w.out.Color(w.theme.Glow))
	}

	hyperlink := style.Hyperlink != "" && w.supportsHyperlinks
	if hyperlink {
		fmt.Fprintf(w.out, "\033]8;;%s\033\\", style.Hyperlink)
	}
	fmt.Fprint(w.out, s)
	if hyperlink {
		fmt.Fprintf(w.out, "\033]8;;\033\\")
	}
}

func (w *niceWriter) newline(force bool) {
	if !force && w.currentLength == 0 {
		return
	}
	fmt.Fprintln(w.out)
	w.currentLength = 0
}
