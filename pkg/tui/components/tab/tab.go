package tab

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/styles"
)

func Render(title, content string, width int) string {
	return RenderWithSuffix(title, "", content, width)
}

// RenderWithSuffix renders a tab with an optional right-aligned suffix on the title line.
func RenderWithSuffix(title, suffix, content string, width int) string {
	styleTitle := styles.TabTitleStyle
	styleBody := styles.TabStyle

	// Build the title line with optional suffix
	var titleLine string
	if suffix != "" {
		// Calculate available space for the dash fill between title and suffix
		titleRendered := styleTitle.PaddingRight(1).Render(title)
		suffixRendered := styles.MutedStyle.PaddingLeft(1).Render(suffix)
		titleWidth := lipgloss.Width(titleRendered)
		suffixWidth := lipgloss.Width(suffixRendered)
		dashWidth := width - titleWidth - suffixWidth
		if dashWidth < 1 {
			dashWidth = 1
		}
		dashes := styleTitle.Render(strings.Repeat("─", dashWidth))
		titleLine = titleRendered + dashes + suffixRendered
	} else {
		titleLine = lipgloss.PlaceHorizontal(width, lipgloss.Left,
			styleTitle.PaddingRight(1).Render(title),
			lipgloss.WithWhitespaceChars("─"),
			lipgloss.WithWhitespaceStyle(styleTitle),
		)
	}

	return styles.NoStyle.PaddingBottom(1).Render(
		lipgloss.JoinVertical(lipgloss.Top,
			titleLine,
			styles.RenderComposite(styleBody.Width(width), content),
		),
	)
}
