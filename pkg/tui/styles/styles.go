package styles

import (
	"github.com/charmbracelet/bubbles/v2/textarea"
	"github.com/charmbracelet/lipgloss/v2"
)

// Tokyo Night-inspired Color Palette
var (
	// Background colors
	Background    = lipgloss.Color("#1a1b26") // Dark blue-black
	BackgroundAlt = lipgloss.Color("#24283b") // Slightly lighter background

	// Primary accent colors
	Accent    = lipgloss.Color("#7aa2f7") // Soft blue
	AccentDim = lipgloss.Color("#565f89") // Dimmed blue

	// Status colors - softer, more professional
	Success = lipgloss.Color("#9ece6a") // Soft green
	Error   = lipgloss.Color("#f7768e") // Soft red
	Warning = lipgloss.Color("#e0af68") // Soft yellow
	Info    = lipgloss.Color("#7dcfff") // Soft cyan

	// Text hierarchy
	TextPrimary   = lipgloss.Color("#c0caf5") // Light blue-white
	TextSecondary = lipgloss.Color("#9aa5ce") // Medium blue-grey
	TextMuted     = lipgloss.Color("#565f89") // Dark blue-grey
	TextSubtle    = lipgloss.Color("#414868") // Very dark blue-grey

	// Border colors
	BorderPrimary   = lipgloss.Color("#7aa2f7") // Soft blue
	BorderSecondary = lipgloss.Color("#414868") // Dark blue-grey
	BorderMuted     = lipgloss.Color("#24283b") // Very dark blue
	BorderWarning   = lipgloss.Color("#e0af68") // Soft yellow for warnings
	BorderError     = lipgloss.Color("#f7768e") // Soft red for errors

	// Diff colors (matching glamour/markdown "dark" theme)
	DiffAddBg    = lipgloss.Color("#20303b") // Dark blue-green
	DiffRemoveBg = lipgloss.Color("#3c2a2a") // Dark red-brown
	DiffAddFg    = lipgloss.Color("#9ece6a") // Soft green
	DiffRemoveFg = lipgloss.Color("#f7768e") // Soft red

	// Interactive element colors
	Selected         = lipgloss.Color("#364a82") // Dark blue for selected items
	SelectedFg       = lipgloss.Color("#c0caf5") // Light text on selected
	Hover            = lipgloss.Color("#2d3f5f") // Slightly lighter than selected
	PlaceholderColor = lipgloss.Color("#565f89") // Muted for placeholders
)

// Base Styles
var (
	BaseStyle = lipgloss.NewStyle().Foreground(TextPrimary)
	AppStyle  = BaseStyle.Padding(0, 1, 0, 1)
)

// Text Styles
var (
	HighlightStyle = BaseStyle.Foreground(Accent)
	MutedStyle     = BaseStyle.Foreground(TextMuted)
	SubtleStyle    = BaseStyle.Foreground(TextSubtle)
	SecondaryStyle = BaseStyle.Foreground(TextSecondary)
	BoldStyle      = BaseStyle.Bold(true)
	ItalicStyle    = BaseStyle.Italic(true)
)

// Status Styles
var (
	SuccessStyle    = BaseStyle.Foreground(Success)
	ErrorStyle      = BaseStyle.Foreground(Error)
	WarningStyle    = BaseStyle.Foreground(Warning)
	InfoStyle       = BaseStyle.Foreground(Info)
	ActiveStyle     = BaseStyle.Foreground(Success)
	InProgressStyle = BaseStyle.Foreground(Warning)
	PendingStyle    = BaseStyle.Foreground(TextSecondary)
)

// Layout Styles
var (
	HeaderStyle        = BaseStyle.Foreground(Accent).Padding(0, 0, 1, 0)
	PaddedContentStyle = BaseStyle.Padding(1, 2)
	CenterStyle        = BaseStyle.Align(lipgloss.Center, lipgloss.Center)
)

// Border Styles
var (
	BorderStyle = BaseStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderPrimary)

	BorderedBoxStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderSecondary).
				Padding(0, 1)

	BorderedBoxFocusedStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderPrimary).
				Padding(0, 1)

	UserMessageBorderStyle = BaseStyle.
				PaddingLeft(1).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(BorderPrimary)
)

// Dialog Styles
var (
	DialogStyle = BaseStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderSecondary).
			Foreground(TextPrimary).
			Padding(1, 2).
			Align(lipgloss.Left)

	DialogWarningStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderWarning).
				Foreground(TextPrimary).
				Padding(1, 2).
				Align(lipgloss.Left)

	DialogTitleStyle = BaseStyle.
				Bold(true).
				Foreground(TextSecondary).
				Align(lipgloss.Center)

	DialogTitleWarningStyle = BaseStyle.
				Bold(true).
				Foreground(Warning).
				Align(lipgloss.Center)

	DialogTitleInfoStyle = BaseStyle.
				Bold(true).
				Foreground(Info).
				Align(lipgloss.Center)

	DialogContentStyle = BaseStyle.
				Foreground(TextPrimary)

	DialogSeparatorStyle = BaseStyle.
				Foreground(BorderMuted)

	DialogLabelStyle = BaseStyle.
				Bold(true).
				Foreground(TextMuted)

	DialogValueStyle = BaseStyle.
				Bold(true).
				Foreground(TextSecondary)

	DialogQuestionStyle = BaseStyle.
				Bold(true).
				Foreground(TextPrimary).
				Align(lipgloss.Center)

	DialogOptionsStyle = BaseStyle.
				Foreground(TextMuted).
				Align(lipgloss.Center)

	DialogHelpStyle = BaseStyle.
			Foreground(TextMuted).
			Italic(true)
)

// Command Palette Styles
var (
	PaletteSelectedStyle = BaseStyle.
				Background(Selected).
				Foreground(SelectedFg).
				Padding(0, 1)

	PaletteUnselectedStyle = BaseStyle.
				Foreground(TextPrimary).
				Padding(0, 1)

	PaletteCategoryStyle = BaseStyle.
				Bold(true).
				Foreground(TextMuted).
				MarginTop(1)

	PaletteDescStyle = BaseStyle.
				Foreground(TextMuted)
)

// Diff Styles (matching glamour markdown theme)
var (
	DiffAddStyle = BaseStyle.
			Background(DiffAddBg).
			Foreground(DiffAddFg)

	DiffRemoveStyle = BaseStyle.
			Background(DiffRemoveBg).
			Foreground(DiffRemoveFg)

	DiffUnchangedStyle = lipgloss.NewStyle()

	DiffContextStyle = BaseStyle
)

// Tool Call Styles
var (
	ToolCallArgs = BaseStyle.
			PaddingLeft(1).
			BorderLeft(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderSecondary)

	ToolCallArgKey = BaseStyle.Bold(true).Foreground(TextSecondary)

	ToolCallResult = BaseStyle.
			PaddingLeft(1).
			BorderLeft(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderSecondary)

	ToolCallResultKey = BaseStyle.Bold(true).Foreground(TextSecondary)
)

// Input Styles
var (
	InputStyle = textarea.Styles{
		Focused: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Blurred: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Cursor: textarea.CursorStyle{
			Color: Accent,
		},
	}
	EditorStyle = BaseStyle.Padding(2, 0, 0, 0)
)

// Notification Styles
var (
	NotificationStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Success).
				Padding(0, 1)

	NotificationInfoStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Info).
				Padding(0, 1)
)

// Deprecated styles (kept for backward compatibility)
var (
	StatusStyle = MutedStyle
	ActionStyle = SecondaryStyle
	ChatStyle   = BaseStyle
)

// Selection Styles
var (
	SelectionStyle = BaseStyle.
		Background(Selected).
		Foreground(SelectedFg)
)
