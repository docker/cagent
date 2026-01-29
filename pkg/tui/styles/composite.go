package styles

import (
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
)

// ANSI reset sequences we need to handle
const (
	resetFull  = "\x1b[0m"
	resetShort = "\x1b[m"
)

// styleSeqCache caches the style sequence for common styles.
// The cache maps a style's string representation to its escape sequence.
var (
	styleSeqCache   = make(map[string]string)
	styleSeqCacheMu sync.RWMutex
)

// clearStyleSeqCache clears the style sequence cache.
// Called when the theme changes to ensure styles are re-computed with new colors.
func clearStyleSeqCache() {
	styleSeqCacheMu.Lock()
	styleSeqCache = make(map[string]string)
	styleSeqCacheMu.Unlock()
}

// getStyleSeq returns the ANSI escape sequence for a style's colors only.
// Results are cached for repeated calls with the same style.
func getStyleSeq(style lipgloss.Style) string {
	// Use the style's rendered empty string as cache key
	// This is a simple way to identify the style
	cacheKey := style.Render("")

	styleSeqCacheMu.RLock()
	if seq, ok := styleSeqCache[cacheKey]; ok {
		styleSeqCacheMu.RUnlock()
		return seq
	}
	styleSeqCacheMu.RUnlock()

	// Compute the style sequence
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
	styleSeq = strings.TrimSuffix(styleSeq, resetFull)
	styleSeq = strings.TrimSuffix(styleSeq, resetShort)

	styleSeqCacheMu.Lock()
	styleSeqCache[cacheKey] = styleSeq
	styleSeqCacheMu.Unlock()

	return styleSeq
}

// RenderComposite renders the content with the given style, but ensures that
// any ANSI reset codes in the content are replaced with the style's active sequences,
// preventing the style's background/foreground from being interrupted.
func RenderComposite(style lipgloss.Style, content string) string {
	// Fast path: if content has no reset sequences, just render normally
	if !strings.Contains(content, "\x1b[") {
		return style.Render(content)
	}

	// Get the cached style sequence
	styleSeq := getStyleSeq(style)

	// Replace reset sequences with reset + styleSeq
	// Handle both \x1b[0m and \x1b[m forms without regex
	modifiedContent := strings.ReplaceAll(content, resetFull, resetFull+styleSeq)
	modifiedContent = strings.ReplaceAll(modifiedContent, resetShort, resetFull+styleSeq)

	// Render the modified content with the original style (to keep padding/layout)
	return style.Render(modifiedContent)
}
