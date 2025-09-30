package sidebar

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/todo"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// Model represents a sidebar component
type Model interface {
	layout.Model
	layout.Sizeable

	SetTokenUsage(usage *runtime.Usage)
	SetTodos(toolCall tools.ToolCall) error
	SetWorking(working bool) tea.Cmd
}

// model implements Model
type model struct {
	width    int
	height   int
	usage    *runtime.Usage
	todoComp *todo.Component
	working  bool
	spinner  spinner.Model
}

// New creates a new sidebar component
func New() Model {
	return &model{
		width:    20, // Default width
		height:   24, // Default height
		usage:    &runtime.Usage{},
		todoComp: todo.NewComponent(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
}

// Init initializes the component
func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) SetTokenUsage(usage *runtime.Usage) {
	m.usage = usage
}

func (m *model) SetTodos(toolCall tools.ToolCall) error {
	return m.todoComp.SetTodos(toolCall)
}

// SetWorking sets the working state and returns a command to start the spinner if needed
func (m *model) SetWorking(working bool) tea.Cmd {
	m.working = working
	if working {
		// Start spinner when beginning to work
		return m.spinner.Tick
	}
	return nil
}

// formatTokenCount formats a token count with K/M suffixes for readability
func formatTokenCount(count int) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	} else if count >= 1000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}

// getCurrentWorkingDirectory returns the current working directory with home directory replaced by ~/
func getCurrentWorkingDirectory() string {
	pwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Replace home directory with ~/
	if homeDir, err := os.UserHomeDir(); err == nil && strings.HasPrefix(pwd, homeDir) {
		pwd = "~" + pwd[len(homeDir):]
	}

	return pwd
}

// Update handles messages and updates the component state
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := m.SetSize(msg.Width, msg.Height)
		return m, cmd
	default:
		// Update spinner when working
		if m.working {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// View renders the component
func (m *model) View() string {
	// Calculate token usage metrics
	totalTokens := m.usage.InputTokens + m.usage.OutputTokens
	var usagePercent float64
	if m.usage.ContextLimit > 0 {
		usagePercent = (float64(m.usage.ContextLength) / float64(m.usage.ContextLimit)) * 100
	}

	// Use predefined styles for the usage display

	// Build top content (title + pwd + token usage)
	topContent := ""

	// Add current working directory in grey
	if pwd := getCurrentWorkingDirectory(); pwd != "" {
		topContent += styles.MutedStyle.Render(pwd) + "\n\n"
	}

	// Format each part with its respective color
	percentageText := styles.MutedStyle.Render(fmt.Sprintf("%.0f%%", usagePercent))
	totalTokensText := styles.SubtleStyle.Render(fmt.Sprintf("(%s)", formatTokenCount(totalTokens)))
	costText := styles.MutedStyle.Render(fmt.Sprintf("$%.2f", m.usage.Cost))

	topContent += fmt.Sprintf("%s %s %s", percentageText, totalTokensText, costText)
	// Add working indicator if active
	if m.working {
		workingIndicator := styles.ActiveStyle.Render(m.spinner.View() + " Working...")
		topContent += "\n" + workingIndicator
	}

	// Get todo content (if any)
	m.todoComp.SetSize(m.width)
	todoContent := m.todoComp.Render()

	// If we have todos, create a layout with todos at the bottom
	if todoContent != "" {
		// Remove trailing newline from todoContent if present
		todoContent = strings.TrimSuffix(todoContent, "\n")

		// Calculate available height for content
		availableHeight := m.height - 2 // Account for borders
		topHeight := strings.Count(topContent, "\n") + 1
		todoHeight := strings.Count(todoContent, "\n") + 1

		// Calculate padding needed to push todos to bottom
		paddingHeight := availableHeight - topHeight - todoHeight
		if paddingHeight < 0 {
			paddingHeight = 0
		}

		// Build final content with padding
		finalContent := topContent
		for range paddingHeight {
			finalContent += "\n"
		}
		finalContent += todoContent

		sidebarStyle := styles.BaseStyle.
			Width(m.width).
			Height(m.height-2).
			Align(lipgloss.Left, lipgloss.Top)

		return sidebarStyle.Render(finalContent)
	} else {
		// No todos, just render top content normally
		sidebarStyle := styles.BaseStyle.
			Width(m.width).
			Height(m.height-2).
			Align(lipgloss.Left, lipgloss.Top)

		return sidebarStyle.Render(topContent)
	}
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.todoComp.SetSize(width)
	return nil
}

// GetSize returns the current dimensions
func (m *model) GetSize() (width, height int) {
	return m.width, m.height
}
