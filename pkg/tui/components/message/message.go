package message

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Model represents a view that can render a message
type Model interface {
	layout.Model
	layout.Sizeable
	SetMessage(msg *types.Message)
	SetSelected(selected bool)
}

// messageModel implements Model
type messageModel struct {
	message  *types.Message
	previous *types.Message

	width    int
	height   int
	focused  bool
	selected bool
	spinner  spinner.Spinner
}

// New creates a new message view
func New(msg, previous *types.Message) *messageModel {
	return &messageModel{
		message:  msg,
		previous: previous,
		width:    80, // Default width
		height:   1,  // Will be calculated
		focused:  false,
		spinner:  spinner.New(spinner.ModeBoth, styles.SpinnerDotsAccentStyle),
	}
}

// Bubble Tea Model methods

// Init initializes the message view
func (mv *messageModel) Init() tea.Cmd {
	if mv.message.Type == types.MessageTypeSpinner || mv.message.Type == types.MessageTypeLoading {
		return mv.spinner.Init()
	}
	return nil
}

func (mv *messageModel) SetMessage(msg *types.Message) {
	mv.message = msg
}

func (mv *messageModel) SetSelected(selected bool) {
	mv.selected = selected
}

// Update handles messages and updates the message view state
func (mv *messageModel) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if mv.message.Type == types.MessageTypeSpinner || mv.message.Type == types.MessageTypeLoading {
		s, cmd := mv.spinner.Update(msg)
		mv.spinner = s.(spinner.Spinner)
		return mv, cmd
	}
	return mv, nil
}

// View renders the message view
func (mv *messageModel) View() string {
	return mv.Render(mv.width)
}

// Render renders the message view content
func (mv *messageModel) Render(width int) string {
	msg := mv.message
	switch msg.Type {
	case types.MessageTypeSpinner:
		return mv.spinner.View()
	case types.MessageTypeUser:
		return styles.UserMessageStyle.Width(width).Render(msg.Content)
	case types.MessageTypeAssistant:
		if msg.Content == "" {
			return mv.spinner.View()
		}

		messageStyle := styles.AssistantMessageStyle
		if mv.selected {
			messageStyle = styles.SelectedMessageStyle
		}

		rendered, err := markdown.NewRenderer(width - messageStyle.GetHorizontalFrameSize()).Render(msg.Content)
		if err != nil {
			rendered = msg.Content
		}

		if mv.sameAgentAsPrevious(msg) {
			return messageStyle.Render(rendered)
		}

		return mv.senderPrefix(msg.Sender) + messageStyle.Render(rendered)
	case types.MessageTypeShellOutput:
		if rendered, err := markdown.NewRenderer(width).Render(fmt.Sprintf("```console\n%s\n```", msg.Content)); err == nil {
			return rendered
		}
		return msg.Content
	case types.MessageTypeCancelled:
		return styles.WarningStyle.Render("⚠ stream cancelled ⚠")
	case types.MessageTypeWelcome:
		messageStyle := styles.WelcomeMessageStyle
		rendered, err := markdown.NewRenderer(width - messageStyle.GetHorizontalFrameSize()).Render(msg.Content)
		if err != nil {
			rendered = msg.Content
		}
		return messageStyle.Width(width - 1).Render(strings.TrimRight(rendered, "\n\r\t "))
	case types.MessageTypeError:
		return styles.ErrorMessageStyle.Width(width - 1).Render(msg.Content)
	case types.MessageTypeLoading:
		// Show spinner with the loading description, truncated to fit width
		spinnerView := mv.spinner.View()
		spinnerWidth := ansi.StringWidth(spinnerView) + 1 // +1 for space separator
		maxDescWidth := width - spinnerWidth
		description := msg.Content
		if maxDescWidth > 0 && ansi.StringWidth(description) > maxDescWidth {
			description = ansi.Truncate(description, maxDescWidth, "…")
		}
		return spinnerView + " " + styles.MutedStyle.Render(description)
	default:
		return msg.Content
	}
}

func (mv *messageModel) senderPrefix(sender string) string {
	if sender == "" {
		return ""
	}
	return styles.AgentBadgeStyle.MarginLeft(2).Render(sender) + "\n\n"
}

// sameAgentAsPrevious returns true if the previous message was from the same agent
func (mv *messageModel) sameAgentAsPrevious(msg *types.Message) bool {
	if mv.previous == nil || mv.previous.Sender != msg.Sender {
		return false
	}
	switch mv.previous.Type {
	case types.MessageTypeAssistant,
		types.MessageTypeAssistantReasoningBlock,
		types.MessageTypeToolCall,
		types.MessageTypeToolResult:
		return true
	default:
		return false
	}
}

// Height calculates the height needed for this message view
func (mv *messageModel) Height(width int) int {
	content := mv.Render(width)
	return strings.Count(content, "\n") + 1
}

// Message returns the underlying message
func (mv *messageModel) Message() *types.Message {
	return mv.message
}

// Layout.Sizeable methods

// SetSize sets the dimensions of the message view
func (mv *messageModel) SetSize(width, height int) tea.Cmd {
	mv.width = width
	mv.height = height
	return nil
}

// GetSize returns the current dimensions
func (mv *messageModel) GetSize() (width, height int) {
	return mv.width, mv.height
}

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
