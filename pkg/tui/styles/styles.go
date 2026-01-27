package styles

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
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
	ColorWhite           = "#E5F2FC"
	ColorAccentBlue      = "#7AA2F7"
	ColorMutedBlue       = "#8B95C1"
	ColorMutedGray       = "#808080"
	ColorFadedGray       = "#404550" // Very dim, close to background - for fade-out effects
	ColorBackgroundAlt   = "#24283B"
	ColorBorderSecondary = "#6B75A8"
	ColorTextPrimary     = "#C0C0C0"
	ColorTextSecondary   = "#808080"
	ColorSuccessGreen    = "#9ECE6A"
	ColorErrorRed        = "#F7768E"
	ColorWarningYellow   = "#E0AF68"
	ColorMobyBlue        = "#1D63ED"
	ColorDarkBlue        = "#202a4b"
	ColorErrorStrong     = "#d74532"
	ColorErrorDark       = "#4a2523"

	// Spinner glow colors (transition from base blue towards white)
	ColorSpinnerDim       = "#9AB8F9"
	ColorSpinnerBright    = "#B8CFFB"
	ColorSpinnerBrightest = "#D6E5FC"

	// Background colors
	ColorBackground = "#1C1C22"

	// Status colors
	ColorInfoCyan  = "#7DCFFF"
	ColorHighlight = "#98C379"

	// Diff colors
	ColorDiffAddBg    = "#20303B"
	ColorDiffRemoveBg = "#3C2A2A"

	// Line number and UI element colors
	ColorLineNumber = "#565F89"
	ColorSeparator  = "#414868"

	// Interactive element colors
	ColorSelected = "#364A82"

	// AutoCompleteGhost colors
	ColorSuggestionGhost = "#6B6B6B"

	// Tab colors
	ColorTab = "#25252c"
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
	White    = lipgloss.Color(ColorWhite)
	MobyBlue = lipgloss.Color(ColorMobyBlue)
	Accent   = lipgloss.Color(ColorAccentBlue)

	// Status colors - softer, more professional
	Success   = lipgloss.Color(ColorSuccessGreen)
	Error     = lipgloss.Color(ColorErrorRed)
	Warning   = lipgloss.Color(ColorWarningYellow)
	Info      = lipgloss.Color(ColorInfoCyan)
	Highlight = lipgloss.Color(ColorHighlight)

	// Text hierarchy
	TextPrimary   = lipgloss.Color(ColorTextPrimary)
	TextSecondary = lipgloss.Color(ColorTextSecondary)
	TextMuted     = lipgloss.Color(ColorMutedBlue)
	TextMutedGray = lipgloss.Color(ColorMutedGray)

	// Border colors
	BorderPrimary   = lipgloss.Color(ColorAccentBlue)
	BorderSecondary = lipgloss.Color(ColorBorderSecondary)
	BorderMuted     = lipgloss.Color(ColorBackgroundAlt)
	BorderWarning   = lipgloss.Color(ColorWarningYellow)

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
	PlaceholderColor = lipgloss.Color(ColorMutedGray)

	// Badge colors
	AgentBadgeFg = White
	AgentBadgeBg = MobyBlue

	// Tabs
	TabBg        = lipgloss.Color(ColorTab)
	TabPrimaryFg = lipgloss.Color(ColorMutedGray)
	TabAccentFg  = lipgloss.Color(ColorHighlight)
)

// Base Styles
const (
	AppPaddingLeft = 1 // Keep in sync with AppStyle padding

	// DoubleClickThreshold is the maximum time between clicks to register as a double-click
	DoubleClickThreshold = 400 * time.Millisecond
)

var (
	NoStyle   = lipgloss.NewStyle()
	BaseStyle = NoStyle.Foreground(TextPrimary)
	AppStyle  = BaseStyle.Padding(0, 1, 0, AppPaddingLeft)
)

// Text Styles
var (
	HighlightWhiteStyle = BaseStyle.Foreground(White).Bold(true)
	MutedStyle          = BaseStyle.Foreground(TextMutedGray)
	SecondaryStyle      = BaseStyle.Foreground(TextSecondary)
	BoldStyle           = BaseStyle.Bold(true)
	FadingStyle         = NoStyle.Foreground(lipgloss.Color(ColorFadedGray)) // Very dim for fade-out animations
)

// Status Styles
var (
	SuccessStyle    = BaseStyle.Foreground(Success)
	ErrorStyle      = BaseStyle.Foreground(Error)
	WarningStyle    = BaseStyle.Foreground(Warning)
	InfoStyle       = BaseStyle.Foreground(Info)
	ActiveStyle     = BaseStyle.Foreground(Success)
	ToBeDoneStyle   = BaseStyle.Foreground(TextPrimary)
	InProgressStyle = BaseStyle.Foreground(Highlight)
	CompletedStyle  = BaseStyle.Foreground(TextMutedGray)
)

// Layout Styles
var (
	CenterStyle = BaseStyle.Align(lipgloss.Center, lipgloss.Center)
)

// Border Styles
var (
	BaseMessageStyle = BaseStyle.
				Padding(1, 1).
				BorderLeft(true).
				BorderStyle(lipgloss.HiddenBorder()).
				BorderForeground(BorderPrimary)

	UserMessageStyle = BaseMessageStyle.
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(BorderPrimary).
				Background(BackgroundAlt).
				Bold(true)

	AssistantMessageStyle = BaseMessageStyle.
				Padding(0, 1)

	WelcomeMessageStyle = BaseMessageStyle.
				BorderStyle(lipgloss.DoubleBorder()).
				Bold(true)

	ErrorMessageStyle = BaseMessageStyle.
				BorderStyle(lipgloss.ThickBorder()).
				Foreground(Error)

	SelectedMessageStyle = AssistantMessageStyle.
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(Success)
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

	TabTitleStyle = BaseStyle.
			Foreground(TabPrimaryFg)

	TabStyle = TabPrimaryStyle.
			Padding(1, 0)

	TabPrimaryStyle = BaseStyle.
			Foreground(TextPrimary)

	TabAccentStyle = BaseStyle.
			Foreground(TabAccentFg)
)

// Model selector Badge colors
const (
	ColorBadgePurple = "#B083EA" // Purple for alloy badge
	ColorBadgeCyan   = "#7DCFFF" // Cyan for default badge
	ColorBadgeGreen  = "#9ECE6A" // Green for current badge
)

// Command Palette Styles
var (
	PaletteCategoryStyle = BaseStyle.
				Bold(true).
				Foreground(White).
				MarginTop(1)

	PaletteUnselectedActionStyle = BaseStyle.
					Foreground(TextPrimary).
					Bold(true)

	PaletteSelectedActionStyle = PaletteUnselectedActionStyle.
					Background(MobyBlue).
					Foreground(White)

	PaletteUnselectedDescStyle = BaseStyle.
					Foreground(TextSecondary)

	PaletteSelectedDescStyle = PaletteUnselectedDescStyle.
					Background(MobyBlue).
					Foreground(White)

	// Badge styles for model picker
	BadgeAlloyStyle = BaseStyle.
			Foreground(lipgloss.Color(ColorBadgePurple))

	BadgeDefaultStyle = BaseStyle.
				Foreground(lipgloss.Color(ColorBadgeCyan))

	BadgeCurrentStyle = BaseStyle.
				Foreground(lipgloss.Color(ColorBadgeGreen))
)

// Star Styles for session browser and sidebar
var (
	StarredStyle   = BaseStyle.Foreground(Success)
	UnstarredStyle = BaseStyle.Foreground(TextMuted)
)

// StarIndicator returns the styled star indicator for a given starred status
func StarIndicator(starred bool) string {
	if starred {
		return StarredStyle.Render("★") + " "
	}
	return UnstarredStyle.Render("☆") + " "
}

// Diff Styles (matching glamour markdown theme)
var (
	DiffAddStyle = BaseStyle.
			Background(DiffAddBg).
			Foreground(DiffAddFg)

	DiffRemoveStyle = BaseStyle.
			Background(DiffRemoveBg).
			Foreground(DiffRemoveFg)

	DiffUnchangedStyle = BaseStyle.Background(BackgroundAlt)
)

// Syntax highlighting UI element styles
var (
	LineNumberStyle = BaseStyle.Foreground(LineNumber).Background(BackgroundAlt)
	SeparatorStyle  = BaseStyle.Foreground(Separator).Background(BackgroundAlt)
)

// Tool Call Styles
var (
	ToolMessageStyle = BaseStyle.
				Foreground(TextMutedGray)

	ToolErrorMessageStyle = BaseStyle.
				Foreground(lipgloss.Color(ColorErrorStrong))

	ToolName = ToolMessageStyle.
			Foreground(TextMutedGray).
			Padding(0, 1)

	ToolNameError = ToolName.
			Foreground(lipgloss.Color(ColorErrorStrong)).
			Background(lipgloss.Color(ColorErrorDark))

	ToolNameDim = ToolMessageStyle.
			Foreground(TextMutedGray).
			Italic(true)

	ToolDescription = ToolMessageStyle.
			Foreground(TextPrimary)

	ToolCompletedIcon = BaseStyle.
				MarginLeft(2).
				Foreground(TextMutedGray)

	ToolErrorIcon = ToolCompletedIcon.
			Background(lipgloss.Color(ColorErrorStrong))

	ToolPendingIcon = ToolCompletedIcon.
			Background(lipgloss.Color(ColorWarningYellow))

	ToolCallArgs = ToolMessageStyle.
			Padding(0, 0, 0, 2)

	ToolCallResult = ToolMessageStyle.
			Padding(0, 0, 0, 2)
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

	// DialogInputStyle is the style for textinput fields in dialogs,
	// matching the main editor's look (cursor color, text color).
	DialogInputStyle = textinput.Styles{
		Focused: textinput.StyleState{
			Text:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Blurred: textinput.StyleState{
			Text:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Cursor: textinput.CursorStyle{
			Color: Accent,
		},
	}
	EditorStyle = BaseStyle.Padding(1, 0, 0, 0)
	// SuggestionGhostStyle renders inline auto-complete hints in a muted tone.
	// Use a distinct grey so suggestion text is visually separate from the user's input.
	SuggestionGhostStyle = BaseStyle.Foreground(lipgloss.Color(ColorSuggestionGhost))
	// SuggestionCursorStyle renders the first character of a suggestion inside the cursor.
	// Uses the same blue accent background as the normal cursor, with ghost-colored foreground text.
	SuggestionCursorStyle = BaseStyle.Background(Accent).Foreground(lipgloss.Color(ColorSuggestionGhost))

	// Attachment banner styles - polished look with subtle border
	AttachmentBannerStyle = BaseStyle.
				Foreground(TextSecondary)

	AttachmentBadgeStyle = BaseStyle.
				Foreground(lipgloss.Color(ColorInfoCyan)).
				Bold(true)

	AttachmentSizeStyle = BaseStyle.
				Foreground(TextMuted).
				Italic(true)

	AttachmentIconStyle = BaseStyle.
				Foreground(lipgloss.Color(ColorInfoCyan))
)

// Scrollbar
var (
	TrackStyle       = lipgloss.NewStyle().Foreground(BorderSecondary)
	ThumbStyle       = lipgloss.NewStyle().Foreground(Info).Background(BackgroundAlt).Bold(true)
	ThumbActiveStyle = lipgloss.NewStyle().Foreground(White).Background(BackgroundAlt).Bold(true)
)

// Resize Handle Style
var (
	ResizeHandleStyle = BaseStyle.
				Foreground(BorderSecondary)

	ResizeHandleHoverStyle = BaseStyle.
				Foreground(Info).
				Bold(true)

	ResizeHandleActiveStyle = BaseStyle.
				Foreground(White).
				Bold(true)
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

	CompletionNormalStyle = BaseStyle.
				Foreground(TextPrimary).
				Bold(true)

	CompletionSelectedStyle = CompletionNormalStyle.
				Foreground(White).
				Background(MobyBlue)

	CompletionDescStyle = BaseStyle.
				Foreground(TextSecondary)

	CompletionSelectedDescStyle = CompletionDescStyle.
					Foreground(White).
					Background(MobyBlue)

	CompletionNoResultsStyle = BaseStyle.
					Foreground(TextMuted).
					Italic(true).
					Align(lipgloss.Center)
)

// Agent and transfer badge styles
var (
	AgentBadgeStyle = BaseStyle.
			Foreground(AgentBadgeFg).
			Background(AgentBadgeBg).
			Padding(0, 1)

	ThinkingBadgeStyle = BaseStyle.
				Foreground(TextMuted). // Muted blue, distinct from gray italic content
				Bold(true).
				Italic(true)
)

// Deprecated styles (kept for backward compatibility)
var (
	ChatStyle = BaseStyle
)

// Selection Styles
var (
	SelectionStyle = BaseStyle.
		Background(Selected).
		Foreground(SelectedFg)
)

// Spinner Styles
var (
	SpinnerDotsAccentStyle    = BaseStyle.Foreground(Accent)
	SpinnerDotsHighlightStyle = BaseStyle.Foreground(TabAccentFg)
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
	h3Color := ColorAccentBlue
	h4Color := ColorAccentBlue
	h5Color := ColorAccentBlue
	h6Color := ColorAccentBlue
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
				Prefix: "## ",
				Color:  &h1Color,
				Bold:   boolPtr(true),
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
