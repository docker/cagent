package styles

import (
	"image/color"

	"github.com/charmbracelet/bubbles/v2/textarea"
	"github.com/charmbracelet/lipgloss/v2"
)

// Color scheme - centralized color palette
var (
	Background = lipgloss.Color("#1f1c28")

	// Primary colors
	highlight = lipgloss.Color("#1D63ED") // Docker blue for headers and focus

	// Status colors
	success    = lipgloss.Color("#00FF00") // Green for success/completed
	errorColor = lipgloss.Color("#E06C75") // Red for errors
	warning    = lipgloss.Color("#FFFF00") // Yellow for warnings/confirmation
	active     = lipgloss.Color("#00ff00") // Green for active/working states

	// Text colors
	primary   = lipgloss.Color("#869395") // ~White for primary text
	muted     = lipgloss.Color("#808080") // Grey for muted text
	subtle    = lipgloss.Color("#C0C0C0") // Light grey for subtle text
	secondary = lipgloss.Color("#606060") // Darker grey for secondary text

	// Border colors
	borderPrimary = lipgloss.Color("#FFA500") // Orange for primary borders

	// State colors
	pending    = lipgloss.Color("#FFFFFF") // White for pending states
	inProgress = lipgloss.Color("#FFA500") // Orange for in-progress states
)

func darken(c color.Color, percent float64) color.Color {
	r, g, b, a := c.RGBA()
	factor := 1.0 - percent/100.0
	return color.RGBA{
		R: uint8(float64(r>>8) * factor),
		G: uint8(float64(g>>8) * factor),
		B: uint8(float64(b>>8) * factor),
		A: uint8(a >> 8),
	}
}

var (
	BaseStyle = lipgloss.NewStyle().Foreground(primary)
	AppStyle  = BaseStyle.Padding(0, 1, 0, 1)

	// Text styles
	HighlightStyle    = BaseStyle.Foreground(highlight)
	MutedStyle        = BaseStyle.Foreground(muted)
	ToolCallArgs      = BaseStyle.PaddingLeft(1).BorderLeft(true).BorderStyle(lipgloss.RoundedBorder())
	ToolCallArgKey    = BaseStyle.Bold(true)
	ToolCallResultKey = BaseStyle.Bold(true)
	ToolCallResult    = BaseStyle.PaddingLeft(1).BorderLeft(true).BorderStyle(lipgloss.RoundedBorder())
	SubtleStyle       = BaseStyle.Foreground(subtle)
	SecondaryStyle    = BaseStyle.Foreground(secondary)

	// Status styles
	SuccessStyle    = BaseStyle.Foreground(success)
	ErrorStyle      = BaseStyle.Foreground(errorColor)
	WarningStyle    = BaseStyle.Foreground(warning)
	ActiveStyle     = BaseStyle.Foreground(active)
	InProgressStyle = BaseStyle.Foreground(inProgress)
	PendingStyle    = BaseStyle.Foreground(pending)

	// Layout styles
	HeaderStyle = BaseStyle.Foreground(highlight).Padding(0, 0, 1, 0)
	BorderStyle = BaseStyle.Border(lipgloss.RoundedBorder()).BorderForeground(borderPrimary)

	// Input styles
	InputStyle = textarea.Styles{
		Focused: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(darken(primary, 40)),
		},
		Blurred: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(darken(primary, 40)),
		},
		Cursor: textarea.CursorStyle{
			Color: highlight,
		},
	}
	EditorStyle = BaseStyle.Padding(2, 0, 0, 0)

	// Layout helpers
	CenterStyle = BaseStyle.Align(lipgloss.Center, lipgloss.Center)

	// Deprecated styles (kept for backward compatibility)
	StatusStyle = MutedStyle
	ActionStyle = SecondaryStyle
	ChatStyle   = BaseStyle
)
