package markdown

import (
	"os"

	"github.com/charmbracelet/glamour/v2"

	"github.com/docker/cagent/pkg/tui/styles"
)

// Renderer is an interface for markdown renderers.
type Renderer interface {
	Render(input string) (string, error)
}

// NewRenderer creates a new fast markdown renderer with the given width.
func NewRenderer(width int) Renderer {
	if os.Getenv("CAGENT_EXPERIMENTAL_MARKDOWN_RENDERER") == "1" {
		return NewFastRenderer(width)
	}
	return NewGlamourRenderer(width)
}

// NewGlamourRenderer creates a markdown renderer using glamour.
// This is kept for compatibility and testing purposes.
func NewGlamourRenderer(width int) *glamour.TermRenderer {
	style := styles.MarkdownStyle()

	r, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStyles(style),
		glamour.WithPreservedNewLines(),
	)
	return r
}
