package tabbar

import (
	"fmt"
	"image/color"
	"math"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	// defaultMaxTitleLen is the default maximum display length for a tab title
	// when no user-configured value is provided.
	defaultMaxTitleLen = 20
	// defaultTabTitle is used when a tab has no title set.
	defaultTabTitle = "New Session"
	// closeButtonText is the close button content rendered inside each tab.
	closeButtonText = " ×"
	// accentBar is the leading vertical bar that indicates focus state.
	accentBar = "▎"
	// runningIndicator is shown before the title when the tab's session is streaming.
	// Cycles through braille spinner frames when animated.
	// attentionIndicator is shown before the title when the tab needs attention,
	// replacing the running indicator to signal that user action is required.
	attentionIndicator = "! "
	// mutedContrastStrength controls how much the muted foreground shifts
	// away from the background (0.0 = invisible, 1.0 = full black/white).
	mutedContrastStrength = 0.45
	// minIndicatorContrast is the minimum WCAG contrast ratio required for
	// semantic indicator colors (running dot, attention bang). If the themed
	// color doesn't meet this threshold against the tab background, it is
	// automatically boosted while preserving its hue.
	minIndicatorContrast = 4.5
	// maxBoostSteps limits the blend iterations in ensureContrast to avoid
	// infinite loops on degenerate inputs.
	maxBoostSteps = 20
)

// runningFrames are the braille spinner characters used to animate the
// running indicator in a streaming tab.
var runningFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Tab represents a single rendered tab in the tab bar.
type Tab struct {
	view        string
	mainZoneEnd int // X offset where close zone begins (relative to tab start)
	width       int
}

// View returns the rendered tab content.
func (t Tab) View() string { return t.view }

// Width returns the total rendered width of the tab.
func (t Tab) Width() int { return t.width }

// MainZoneEnd returns the X offset where the main click area ends
// and the close-button click area begins.
func (t Tab) MainZoneEnd() int { return t.mainZoneEnd }

// --- Color helpers ---

// sRGBLuminance returns the relative luminance of an sRGB color using the
// WCAG 2.x formula (linearized channel values, ITU-R BT.709 coefficients).
func sRGBLuminance(r, g, b float64) float64 {
	linearize := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

// colorToLinear extracts normalized [0,1] sRGB components from a color.Color.
func colorToLinear(c color.Color) (float64, float64, float64) {
	r, g, b, _ := c.RGBA()
	return float64(r) / 65535, float64(g) / 65535, float64(b) / 65535
}

// contrastRatio returns the WCAG 2.x contrast ratio between two colors.
func contrastRatio(fg, bg color.Color) float64 {
	r1, g1, b1 := colorToLinear(fg)
	r2, g2, b2 := colorToLinear(bg)
	l1 := sRGBLuminance(r1, g1, b1)
	l2 := sRGBLuminance(r2, g2, b2)
	lighter := max(l1, l2)
	darker := min(l1, l2)
	return (lighter + 0.05) / (darker + 0.05)
}

// toHexColor formats normalized [0,1] RGB components as a lipgloss color.
func toHexColor(r, g, b float64) color.Color {
	clamp := func(v float64) int {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 255
		}
		return int(v * 255)
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", clamp(r), clamp(g), clamp(b)))
}

// mutedContrastFg returns a foreground color that is visible but subtle against
// the given background. It blends the background toward white (for dark bg) or
// black (for light bg) by mutedContrastStrength.
func mutedContrastFg(bg color.Color) color.Color {
	rf, gf, bf := colorToLinear(bg)

	// Perceived luminance for direction decision (BT.601 for perceptual balance).
	lum := 0.299*rf + 0.587*gf + 0.114*bf

	var tgt float64
	if lum > 0.5 {
		tgt = 0.0
	} else {
		tgt = 1.0
	}

	s := mutedContrastStrength
	return toHexColor(rf+(tgt-rf)*s, gf+(tgt-gf)*s, bf+(tgt-bf)*s)
}

// ensureContrast returns fg unchanged if it already meets minIndicatorContrast
// against bg. Otherwise it progressively blends fg toward white (on dark bg) or
// black (on light bg) until the threshold is met, preserving the original hue.
func ensureContrast(fg, bg color.Color) color.Color {
	if contrastRatio(fg, bg) >= minIndicatorContrast {
		return fg
	}

	rf, gf, bf := colorToLinear(fg)
	bgR, bgG, bgB := colorToLinear(bg)
	bgLum := sRGBLuminance(bgR, bgG, bgB)

	// Blend toward white on dark backgrounds, toward black on light ones.
	var tR, tG, tB float64
	if bgLum > 0.5 {
		tR, tG, tB = 0, 0, 0
	} else {
		tR, tG, tB = 1, 1, 1
	}

	for step := 1; step <= maxBoostSteps; step++ {
		t := float64(step) / float64(maxBoostSteps)
		nr := rf + (tR-rf)*t
		ng := gf + (tG-gf)*t
		nb := bf + (tB-bf)*t
		candidate := toHexColor(nr, ng, nb)
		if contrastRatio(candidate, bg) >= minIndicatorContrast {
			return candidate
		}
	}

	// Fallback: full contrast direction (should always meet the threshold).
	return toHexColor(tR, tG, tB)
}

// renderTab creates a Tab: ▎ title ×
// The accent bar is bright for the focused tab and dim for inactive tabs.
// The close button color is computed dynamically for optimal contrast against
// the tab's background, so it works in any theme without manual tuning.
// Indicator colors (running, attention) preserve their semantic hue but are
// automatically boosted when contrast against the tab background is too low.
func renderTab(info messages.TabInfo, maxTitleLen, animFrame int) Tab {
	title := info.Title
	if title == "" {
		title = defaultTabTitle
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	// Pick colors based on focus state.
	var bgColor, fgColor, barColor color.Color
	if info.IsActive {
		bgColor = styles.TabActiveBg
		fgColor = styles.TabActiveFg
		barColor = styles.TabBorder // bright accent for focused tab
	} else {
		bgColor = styles.TabBg
		fgColor = styles.TabInactiveFg
		barColor = styles.TabInactiveFg // dim accent for inactive tab
	}

	// Close button color derived from this tab's background.
	closeFg := mutedContrastFg(bgColor)

	// Helper: every segment gets the same background.
	pad := lipgloss.NewStyle().Background(bgColor)

	// Leading accent bar.
	bar := lipgloss.NewStyle().Foreground(barColor).Background(bgColor).Render(accentBar)

	titleSt := lipgloss.NewStyle().Foreground(fgColor).Background(bgColor)
	if info.IsActive {
		titleSt = titleSt.Bold(true)
	}

	content := bar
	switch {
	case info.NeedsAttention:
		// Attention takes priority over running: replace the streaming dot
		// with a warning-colored indicator so it's obvious the tab needs action.
		attnFg := ensureContrast(styles.Warning, bgColor)
		content += lipgloss.NewStyle().Foreground(attnFg).Background(bgColor).Bold(true).Render(attentionIndicator)
	case info.IsRunning && !info.IsActive:
		runFg := ensureContrast(styles.TabAccentFg, bgColor)
		frame := runningFrames[animFrame%len(runningFrames)]
		content += lipgloss.NewStyle().Foreground(runFg).Background(bgColor).Render(frame + " ")
	default:
		content += pad.Render(" ")
	}
	content += titleSt.Render(title)

	mainEnd := lipgloss.Width(content)

	closeBtn := lipgloss.NewStyle().Foreground(closeFg).Background(bgColor).Render(closeButtonText)
	content += closeBtn + pad.Render(" ")

	width := lipgloss.Width(content)

	return Tab{
		view:        content,
		mainZoneEnd: mainEnd,
		width:       width,
	}
}
