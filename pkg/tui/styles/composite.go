package styles

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

var resetPattern = regexp.MustCompile(`\x1b\[0?m`)

// RenderComposite renders the content with the given style, but ensures that
// any ANSI reset codes in the content are replaced with the style's active sequences,
// preventing the style's background/foreground from being interrupted.
func RenderComposite(style lipgloss.Style, content string) string {
	// Get the escape sequence for the style without layout (padding/margin)
	// We render an empty string to get the codes.
	// output is usually: <codes><reset>
	cleanStyle := style.
		UnsetPadding().
		UnsetMargins().
		UnsetWidth().
		UnsetHeight().
		UnsetBold().
		UnsetItalic().
		UnsetUnderline().
		UnsetStrikethrough().
		UnsetReverse().
		UnsetBlink().
		UnsetFaint().
		UnsetBorderStyle().
		UnsetBorderTop().
		UnsetBorderBottom().
		UnsetBorderLeft().
		UnsetBorderRight().
		UnsetBorderForeground().
		UnsetBorderBackground()
	styleSeq := cleanStyle.Render("")

	// Remove the trailing reset code to get just the "start" sequence
	// We assume the last sequence is the reset.
	// lipgloss typically ends with \x1b[0m or \x1b[m
	styleSeq = strings.TrimSuffix(styleSeq, "\x1b[0m")
	styleSeq = strings.TrimSuffix(styleSeq, "\x1b[m")

	// Replace all resets in content with reset + styleSeq
	// We use a regex to match various forms of reset
	modifiedContent := resetPattern.ReplaceAllString(content, "\x1b[0m"+styleSeq)

	// Render the modified content with the original style (to keep padding/layout)
	return style.Render(modifiedContent)
}
