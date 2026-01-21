package toolcommon

import (
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Renderer is a function that renders a tool call view.
// It receives the message, spinner, session state, and available width/height.
type Renderer func(msg *types.Message, s spinner.Spinner, sessionState *service.SessionState, width, height int) string

// Base provides common boilerplate for tool components.
// It handles spinner management, sizing, and delegates rendering to a custom function.
type Base struct {
	message      *types.Message
	spinner      spinner.Spinner
	width        int
	height       int
	sessionState *service.SessionState
	render       Renderer
}

// NewBase creates a new base tool component with the given renderer.
func NewBase(msg *types.Message, sessionState *service.SessionState, render Renderer) *Base {
	return &Base{
		message:      msg,
		spinner:      spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsAccentStyle),
		width:        80,
		height:       1,
		sessionState: sessionState,
		render:       render,
	}
}

// Message returns the tool message.
func (b *Base) Message() *types.Message {
	return b.message
}

// SessionState returns the session state.
func (b *Base) SessionState() *service.SessionState {
	return b.sessionState
}

// Width returns the current width.
func (b *Base) Width() int {
	return b.width
}

// Height returns the current height.
func (b *Base) Height() int {
	return b.height
}

// Spinner returns the spinner.
func (b *Base) Spinner() spinner.Spinner {
	return b.spinner
}

func (b *Base) SetSize(width, height int) tea.Cmd {
	b.width = width
	b.height = height
	return nil
}

func (b *Base) Init() tea.Cmd {
	if b.isSpinnerActive() {
		return b.spinner.Init()
	}
	return nil
}

func (b *Base) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if b.isSpinnerActive() {
		model, cmd := b.spinner.Update(msg)
		b.spinner = model.(spinner.Spinner)
		return b, cmd
	}
	return b, nil
}

func (b *Base) View() string {
	return b.render(b.message, b.spinner, b.sessionState, b.width, b.height)
}

func (b *Base) isSpinnerActive() bool {
	return b.message.ToolStatus == types.ToolStatusPending ||
		b.message.ToolStatus == types.ToolStatusRunning
}

// SimpleRenderer creates a renderer that extracts a single string argument
// and renders it with RenderTool. This covers the most common case where
// tools just display one argument (like path, command, etc.).
func SimpleRenderer(extractArg func(args string) string) Renderer {
	return func(msg *types.Message, s spinner.Spinner, sessionState *service.SessionState, width, _ int) string {
		arg := ""
		if msg.ToolCall.Function.Arguments != "" {
			arg = extractArg(msg.ToolCall.Function.Arguments)
		}
		return RenderTool(msg, s, arg, "", width, sessionState.HideToolResults())
	}
}

// SimpleRendererWithResult creates a renderer that extracts a single string argument
// and also shows a result/summary after completion.
func SimpleRendererWithResult(
	extractArg func(args string) string,
	extractResult func(msg *types.Message) string,
) Renderer {
	return func(msg *types.Message, s spinner.Spinner, sessionState *service.SessionState, width, _ int) string {
		arg := ""
		if msg.ToolCall.Function.Arguments != "" {
			arg = extractArg(msg.ToolCall.Function.Arguments)
		}

		result := ""
		if msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError {
			result = extractResult(msg)
		}

		return RenderTool(msg, s, arg, result, width, sessionState.HideToolResults())
	}
}
