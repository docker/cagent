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

// NewRenderer creates a new markdown renderer with the given width.
// The fast renderer is used when either the experimental markdown renderer
// or the experimental concurrent agents feature is enabled — the concurrent
// agents TUI bundles the fast renderer by default.
func NewRenderer(width int) Renderer {
	if os.Getenv("CAGENT_EXPERIMENTAL_MARKDOWN_RENDERER") == "1" ||
		os.Getenv("CAGENT_EXPERIMENTAL_CONCURRENT_AGENTS") == "1" {
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
	)
	return r
}
