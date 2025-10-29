package message

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Model represents a view that can render a message
type Model interface {
	layout.Model
	layout.Sizeable
	SetMessage(msg *types.Message)
}

// messageModel implements Model
type messageModel struct {
	message *types.Message
	width   int
	height  int
	focused bool
	spinner spinner.Model
}

// New creates a new message view
func New(msg *types.Message) *messageModel {
	return &messageModel{
		message: msg,
		width:   80, // Default width
		height:  1,  // Will be calculated
		focused: false,
		spinner: spinner.New(spinner.WithSpinner(spinner.Points)),
	}
}

// Bubble Tea Model methods

// Init initializes the message view
func (mv *messageModel) Init() tea.Cmd {
	if mv.message.Type == types.MessageTypeSpinner {
		return mv.spinner.Tick
	}
	return nil
}

func (mv *messageModel) SetMessage(msg *types.Message) {
	mv.message = msg
}

// Update handles messages and updates the message view state
func (mv *messageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mv.message.Type == types.MessageTypeSpinner {
		var cmd tea.Cmd
		mv.spinner, cmd = mv.spinner.Update(msg)
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
		if rendered, err := markdown.NewRenderer(width - len(styles.UserMessageBorderStyle.Render(""))).Render(msg.Content); err == nil {
			return styles.UserMessageBorderStyle.Render(strings.TrimRight(rendered, "\n\r\t "))
		}

		return msg.Content
	case types.MessageTypeAssistant:
		if msg.Content == "" {
			return mv.spinner.View()
		}

		text := senderPrefix(msg.Sender) + msg.Content
		rendered, err := markdown.NewRenderer(width).Render(text)
		if err != nil {
			return text
		}

		return strings.TrimRight(rendered, "\n\r\t ")
	case types.MessageTypeAssistantReasoning:
		if msg.Content == "" {
			return mv.spinner.View()
		}
		text := "Thinking: " + senderPrefix(msg.Sender) + msg.Content
		// Render through the markdown renderer to ensure proper wrapping to width
		rendered, err := markdown.NewRenderer(width).Render(text)
		if err != nil {
			return styles.MutedStyle.Italic(true).Render(text)
		}
		// Strip ANSI from inner rendering so muted style fully applies
		clean := stripANSI(strings.TrimRight(rendered, "\n\r\t "))
		return styles.MutedStyle.Italic(true).Render(clean)
	case types.MessageTypeShellOutput:
		if rendered, err := markdown.NewRenderer(width).Render(fmt.Sprintf("```console\n%s\n```", msg.Content)); err == nil {
			return strings.TrimRight(rendered, "\n\r\t ")
		}
		return msg.Content
	case types.MessageTypeSeparator:
		return styles.MutedStyle.Render("•" + strings.Repeat("─", mv.width-3) + "•")
	case types.MessageTypeCancelled:
		return styles.WarningStyle.Render("⚠ stream cancelled ⚠")
	case types.MessageTypeError:
		return styles.ErrorStyle.Render("│ " + msg.Content)
	case types.MessageTypeWarning:
		return styles.WarningStyle.Render(msg.Content)
	case types.MessageTypeSystem:
		return styles.MutedStyle.Render("ℹ " + msg.Content)
	default:
		return msg.Content
	}
}

func senderPrefix(sender string) string {
	if sender == "" || sender == "root" {
		return ""
	}
	return fmt.Sprintf("%s: ", sender)
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
