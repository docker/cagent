package styles

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/glamour/v2/ansi"
)

const (
	defaultListIndent = 2
	defaultMargin     = 2
)

// Color hex values (used throughout the file)
const (
	// Primary colors
	ColorAccentBlue      = "#7AA2F7" // Soft blue
	ColorMutedBlue       = "#8B95C1" // Dark blue-grey
	ColorBackgroundAlt   = "#24283B" // Slightly lighter background
	ColorBorderSecondary = "#6B75A8" // Dark blue-grey
	ColorTextPrimary     = "#C0CAF5" // Light blue-white
	ColorTextSecondary   = "#9AA5CE" // Medium blue-grey
	ColorSuccessGreen    = "#9ECE6A" // Soft green
	ColorErrorRed        = "#F7768E" // Soft red
	ColorWarningYellow   = "#E0AF68" // Soft yellow

	// Spinner glow colors (transition from base blue towards white)
	ColorSpinnerDim       = "#9AB8F9" // Lighter blue
	ColorSpinnerBright    = "#B8CFFB" // Much lighter blue
	ColorSpinnerBrightest = "#D6E5FC" // Very light blue, near white

	// Background colors
	ColorBackground = "#1A1B26" // Dark blue-black

	// Status colors
	ColorInfoCyan = "#7DCFFF" // Soft cyan

	// Badge colors
	ColorAgentBadge    = "#BB9AF7" // Soft purple
	ColorTransferBadge = "#7DCFFF" // Soft cyan

	// Diff colors
	ColorDiffAddBg    = "#20303B" // Dark blue-green
	ColorDiffRemoveBg = "#3C2A2A" // Dark red-brown

	// Line number and UI element colors
	ColorLineNumber = "#565F89" // Muted blue-grey (same as ColorMutedBlue)
	ColorSeparator  = "#414868" // Dark blue-grey (same as ColorBorderSecondary)

	// Interactive element colors
	ColorSelected = "#364A82" // Dark blue for selected items
	ColorHover    = "#2D3F5F" // Slightly lighter than selected

	// AutoCompleteGhost colors
	ColorSuggestionGhost = "#6B6B6B"
)

// Chroma syntax highlighting colors (Monokai theme)
const (
	ChromaErrorFgColor             = "#F1F1F1"
	ChromaSuccessColor             = "#00D787"
	ChromaErrorBgColor             = "#F05B5B"
	ChromaCommentColor             = "#676767"
	ChromaCommentPreprocColor      = "#FF875F"
	ChromaKeywordColor             = "#00AAFF"
	ChromaKeywordReservedColor     = "#FF5FD2"
	ChromaKeywordNamespaceColor    = "#FF5F87"
	ChromaKeywordTypeColor         = "#6E6ED8"
	ChromaOperatorColor            = "#EF8080"
	ChromaPunctuationColor         = "#E8E8A8"
	ChromaNameBuiltinColor         = "#FF8EC7"
	ChromaNameTagColor             = "#B083EA"
	ChromaNameAttributeColor       = "#7A7AE6"
	ChromaNameDecoratorColor       = "#FFFF87"
	ChromaLiteralNumberColor       = "#6EEFC0"
	ChromaLiteralStringColor       = "#C69669"
	ChromaLiteralStringEscapeColor = "#AFFFD7"
	ChromaGenericDeletedColor      = "#FD5B5B"
	ChromaGenericSubheadingColor   = "#777777"
	ChromaBackgroundColor          = "#373737"
)

// ANSI color codes (8-bit color codes)
const (
	ANSIColor252 = "252"
	ANSIColor39  = "39"
	ANSIColor63  = "63"
	ANSIColor35  = "35"
	ANSIColor212 = "212"
	ANSIColor243 = "243"
	ANSIColor244 = "244"
)

// Tokyo Night-inspired Color Palette
var (
	// Background colors
	Background    = lipgloss.Color(ColorBackground)
	BackgroundAlt = lipgloss.Color(ColorBackgroundAlt)

	// Primary accent colors
	Accent    = lipgloss.Color(ColorAccentBlue)
	AccentDim = lipgloss.Color(ColorMutedBlue)

	// Status colors - softer, more professional
	Success = lipgloss.Color(ColorSuccessGreen)
	Error   = lipgloss.Color(ColorErrorRed)
	Warning = lipgloss.Color(ColorWarningYellow)
	Info    = lipgloss.Color(ColorInfoCyan)

	// Text hierarchy
	TextPrimary   = lipgloss.Color(ColorTextPrimary)
	TextSecondary = lipgloss.Color(ColorTextSecondary)
	TextMuted     = lipgloss.Color(ColorMutedBlue)
	TextSubtle    = lipgloss.Color(ColorBorderSecondary)

	// Border colors
	BorderPrimary   = lipgloss.Color(ColorAccentBlue)
	BorderSecondary = lipgloss.Color(ColorBorderSecondary)
	BorderMuted     = lipgloss.Color(ColorBackgroundAlt)
	BorderWarning   = lipgloss.Color(ColorWarningYellow)
	BorderError     = lipgloss.Color(ColorErrorRed)

	// Diff colors (matching glamour/markdown "dark" theme)
	DiffAddBg    = lipgloss.Color(ColorDiffAddBg)
	DiffRemoveBg = lipgloss.Color(ColorDiffRemoveBg)
	DiffAddFg    = lipgloss.Color(ColorSuccessGreen)
	DiffRemoveFg = lipgloss.Color(ColorErrorRed)

	// UI element colors
	LineNumber = lipgloss.Color(ColorLineNumber)
	Separator  = lipgloss.Color(ColorSeparator)

	// Interactive element colors
	Selected         = lipgloss.Color(ColorSelected)
	SelectedFg       = lipgloss.Color(ColorTextPrimary)
	Hover            = lipgloss.Color(ColorHover)
	PlaceholderColor = lipgloss.Color(ColorMutedBlue)

	// Badge colors
	AgentBadge    = lipgloss.Color(ColorAgentBadge)
	TransferBadge = lipgloss.Color(ColorTransferBadge)
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
	CenterStyle = BaseStyle.Align(lipgloss.Center, lipgloss.Center)
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
				Padding(1, 2).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(BorderPrimary).
				Bold(true).
				Background(BackgroundAlt)

	WelcomeMessageBorderStyle = BaseStyle.
					Padding(1, 2).
					BorderLeft(true).
					BorderStyle(lipgloss.DoubleBorder()).
					Bold(true)

	ErrorMessageStyle = ErrorStyle.
				Padding(0, 2).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder())
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

	DiffUnchangedStyle = BaseStyle.Background(BackgroundAlt)

	DiffContextStyle = BaseStyle
)

// Syntax highlighting UI element styles
var (
	LineNumberStyle = BaseStyle.Foreground(LineNumber).Background(BackgroundAlt)
	SeparatorStyle  = BaseStyle.Foreground(Separator).Background(BackgroundAlt)
)

// Tool Call Styles
var (
	ToolMessageStyle = BaseStyle.
				Padding(1).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(BorderSecondary).
				Background(BackgroundAlt)

	ToolCallArgs = BaseStyle.
			BorderLeft(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderSecondary)

	ToolCallArgKey = BaseStyle.Bold(true).Foreground(TextSecondary)

	ToolCallResult = BaseStyle.
			BorderLeft(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderSecondary)

	ToolCallResultKey = BaseStyle.
				Bold(true).
				Foreground(TextSecondary)
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
	// SuggestionGhostStyle renders inline auto-complete hints in a muted tone.
	// Use a distinct grey so suggestion text is visually separate from the user's input.
	SuggestionGhostStyle = BaseStyle.Foreground(lipgloss.Color(ColorSuggestionGhost))
)

// Scrollbar
var (
	TrackStyle = lipgloss.NewStyle().Foreground(BorderSecondary)
	ThumbStyle = lipgloss.NewStyle().Foreground(Accent)
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

	NotificationWarningStyle = BaseStyle.
					Border(lipgloss.RoundedBorder()).
					BorderForeground(Warning).
					Padding(0, 1)

	NotificationErrorStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Error).
				Padding(0, 1)
)

// Completion Styles
var (
	CompletionBoxStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderSecondary).
				Padding(0, 1)

	CompletionSelectedStyle = BaseStyle.
				Foreground(TextPrimary).
				Bold(true)

	CompletionNormalStyle = BaseStyle.
				Foreground(TextPrimary)

	CompletionDescStyle = BaseStyle.
				Foreground(TextSecondary).
				Italic(true)

	CompletionNoResultsStyle = BaseStyle.
					Foreground(TextMuted).
					Italic(true).
					Align(lipgloss.Center)
)

// Agent and transfer badge styles
var (
	AgentBadgeStyle = BaseStyle.
			Foreground(AgentBadge).
			Bold(true).
			Padding(0, 1)

	TransferBadgeStyle = BaseStyle.
				Foreground(TransferBadge).
				Bold(true).
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

// Spinner Styles
var (
	SpinnerCharStyle          = BaseStyle.Foreground(Accent)
	SpinnerTextBrightestStyle = BaseStyle.Foreground(lipgloss.Color(ColorSpinnerBrightest))
	SpinnerTextBrightStyle    = BaseStyle.Foreground(lipgloss.Color(ColorSpinnerBright))
	SpinnerTextDimStyle       = BaseStyle.Foreground(lipgloss.Color(ColorSpinnerDim))
	SpinnerTextDimmestStyle   = BaseStyle.Foreground(Accent)
)

func toChroma(style ansi.StylePrimitive) string {
	var s []string

	if style.Color != nil {
		s = append(s, *style.Color)
	}
	if style.BackgroundColor != nil {
		s = append(s, "bg:"+*style.BackgroundColor)
	}
	if style.Italic != nil && *style.Italic {
		s = append(s, "italic")
	}
	if style.Bold != nil && *style.Bold {
		s = append(s, "bold")
	}
	if style.Underline != nil && *style.Underline {
		s = append(s, "underline")
	}

	return strings.Join(s, " ")
}

func getChromaTheme() chroma.StyleEntries {
	md := MarkdownStyle().CodeBlock
	return chroma.StyleEntries{
		chroma.Text:                toChroma(md.Chroma.Text),
		chroma.Error:               toChroma(md.Chroma.Error),
		chroma.Comment:             toChroma(md.Chroma.Comment),
		chroma.CommentPreproc:      toChroma(md.Chroma.CommentPreproc),
		chroma.Keyword:             toChroma(md.Chroma.Keyword),
		chroma.KeywordReserved:     toChroma(md.Chroma.KeywordReserved),
		chroma.KeywordNamespace:    toChroma(md.Chroma.KeywordNamespace),
		chroma.KeywordType:         toChroma(md.Chroma.KeywordType),
		chroma.Operator:            toChroma(md.Chroma.Operator),
		chroma.Punctuation:         toChroma(md.Chroma.Punctuation),
		chroma.Name:                toChroma(md.Chroma.Name),
		chroma.NameBuiltin:         toChroma(md.Chroma.NameBuiltin),
		chroma.NameTag:             toChroma(md.Chroma.NameTag),
		chroma.NameAttribute:       toChroma(md.Chroma.NameAttribute),
		chroma.NameClass:           toChroma(md.Chroma.NameClass),
		chroma.NameDecorator:       toChroma(md.Chroma.NameDecorator),
		chroma.NameFunction:        toChroma(md.Chroma.NameFunction),
		chroma.LiteralNumber:       toChroma(md.Chroma.LiteralNumber),
		chroma.LiteralString:       toChroma(md.Chroma.LiteralString),
		chroma.LiteralStringEscape: toChroma(md.Chroma.LiteralStringEscape),
		chroma.GenericDeleted:      toChroma(md.Chroma.GenericDeleted),
		chroma.GenericEmph:         toChroma(md.Chroma.GenericEmph),
		chroma.GenericInserted:     toChroma(md.Chroma.GenericInserted),
		chroma.GenericStrong:       toChroma(md.Chroma.GenericStrong),
		chroma.GenericSubheading:   toChroma(md.Chroma.GenericSubheading),
		chroma.Background:          toChroma(md.Chroma.Background),
	}
}

func ChromaStyle() *chroma.Style {
	style, err := chroma.NewStyle("cagent", getChromaTheme())
	if err != nil {
		panic(err)
	}
	return style
}

func MarkdownStyle() ansi.StyleConfig {
	h1Color := ColorAccentBlue
	h2Color := ColorAccentBlue
	h3Color := ColorTextSecondary
	h4Color := ColorTextSecondary
	h5Color := ColorTextSecondary
	h6Color := ColorMutedBlue
	linkColor := ColorAccentBlue
	strongColor := ColorTextPrimary
	codeColor := ColorTextPrimary
	codeBgColor := ColorBackgroundAlt
	blockquoteColor := ColorTextSecondary
	listColor := ColorTextPrimary
	hrColor := ColorBorderSecondary
	codeBg := ColorBackgroundAlt

	customDarkStyle := ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "",
				BlockSuffix: "",
				Color:       stringPtr(ANSIColor252),
			},
			Margin: uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &blockquoteColor,
			},
			Indent:      uintPtr(1),
			IndentToken: nil,
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       stringPtr(ANSIColor39),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           &h1Color,
				BackgroundColor: stringPtr(ANSIColor63),
				Bold:            boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  &h2Color,
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  &h3Color,
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  &h4Color,
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  &h5Color,
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  &h6Color,
				Bold:   boolPtr(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Color: &strongColor,
			Bold:  boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  &hrColor,
			Format: "\n--------\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     &linkColor,
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr(ANSIColor35),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     stringPtr(ANSIColor212),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  stringPtr(ANSIColor243),
			Format: "Image: {{.text}} →",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           &codeColor,
				BackgroundColor: &codeBgColor,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr(ANSIColor244),
				},
				Margin: uintPtr(defaultMargin),
			},
			Theme: "monokai",
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: stringPtr(ColorTextPrimary),
				},
				Error: ansi.StylePrimitive{
					Color:           stringPtr(ChromaErrorFgColor),
					BackgroundColor: stringPtr(ChromaErrorBgColor),
				},
				Comment: ansi.StylePrimitive{
					Color: stringPtr(ChromaCommentColor),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: stringPtr(ChromaCommentPreprocColor),
				},
				Keyword: ansi.StylePrimitive{
					Color: stringPtr(ChromaKeywordColor),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: stringPtr(ChromaKeywordReservedColor),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: stringPtr(ChromaKeywordNamespaceColor),
				},
				KeywordType: ansi.StylePrimitive{
					Color: stringPtr(ChromaKeywordTypeColor),
				},
				Operator: ansi.StylePrimitive{
					Color: stringPtr(ChromaOperatorColor),
				},
				Punctuation: ansi.StylePrimitive{
					Color: stringPtr(ChromaPunctuationColor),
				},
				Name: ansi.StylePrimitive{
					Color: stringPtr(ColorTextPrimary),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: stringPtr(ChromaNameBuiltinColor),
				},
				NameTag: ansi.StylePrimitive{
					Color: stringPtr(ChromaNameTagColor),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: stringPtr(ChromaNameAttributeColor),
				},
				NameClass: ansi.StylePrimitive{
					Color:     stringPtr(ChromaErrorFgColor),
					Underline: boolPtr(true),
					Bold:      boolPtr(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: stringPtr(ChromaNameDecoratorColor),
				},
				NameFunction: ansi.StylePrimitive{
					Color: stringPtr(ChromaSuccessColor),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: stringPtr(ChromaLiteralNumberColor),
				},
				LiteralString: ansi.StylePrimitive{
					Color: stringPtr(ChromaLiteralStringColor),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: stringPtr(ChromaLiteralStringEscapeColor),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: stringPtr(ChromaGenericDeletedColor),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: stringPtr(ChromaSuccessColor),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: stringPtr(ChromaGenericSubheadingColor),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: stringPtr(ChromaBackgroundColor),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n🠶 ",
		},
	}

	customDarkStyle.List.Color = &listColor
	customDarkStyle.CodeBlock.BackgroundColor = &codeBg

	return customDarkStyle
}

func uintPtr(u uint) *uint {
	return &u
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}
