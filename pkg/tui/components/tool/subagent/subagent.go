// Package subagent provides a tree-like view for sub-agent (transfer task) execution.
// It displays tool calls made by sub-agents in a collapsed tree structure.
package subagent

import (
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// ToolNode represents a tool call in the tree
type ToolNode struct {
	ID         string // Tool call ID for matching results
	Name       string
	Status     types.ToolStatus
	IsError    bool
	HasContent bool // True if the tool returned meaningful content
}

// Model represents a sub-agent execution tree view
type Model struct {
	message      *types.Message
	spinner      spinner.Spinner
	width        int
	height       int
	sessionState *service.SessionState

	// Sub-agent execution state
	fromAgent string
	toAgent   string
	task      string
	isRunning bool
	tools     []ToolNode
}

// New creates a new sub-agent tree view
func New(msg *types.Message, sessionState *service.SessionState) *Model {
	// Parse the transfer task args to get agent and task info
	toAgent, task := parseTransferTaskArgs(msg.ToolCall.Function.Arguments)

	return &Model{
		message:      msg,
		spinner:      spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsAccentStyle),
		width:        80,
		height:       1,
		sessionState: sessionState,
		fromAgent:    msg.Sender,
		toAgent:      toAgent,
		task:         task,
		isRunning:    true,
		tools:        make([]ToolNode, 0),
	}
}

// parseTransferTaskArgs extracts agent and task from the arguments JSON
func parseTransferTaskArgs(args string) (agent, task string) {
	var params builtin.TransferTaskArgs
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", ""
	}
	return params.Agent, params.Task
}

// UpdateFromMessage updates the task info from the message's current arguments
// This is called when tool arguments are updated during streaming
func (m *Model) UpdateFromMessage() {
	if m.message == nil {
		return
	}
	agent, task := parseTransferTaskArgs(m.message.ToolCall.Function.Arguments)
	if agent != "" {
		m.toAgent = agent
	}
	if task != "" {
		m.task = task
	}
}

// AddTool adds a tool call to the tree
func (m *Model) AddTool(id, name string, status types.ToolStatus, isError bool) {
	// Check if tool already exists by ID, update if so
	for i := range m.tools {
		if m.tools[i].ID == id {
			m.tools[i].Status = status
			m.tools[i].IsError = isError
			return
		}
	}
	m.tools = append(m.tools, ToolNode{
		ID:      id,
		Name:    name,
		Status:  status,
		IsError: isError,
	})
}

// UpdateTool updates the status of a tool by ID
func (m *Model) UpdateTool(id string, status types.ToolStatus, isError, hasContent bool) {
	for i := range m.tools {
		if m.tools[i].ID == id {
			m.tools[i].Status = status
			m.tools[i].IsError = isError
			m.tools[i].HasContent = hasContent
			return
		}
	}
}

// SetRunning sets whether the sub-agent is still running
func (m *Model) SetRunning(running bool) {
	m.isRunning = running
}

// IsRunning returns whether the sub-agent is still running
func (m *Model) IsRunning() bool {
	return m.isRunning
}

// Message returns the underlying message
func (m *Model) Message() *types.Message {
	return m.message
}

// ToAgent returns the name of the sub-agent
func (m *Model) ToAgent() string {
	return m.toAgent
}

func (m *Model) Init() tea.Cmd {
	if m.isRunning {
		return m.spinner.Init()
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if m.isRunning {
		model, cmd := m.spinner.Update(msg)
		m.spinner = model.(spinner.Spinner)
		return m, cmd
	}
	return m, nil
}

func (m *Model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *Model) View() string {
	return styles.ToolMessageStyle.Width(m.width).Render(lipgloss.JoinVertical(lipgloss.Top, m.renderHeader(), m.renderTask(), m.renderToolTree()))
}

func (m *Model) renderHeader() string {
	return lipgloss.JoinHorizontal(lipgloss.Left, styles.AgentBadgeStyle.MarginLeft(2).Render(m.fromAgent), " → ", styles.AgentBadgeStyle.Render(m.toAgent))
}

func (m *Model) renderTask() string {
	// Truncate task if too long
	task := m.task
	maxLen := m.width - 8 // Account for indentation and margins
	if len(task) > maxLen && maxLen > 0 {
		task = task[:maxLen-1] + "…"
	}

	taskStyle := lipgloss.NewStyle().
		MarginLeft(4).
		Foreground(styles.TextMutedGray)

	return taskStyle.Render(task)
}

func (m *Model) renderToolTree() string {
	if len(m.tools) == 0 {
		return ""
	}

	var lines []string

	treeStyle := lipgloss.NewStyle().
		MarginLeft(4).
		Foreground(styles.TextMutedGray)

	for i, tool := range m.tools {
		var prefix string
		if i == len(m.tools)-1 {
			prefix = "└─"
		} else {
			prefix = "├─"
		}

		icon := m.getToolIcon(tool)
		line := fmt.Sprintf("%s %s %s", prefix, icon, tool.Name)
		lines = append(lines, treeStyle.Render(line))
	}

	return lipgloss.JoinVertical(lipgloss.Top, lines...)
}

func (m *Model) getToolIcon(tool ToolNode) string {
	switch tool.Status {
	case types.ToolStatusPending, types.ToolStatusRunning:
		return styles.BaseStyle.MarginLeft(2).Render(m.spinner.View())
	case types.ToolStatusCompleted:
		return styles.ToolCompletedIcon.Render("✓")
	case types.ToolStatusError:
		return styles.ToolErrorIcon.Render("✗")
	default:
		return styles.ToolPendingIcon.Render("○")
	}
}
