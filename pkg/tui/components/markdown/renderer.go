package markdown

import (
	"github.com/charmbracelet/glamour/v2"
	allstyles "github.com/charmbracelet/glamour/v2/styles"
)

func uintPtr(u uint) *uint { return &u }

func NewRenderer(width int) *glamour.TermRenderer {
	customDarkStyle := *allstyles.DefaultStyles["dark"]

	customDarkStyle.Document.Margin = uintPtr(0)
	customDarkStyle.Document.BlockPrefix = ""
	customDarkStyle.Document.BlockSuffix = ""

	// The default indent token is buggy. It breaks line splitting.
	customDarkStyle.BlockQuote.IndentToken = nil

	r, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(min(width, 120)),
		glamour.WithStyles(customDarkStyle),
	)
	return r
}
