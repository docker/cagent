package statusbar

import (
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/docker/cagent/internal/tui/core"
	"github.com/docker/cagent/internal/tui/styles"
	"github.com/docker/cagent/internal/version"
)

// StatusBar represents the status bar component that displays key bindings help
type StatusBar struct {
	width int
	help  core.KeyMapHelp
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

// formatHelpString creates a formatted help string from key bindings
func (s *StatusBar) formatHelpString(bindings []key.Binding) string {
	var helpParts []string
	for _, binding := range bindings {
		if binding.Help().Key != "" && binding.Help().Desc != "" {
			keyPart := styles.StatusStyle.Render(binding.Help().Key)
			actionPart := styles.ActionStyle.Render(binding.Help().Desc)
			helpParts = append(helpParts, keyPart+" "+actionPart)
		}
	}

	if len(helpParts) == 0 {
		return ""
	}

	// Join with proper spacing between key bindings
	return strings.Join(helpParts, "  ")
}

// View renders the status bar
func (s *StatusBar) View() string {
	versionText := styles.MutedStyle.Render(version.Version)

	var helpText string
	if s.help != nil {
		help := s.help.Help()
		if help != nil {
			shortcuts := help.ShortHelp()
			if len(shortcuts) > 0 {
				helpText = s.formatHelpString(shortcuts)
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
			Render(versionText)
	}

	helpStyled := styles.BaseStyle.PaddingLeft(1).Render(helpText)
	versionStyled := styles.BaseStyle.PaddingRight(1).Render(versionText)

	helpWidth := lipgloss.Width(helpStyled)
	versionWidth := lipgloss.Width(versionStyled)
	availableSpace := s.width - helpWidth - versionWidth

	if availableSpace < 0 {
		availableSpace = 1
	}

	spacer := strings.Repeat(" ", availableSpace)

	return helpStyled + spacer + versionStyled
}
