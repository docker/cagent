package defaulttool

import (
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Component is the fallback component for rendering tool calls
// that don't have a specialized component registered.
// It provides a standard visualization with tool name, arguments, and results.
type Component struct {
	message *types.Message
	spinner spinner.Spinner
	width   int
	height  int
}

// New creates a new default tool component.
func New(
	msg *types.Message,
	_ *service.SessionState,
) layout.Model {
	return &Component{
		message: msg,
		spinner: spinner.New(spinner.ModeSpinnerOnly),
		width:   80,
		height:  1,
	}
}

func (c *Component) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	return nil
}

func (c *Component) Init() tea.Cmd {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		return c.spinner.Init()
	}
	return nil
}

func (c *Component) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if c.message.ToolStatus == types.ToolStatusPending || c.message.ToolStatus == types.ToolStatusRunning {
		var cmd tea.Cmd
		var model layout.Model
		model, cmd = c.spinner.Update(msg)
		c.spinner = model.(spinner.Spinner)
		return c, cmd
	}

	return c, nil
}

func (c *Component) View() string {
	msg := c.message
	displayName := msg.ToolDefinition.DisplayName()

	var argsContent string
	if msg.ToolCall.Function.Arguments != "" {
		argsContent = renderToolArgs(msg.ToolCall, c.width-3)
	}

	if argsContent == "" {
		return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), msg.ToolDefinition.DisplayName(), c.spinner.View(), "", c.width)
	}

	var resultContent string
	if (msg.ToolStatus == types.ToolStatusCompleted || msg.ToolStatus == types.ToolStatusError) && msg.Content != "" {
		resultContent = toolcommon.FormatToolResult(msg.Content, c.width)
	}

	return toolcommon.RenderTool(toolcommon.Icon(msg.ToolStatus), styles.HighlightStyle.Render(displayName), argsContent, resultContent, c.width)
}
