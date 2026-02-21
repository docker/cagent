package styles

import (
	"image/color"
	"sync"

	"charm.land/lipgloss/v2"
)

// AgentBadgeColors holds the resolved foreground and background colors for an agent badge.
type AgentBadgeColors struct {
	Fg color.Color
	Bg color.Color
}

// cachedBadgeStyle holds a precomputed badge style for a palette index.
type cachedBadgeStyle struct {
	colors AgentBadgeColors
	style  lipgloss.Style
}

// agentRegistry maps agent names to their index in the team list and holds
// precomputed styles for each palette entry.
var agentRegistry struct {
	sync.RWMutex
	indices      map[string]int
	badgeStyles  []cachedBadgeStyle
	accentStyles []lipgloss.Style
}

// SetAgentOrder updates the agent name → index mapping and rebuilds the style cache.
// Call this when the team info changes (e.g., on TeamInfoEvent).
func SetAgentOrder(agentNames []string) {
	agentRegistry.Lock()
	defer agentRegistry.Unlock()

	agentRegistry.indices = make(map[string]int, len(agentNames))
	for i, name := range agentNames {
		agentRegistry.indices[name] = i
	}

	rebuildAgentColorCache()
}

// rebuildAgentColorCache precomputes badge and accent styles from the current theme's hues.
// Must be called with agentRegistry.Lock held.
func rebuildAgentColorCache() {
	theme := CurrentTheme()

	hues := theme.Colors.AgentHues
	if len(hues) == 0 {
		hues = DefaultAgentHues
	}

	bg := lipgloss.Color(theme.Colors.Background)
	badgeColors := GenerateBadgePalette(hues, bg)
	accentColors := GenerateAccentPalette(hues, bg)

	agentRegistry.badgeStyles = make([]cachedBadgeStyle, len(badgeColors))
	for i, bgColor := range badgeColors {
		r, g, b := ColorToRGB(bgColor)
		bgHex := RGBToHex(r, g, b)
		fgHex := BestForegroundHex(
			bgHex,
			theme.Colors.TextBright,
			theme.Colors.Background,
			"#000000",
			"#ffffff",
		)
		colors := AgentBadgeColors{
			Fg: lipgloss.Color(fgHex),
			Bg: bgColor,
		}
		agentRegistry.badgeStyles[i] = cachedBadgeStyle{
			colors: colors,
			style: BaseStyle.
				Foreground(colors.Fg).
				Background(colors.Bg).
				Padding(0, 1),
		}
	}

	agentRegistry.accentStyles = make([]lipgloss.Style, len(accentColors))
	for i, c := range accentColors {
		agentRegistry.accentStyles[i] = BaseStyle.Foreground(c)
	}
}

// InvalidateAgentColorCache rebuilds the cached agent styles.
// Call this after a theme change so colors are recalculated against the new background.
func InvalidateAgentColorCache() {
	agentRegistry.Lock()
	defer agentRegistry.Unlock()

	rebuildAgentColorCache()
}

// AgentBadgeColorsFor returns the badge foreground/background colors for a given agent name.
func AgentBadgeColorsFor(agentName string) AgentBadgeColors {
	agentRegistry.RLock()
	defer agentRegistry.RUnlock()

	idx, ok := agentRegistry.indices[agentName]
	if !ok {
		return AgentBadgeColors{
			Fg: lipgloss.Color("#ffffff"),
			Bg: lipgloss.Color("#1D63ED"),
		}
	}

	size := len(agentRegistry.badgeStyles)
	if size > 0 {
		return agentRegistry.badgeStyles[idx%size].colors
	}

	return AgentBadgeColors{
		Fg: lipgloss.Color("#ffffff"),
		Bg: lipgloss.Color("#1D63ED"),
	}
}

// AgentBadgeStyleFor returns a lipgloss badge style colored for the given agent.
func AgentBadgeStyleFor(agentName string) lipgloss.Style {
	agentRegistry.RLock()
	defer agentRegistry.RUnlock()

	idx, ok := agentRegistry.indices[agentName]
	if !ok {
		return BaseStyle.
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#1D63ED")).
			Padding(0, 1)
	}

	size := len(agentRegistry.badgeStyles)
	if size > 0 {
		return agentRegistry.badgeStyles[idx%size].style
	}

	return BaseStyle.
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1D63ED")).
		Padding(0, 1)
}

// AgentAccentStyleFor returns a foreground-only style for agent names (used in sidebar).
func AgentAccentStyleFor(agentName string) lipgloss.Style {
	agentRegistry.RLock()
	defer agentRegistry.RUnlock()

	idx, ok := agentRegistry.indices[agentName]
	if !ok {
		return BaseStyle.Foreground(lipgloss.Color("#98C379"))
	}

	size := len(agentRegistry.accentStyles)
	if size > 0 {
		return agentRegistry.accentStyles[idx%size]
	}

	return BaseStyle.Foreground(lipgloss.Color("#98C379"))
}
