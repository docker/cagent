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
// It receives the message, spinner, session state reader, and available width/height.
// Note: Uses SessionStateReader interface for read-only access to session state.
type Renderer func(msg *types.Message, s spinner.Spinner, sessionState service.SessionStateReader, width, height int) string

// CollapsedRenderer is a function that renders a simplified view for collapsed reasoning blocks.
type CollapsedRenderer func(msg *types.Message, s spinner.Spinner, sessionState service.SessionStateReader, width, height int) string

// Base provides common boilerplate for tool components.
// It handles spinner management, sizing, and delegates rendering to a custom function.
type Base struct {
	message           *types.Message
	spinner           spinner.Spinner
	width             int
	height            int
	sessionState      service.SessionStateReader // read-only access to session state
	render            Renderer
	collapsedRenderer CollapsedRenderer
	spinnerRegistered bool // tracks whether spinner is registered with coordinator
}

// NewBase creates a new base tool component with the given renderer.
// Accepts SessionStateReader for read-only access (also accepts *SessionState which implements it).
func NewBase(msg *types.Message, sessionState service.SessionStateReader, render Renderer) *Base {
	return &Base{
		message:      msg,
		spinner:      spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsAccentStyle),
		width:        80,
		height:       1,
		sessionState: sessionState,
		render:       render,
	}
}

// NewBaseWithCollapsed creates a new base tool component with both regular and collapsed renderers.
// Accepts SessionStateReader for read-only access (also accepts *SessionState which implements it).
func NewBaseWithCollapsed(msg *types.Message, sessionState service.SessionStateReader, render Renderer, collapsedRender CollapsedRenderer) *Base {
	return &Base{
		message:           msg,
		spinner:           spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsAccentStyle),
		width:             80,
		height:            1,
		sessionState:      sessionState,
		render:            render,
		collapsedRenderer: collapsedRender,
	}
}

// Message returns the tool message.
func (b *Base) Message() *types.Message {
	return b.message
}

// SessionState returns the session state reader.
func (b *Base) SessionState() service.SessionStateReader {
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
		cmd := b.spinner.Init()
		b.spinnerRegistered = true
		return cmd
	}
	return nil
}

func (b *Base) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	isActive := b.isSpinnerActive()

	var initCmd tea.Cmd

	if isActive && !b.spinnerRegistered {
		initCmd = b.spinner.Init()
		b.spinnerRegistered = true
	} else if !isActive && b.spinnerRegistered {
		// Spinner became inactive - unregister from coordinator
		b.spinnerRegistered = false
		b.spinner.Stop()
	}

	if isActive {
		model, cmd := b.spinner.Update(msg)
		b.spinner = model.(spinner.Spinner)
		if initCmd != nil {
			return b, tea.Batch(initCmd, cmd)
		}
		return b, cmd
	}
	return b, initCmd
}

func (b *Base) View() string {
	return b.render(b.message, b.spinner, b.sessionState, b.width, b.height)
}

// CollapsedView returns a simplified view for use in collapsed reasoning blocks.
// Falls back to the regular View() if no collapsed renderer is provided.
func (b *Base) CollapsedView() string {
	if b.collapsedRenderer != nil {
		return b.collapsedRenderer(b.message, b.spinner, b.sessionState, b.width, b.height)
	}
	return b.View()
}

func (b *Base) isSpinnerActive() bool {
	return b.message.ToolStatus == types.ToolStatusPending ||
		b.message.ToolStatus == types.ToolStatusRunning
}

// SimpleRenderer creates a renderer that extracts a single string argument
// and renders it with RenderTool. This covers the most common case where
// tools just display one argument (like path, command, etc.).
func SimpleRenderer(extractArg func(args string) string) Renderer {
	return func(msg *types.Message, s spinner.Spinner, sessionState service.SessionStateReader, width, _ int) string {
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
	return func(msg *types.Message, s spinner.Spinner, sessionState service.SessionStateReader, width, _ int) string {
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
