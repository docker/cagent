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
	SetMCPInitializing(initializing bool) tea.Cmd
}

// model implements Model
type model struct {
	width    int
	height   int
	usage    *runtime.Usage
	todoComp *todo.Component
	working  bool
	mcpInit  bool
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
	if usage == nil {
		m.usage = &runtime.Usage{}
		return
	}
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

// SetMCPInitializing toggles the MCP initialization spinner state
func (m *model) SetMCPInitializing(initializing bool) tea.Cmd {
    m.mcpInit = initializing
    if initializing {
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

func formatCost(cost float64) string {
	if cost < 0.01 && cost > 0 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// ellipsize shortens a string to max characters, adding … if trimmed
func ellipsize(s string, max int) string {
    if max <= 0 {
        return ""
    }
    r := []rune(s)
    if len(r) <= max {
        return s
    }
    if max <= 1 {
        return string(r[:1])
    }
    return string(r[:max-1]) + "…"
}

// visualWidth returns rune length for simple width calculations
func visualWidth(s string) int {
    return len([]rune(s))
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
		// Update spinner when working or initializing MCP
		if m.working || m.mcpInit {
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

    // Build top content (cwd + usage summary)
    topContent := ""

	// Add current working directory in grey
	if pwd := getCurrentWorkingDirectory(); pwd != "" {
		topContent += styles.MutedStyle.Render(pwd) + "\n\n"
	}

    // Minimalist summary: "3% (4.3K) $0.04"
    percentageText := styles.MutedStyle.Render(fmt.Sprintf("%.0f%%", usagePercent))
    totalTokensText := styles.SubtleStyle.Render(fmt.Sprintf("(%s)", formatTokenCount(totalTokens)))
    costText := styles.MutedStyle.Render(formatCost(m.usage.Cost))
    topContent += fmt.Sprintf("%s %s %s", percentageText, totalTokensText, costText)
	// Add working/initializing indicator if active
	if m.mcpInit || m.working {
		label := "Working..."
		if m.mcpInit {
			label = "Initializing MCP servers..."
		}
		indicator := styles.ActiveStyle.Render(m.spinner.View() + " " + label)
		topContent += "\n" + indicator
	}

	// Get todo content (if any)
	m.todoComp.SetSize(m.width)
	todoContent := m.todoComp.Render()

	// Build per-session breakdown if available
    var sessionsContent string
    if len(m.usage.Breakdown) > 0 {
        active := make(map[string]struct{}, len(m.usage.ActiveSessions))
        for _, id := range m.usage.ActiveSessions {
            active[id] = struct{}{}
        }

        var builder strings.Builder
        builder.WriteString(styles.HighlightStyle.Render("Sessions"))
        for _, row := range m.usage.Breakdown {
            total := row.InputTokens + row.OutputTokens
            name := row.AgentName
            if name == "" {
                name = row.SessionID
            }
            if row.Title != "" {
                name = fmt.Sprintf("%s — %s", name, row.Title)
            }
            prefix := strings.Repeat("  ", row.Depth)
            // Active/inactive indicator
            icon := styles.MutedStyle.Render("○")
            if _, ok := active[row.SessionID]; ok {
                icon = styles.ActiveStyle.Render("●")
            }
            // First line: icon + name, ellipsized to fit the row width
            nameAvail := m.width - visualWidth(prefix) - 2 // icon + space
            if nameAvail < 8 {
                nameAvail = 8
            }
            nameLine := fmt.Sprintf("%s%s %s", prefix, icon, ellipsize(name, nameAvail))

            // Second line: tokens and cost right-aligned in the available width
            tokensPlain := formatTokenCount(total)
            costPlain := formatCost(row.Cost)
            avail := m.width - visualWidth(prefix) - 2 // two extra spaces after prefix
            if avail < 0 {
                avail = 0
            }
            rightLen := visualWidth(tokensPlain) + 2 + visualWidth(costPlain)
            pad := avail - rightLen
            if pad < 1 {
                pad = 1
            }
            secondLine := fmt.Sprintf("%s  %s%s  %s",
                prefix,
                strings.Repeat(" ", pad),
                styles.SubtleStyle.Render(tokensPlain),
                styles.MutedStyle.Render(costPlain),
            )

            builder.WriteString("\n" + styles.BaseStyle.Render(nameLine))
            builder.WriteString("\n" + styles.BaseStyle.Render(secondLine))
        }
        sessionsContent = builder.String()
    }

	if sessionsContent != "" {
		topContent += "\n\n" + sessionsContent
	}

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
