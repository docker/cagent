package statusbar

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/version"
)

// StatusBar represents the status bar component that displays key bindings help
type StatusBar struct {
	width int
	help  core.KeyMapHelp

	// Cached values to avoid repeated allocations
	cachedHelpText    string
	cachedBindingsLen int
	cachedVersionText string
}

// New creates a new StatusBar instance
func New(help core.KeyMapHelp) StatusBar {
	return StatusBar{
		help: help,
	}
}

// SetWidth sets the width of the status bar
func (s *StatusBar) SetWidth(width int) {
	s.width = width
}

// SetHelp sets the help provider for the status bar
func (s *StatusBar) SetHelp(help core.KeyMapHelp) {
	s.help = help
	// Invalidate cache when help changes
	s.cachedHelpText = ""
	s.cachedBindingsLen = 0
}

// formatHelpString creates a formatted help string from key bindings
func (s *StatusBar) formatHelpString(bindings []key.Binding) string {
	var helpParts []string
	for _, binding := range bindings {
		if binding.Help().Key != "" && binding.Help().Desc != "" {
			keyPart := styles.HighlightWhiteStyle.Render(binding.Help().Key)
			actionPart := styles.SecondaryStyle.Render(binding.Help().Desc)
			helpParts = append(helpParts, keyPart+" "+actionPart)
		}
	}

	// Join with proper spacing between key bindings
	return strings.Join(helpParts, "  ")
}

// InvalidateCache clears all cached values.
// Call this when the theme changes to pick up new colors.
func (s *StatusBar) InvalidateCache() {
	s.cachedHelpText = ""
	s.cachedVersionText = ""
	s.cachedBindingsLen = 0
}

// View renders the status bar
func (s *StatusBar) View() string {
	// Regenerate version text if empty
	if s.cachedVersionText == "" {
		s.cachedVersionText = styles.MutedStyle.Render("cagent " + version.Version)
	}

	var helpText string
	if s.help != nil {
		help := s.help.Help()
		if help != nil {
			shortcuts := help.ShortHelp()
			if len(shortcuts) > 0 {
				// Only regenerate help text if bindings count changed
				if len(shortcuts) != s.cachedBindingsLen || s.cachedHelpText == "" {
					s.cachedHelpText = s.formatHelpString(shortcuts)
					s.cachedBindingsLen = len(shortcuts)
				}
				helpText = s.cachedHelpText
			}
		}
	}

	// If no help text, just show version aligned right
	if helpText == "" {
		return styles.BaseStyle.
			Width(s.width).
			PaddingLeft(1).
			PaddingRight(1).
			Align(lipgloss.Right).
			Render(s.cachedVersionText)
	}

	helpStyled := styles.BaseStyle.PaddingLeft(1).Render(helpText)
	versionStyled := styles.BaseStyle.PaddingRight(1).Render(s.cachedVersionText)

	helpWidth := lipgloss.Width(helpStyled)
	versionWidth := lipgloss.Width(versionStyled)
	availableSpace := s.width - helpWidth - versionWidth

	availableSpace = max(availableSpace, 1)

	spacer := strings.Repeat(" ", availableSpace)

	return helpStyled + spacer + versionStyled
}
