package sidebar

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/tool/todotool"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

type Mode int

const (
	ModeVertical Mode = iota
	ModeHorizontal
)

// Model represents a sidebar component
type Model interface {
	layout.Model
	layout.Sizeable

	SetTokenUsage(event *runtime.TokenUsageEvent)
	SetTodos(toolCall tools.ToolCall) error
	SetWorking(working bool) tea.Cmd
	SetMode(mode Mode)
	SetAgentInfo(agentName, model, description string)
	SetTeamInfo(availableAgents []string)
	SetAgentSwitching(switching bool)
	SetToolsetInfo(availableTools int)
	SetToolStatus(toolName, status string)
	GetSize() (width, height int)
}

// ragIndexingState tracks per-strategy indexing progress
type ragIndexingState struct {
	current int
	total   int
	spinner spinner.Spinner
}

// model implements Model
type model struct {
	width            int
	height           int
	sessionUsage     map[string]*runtime.Usage // sessionID -> latest usage snapshot
	sessionAgent     map[string]string         // sessionID -> agent name
	todoComp         *todotool.SidebarComponent
	working          bool
	mcpInit          bool
	ragIndexing      map[string]*ragIndexingState // strategy name -> indexing state
	spinner          spinner.Spinner
	mode             Mode
	sessionTitle     string
	currentAgent     string
	agentModel       string
	agentDescription string
	availableAgents  []string
	agentSwitching   bool
	availableTools   int
	activeTools      []string
	toolExecutions   map[string]string // tool name -> status (running, completed, failed)
}

func New(manager *service.TodoManager) Model {
	return &model{
		width:          20,
		height:         24,
		sessionUsage:   make(map[string]*runtime.Usage),
		sessionAgent:   make(map[string]string),
		todoComp:       todotool.NewSidebarComponent(manager),
		spinner:        spinner.New(spinner.ModeSpinnerOnly),
		sessionTitle:   "New session",
		ragIndexing:    make(map[string]*ragIndexingState),
		toolExecutions: make(map[string]string),
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) SetTokenUsage(event *runtime.TokenUsageEvent) {
	if event == nil || event.Usage == nil || event.SessionID == "" || event.AgentContext.AgentName == "" {
		return
	}

	// Store/replace by session ID (each event has cumulative totals for that session)
	usage := *event.Usage
	m.sessionUsage[event.SessionID] = &usage
	m.sessionAgent[event.SessionID] = event.AgentContext.AgentName
}

func (m *model) SetTodos(toolCall tools.ToolCall) error {
	return m.todoComp.SetTodos(toolCall)
}

// SetWorking sets the working state and returns a command to start the spinner if needed
func (m *model) SetWorking(working bool) tea.Cmd {
	m.working = working
	if working {
		return m.spinner.Init()
	}
	return nil
}

// SetAgentInfo sets the current agent information
func (m *model) SetAgentInfo(agentName, model, description string) {
	m.currentAgent = agentName
	m.agentModel = model
	m.agentDescription = description
}

// SetTeamInfo sets the available agents in the team
func (m *model) SetTeamInfo(availableAgents []string) {
	m.availableAgents = availableAgents
}

// SetAgentSwitching sets whether an agent switch is in progress
func (m *model) SetAgentSwitching(switching bool) {
	m.agentSwitching = switching
}

// SetToolsetInfo sets the number of available tools
func (m *model) SetToolsetInfo(availableTools int) {
	m.availableTools = availableTools
}

// SetToolStatus updates the status of a specific tool
func (m *model) SetToolStatus(toolName, status string) {
	if m.toolExecutions == nil {
		m.toolExecutions = make(map[string]string)
	}

	// Update tool status
	m.toolExecutions[toolName] = status

	// Update active tools list
	m.activeTools = nil
	for tool, stat := range m.toolExecutions {
		if stat == "running" {
			m.activeTools = append(m.activeTools, tool)
		}
	}
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
	return fmt.Sprintf("%.2f", cost)
}

// contextPercent returns a context usage percentage string when a single session has a limit.
func (m *model) contextPercent() (string, bool) {
	if len(m.sessionUsage) != 1 {
		return "", false
	}
	for _, usage := range m.sessionUsage {
		if usage.ContextLimit > 0 {
			percent := (float64(usage.ContextLength) / float64(usage.ContextLimit)) * 100
			return fmt.Sprintf("Context: %.0f%%", percent), true
		}
	}
	return "", false
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

// Update handles messages and updates the component state.
func (m *model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := m.SetSize(msg.Width, msg.Height)
		return m, cmd
	case *runtime.TokenUsageEvent:
		m.SetTokenUsage(msg)
		return m, nil
	case *runtime.MCPInitStartedEvent:
		m.mcpInit = true
		return m, m.spinner.Init()
	case *runtime.MCPInitFinishedEvent:
		m.mcpInit = false
		return m, nil
	case *runtime.RAGIndexingStartedEvent:
		// Use composite key: "ragName/strategyName" to differentiate strategies within same RAG manager
		key := msg.RAGName + "/" + msg.StrategyName
		slog.Debug("Sidebar received RAG indexing started event", "rag", msg.RAGName, "strategy", msg.StrategyName, "key", key)
		state := &ragIndexingState{
			spinner: spinner.New(spinner.ModeSpinnerOnly),
		}
		m.ragIndexing[key] = state
		return m, state.spinner.Init()
	case *runtime.RAGIndexingProgressEvent:
		key := msg.RAGName + "/" + msg.StrategyName
		slog.Debug("Sidebar received RAG indexing progress event", "rag", msg.RAGName, "strategy", msg.StrategyName, "current", msg.Current, "total", msg.Total)
		if state, exists := m.ragIndexing[key]; exists {
			state.current = msg.Current
			state.total = msg.Total
		}
		return m, nil
	case *runtime.RAGIndexingCompletedEvent:
		key := msg.RAGName + "/" + msg.StrategyName
		slog.Debug("Sidebar received RAG indexing completed event", "rag", msg.RAGName, "strategy", msg.StrategyName)
		delete(m.ragIndexing, key)
		return m, nil
	case *runtime.RAGReadyEvent:
		// Kept for backward compatibility, but indexing_completed now handles this
		// For RAGReadyEvent, we don't have strategy name, so delete all keys with this RAG name prefix
		slog.Debug("Sidebar received RAG ready event", "rag", msg.RAGName)
		for key := range m.ragIndexing {
			if strings.HasPrefix(key, msg.RAGName+"/") {
				delete(m.ragIndexing, key)
			}
		}
		return m, nil
	case *runtime.SessionTitleEvent:
		m.sessionTitle = msg.Title
		return m, nil
	case *runtime.AgentInfoEvent:
		m.SetAgentInfo(msg.AgentName, msg.Model, msg.Description)
		return m, nil
	case *runtime.TeamInfoEvent:
		m.SetTeamInfo(msg.AvailableAgents)
		return m, nil
	case *runtime.AgentSwitchingEvent:
		m.SetAgentSwitching(msg.Switching)
		return m, nil
	case *runtime.ToolsetInfoEvent:
		m.SetToolsetInfo(msg.AvailableTools)
		return m, nil
	case *runtime.ToolStatusEvent:
		m.SetToolStatus(msg.ToolName, msg.Status)
		return m, nil
	default:
		var cmds []tea.Cmd

		// Update main spinner for working/mcpInit states
		if m.working || m.mcpInit {
			var cmd tea.Cmd
			var model layout.Model
			model, cmd = m.spinner.Update(msg)
			m.spinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}

		// Update each RAG indexing spinner
		for _, state := range m.ragIndexing {
			var cmd tea.Cmd
			var model layout.Model
			model, cmd = state.spinner.Update(msg)
			state.spinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}

		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil
	}
}

// View renders the component
func (m *model) View() string {
	if m.mode == ModeVertical {
		return m.verticalView()
	}

	return m.horizontalView()
}

func (m *model) horizontalView() string {
	pwd := getCurrentWorkingDirectory()
	usageSummary := m.tokenUsageSummary()

	wi := m.workingIndicatorHorizontal()
	titleGapWidth := m.width - lipgloss.Width(m.sessionTitle) - lipgloss.Width(wi) - 2
	title := fmt.Sprintf("%s%*s%s", m.sessionTitle, titleGapWidth, "", wi)

	gapWidth := m.width - lipgloss.Width(pwd) - lipgloss.Width(usageSummary) - 2
	return lipgloss.JoinVertical(lipgloss.Top, title, fmt.Sprintf("%s%*s%s", styles.MutedStyle.Render(pwd), gapWidth, "", usageSummary))
}

func (m *model) verticalView() string {
	topContent := m.sessionTitle + "\n"

	if pwd := getCurrentWorkingDirectory(); pwd != "" {
		topContent += styles.MutedStyle.Render(pwd) + "\n\n"
	}

	topContent += m.tokenUsage()
	topContent += "\n" + m.workingIndicator()

	// Add agent information
	if agentInfo := m.agentInfo(); agentInfo != "" {
		topContent += "\n\n" + agentInfo
	}

	// Add toolset information
	if toolsetInfo := m.toolsetInfo(); toolsetInfo != "" {
		topContent += "\n\n" + toolsetInfo
	}

	m.todoComp.SetSize(m.width)
	todoContent := strings.TrimSuffix(m.todoComp.Render(), "\n")
	if todoContent != "" {
		topContent += "\n\n" + todoContent
	}

	return styles.BaseStyle.
		Width(m.width).
		Height(m.height-2).
		Align(lipgloss.Left, lipgloss.Top).
		Render(topContent)
}

func (m *model) workingIndicator() string {
	var indicators []string

	// Add working indicator if agent is processing
	if m.working {
		indicators = append(indicators, styles.ActiveStyle.Render(m.spinner.View()+" "+"Working..."))
	}

	// Add MCP init indicator if initializing
	if m.mcpInit {
		indicators = append(indicators, styles.ActiveStyle.Render(m.spinner.View()+" "+"Initializing MCP servers..."))
	}

	// Add RAG indexing indicators for each active indexing operation
	// Group strategies by RAG source to avoid repeating the source name
	ragGroups := make(map[string][]struct {
		strategyName string
		state        *ragIndexingState
	})

	for key, state := range m.ragIndexing {
		parts := strings.Split(key, "/")
		if len(parts) == 2 {
			ragName := parts[0]
			if ragGroups[ragName] == nil {
				ragGroups[ragName] = []struct {
					strategyName string
					state        *ragIndexingState
				}{}
			}
			ragGroups[ragName] = append(ragGroups[ragName], struct {
				strategyName string
				state        *ragIndexingState
			}{parts[1], state})
		}
	}

	// Display each RAG source with its strategies (sorted for stable display)
	ragNames := make([]string, 0, len(ragGroups))
	for ragName := range ragGroups {
		ragNames = append(ragNames, ragName)
	}
	sort.Strings(ragNames)

	for _, ragName := range ragNames {
		strategies := ragGroups[ragName]
		displayRagName := strings.ReplaceAll(ragName, "_", " ")

		// Sort strategies by name for stable display
		sort.Slice(strategies, func(i, j int) bool {
			return strategies[i].strategyName < strategies[j].strategyName
		})

		// First line: RAG source name with spinner
		spinnerView := strategies[0].state.spinner.View()
		ragNameStyled := styles.BoldStyle.Render(displayRagName)
		line1 := fmt.Sprintf("%s Indexing %s", spinnerView, ragNameStyled)
		indicators = append(indicators, styles.ActiveStyle.Render(line1))

		// Following lines: each strategy with progress, indented with subtle emphasis
		for _, strategy := range strategies {
			displayStratName := strings.ReplaceAll(strategy.strategyName, "-", " ")
			progress := ""
			if strategy.state.total > 0 {
				progress = fmt.Sprintf(" [%d/%d]", strategy.state.current, strategy.state.total)
			}
			stratNameStyled := styles.BoldStyle.Render(displayStratName)
			line := fmt.Sprintf("  • %s%s", stratNameStyled, progress)
			indicators = append(indicators, line)
		}
	}

	if len(indicators) == 0 {
		return ""
	}

	return strings.Join(indicators, "\n")
}

// workingIndicatorHorizontal returns a single-line version of the working indicator for horizontal mode
func (m *model) workingIndicatorHorizontal() string {
	var labels []string

	// Add working indicator if agent is processing
	if m.working {
		labels = append(labels, "Working...")
	}

	// Add MCP init indicator if initializing
	if m.mcpInit {
		labels = append(labels, "Initializing MCP servers...")
	}

	// Add RAG indexing labels for each active indexing operation
	// Group strategies by RAG source to avoid repeating the source name
	ragGroups := make(map[string][]struct {
		strategyName string
		state        *ragIndexingState
	})

	for key, state := range m.ragIndexing {
		parts := strings.Split(key, "/")
		if len(parts) == 2 {
			ragName := parts[0]
			if ragGroups[ragName] == nil {
				ragGroups[ragName] = []struct {
					strategyName string
					state        *ragIndexingState
				}{}
			}
			ragGroups[ragName] = append(ragGroups[ragName], struct {
				strategyName string
				state        *ragIndexingState
			}{parts[1], state})
		}
	}

	// Display each RAG source with its strategies (sorted for stable display)
	ragNames := make([]string, 0, len(ragGroups))
	for ragName := range ragGroups {
		ragNames = append(ragNames, ragName)
	}
	sort.Strings(ragNames)

	for _, ragName := range ragNames {
		strategies := ragGroups[ragName]
		displayRagName := strings.ReplaceAll(ragName, "_", " ")

		// Sort strategies by name for stable display
		sort.Slice(strategies, func(i, j int) bool {
			return strategies[i].strategyName < strategies[j].strategyName
		})

		// First line: RAG source name (bold for emphasis)
		ragNameStyled := styles.BoldStyle.Render(displayRagName)
		labels = append(labels, fmt.Sprintf("Indexing %s", ragNameStyled))

		// Following lines: each strategy with progress, indented with bullet
		for _, strategy := range strategies {
			displayStratName := strings.ReplaceAll(strategy.strategyName, "-", " ")
			progress := ""
			if strategy.state.total > 0 {
				progress = fmt.Sprintf(" [%d/%d]", strategy.state.current, strategy.state.total)
			}
			stratNameStyled := styles.BoldStyle.Render(displayStratName)
			labels = append(labels, fmt.Sprintf("  • %s%s", stratNameStyled, progress))
		}
	}

	if len(labels) == 0 {
		return ""
	}

	// For horizontal mode, show all labels separated by " | "
	label := strings.Join(labels, " | ")
	return styles.ActiveStyle.Render(m.spinner.View() + " " + label)
}

func (m *model) tokenUsage() string {
	if len(m.sessionUsage) == 0 {
		return ""
	}

	var totalTokens int
	var totalCost float64
	for _, usage := range m.sessionUsage {
		totalTokens += usage.InputTokens + usage.OutputTokens
		totalCost += usage.Cost
	}

	agentTotals := make(map[string]*runtime.Usage)
	for sessionID, usage := range m.sessionUsage {
		agent := m.sessionAgent[sessionID]
		if agent == "" {
			continue
		}
		if existing, ok := agentTotals[agent]; ok {
			existing.InputTokens += usage.InputTokens
			existing.OutputTokens += usage.OutputTokens
			existing.Cost += usage.Cost
		} else {
			u := *usage
			agentTotals[agent] = &u
		}
	}

	var b strings.Builder
	b.WriteString(styles.HighlightStyle.Render("TOTAL USAGE"))
	b.WriteString("\n  ")
	line := fmt.Sprintf("Tokens: %s | Cost: $%s", formatTokenCount(totalTokens), formatCost(totalCost))
	if ctxText, ok := m.contextPercent(); ok {
		line = fmt.Sprintf("%s | %s", line, ctxText)
	}
	b.WriteString(line)

	b.WriteString("\n--------------------------------\n")
	b.WriteString(styles.HighlightStyle.Render("SESSION BREAKDOWN"))

	agentNames := make([]string, 0, len(agentTotals))
	for name := range agentTotals {
		agentNames = append(agentNames, name)
	}
	sort.Strings(agentNames)

	if len(agentNames) == 0 {
		b.WriteString("\n  ")
		b.WriteString(styles.SubtleStyle.Render("No usage yet"))
		return b.String()
	}

	for _, name := range agentNames {
		usage := agentTotals[name]
		tokens := usage.InputTokens + usage.OutputTokens
		b.WriteString(fmt.Sprintf("\n  %s", styles.SubtleStyle.Render(name)))
		b.WriteString(fmt.Sprintf("\n    Tokens: %s | Cost: $%s", formatTokenCount(tokens), formatCost(usage.Cost)))
	}

	return b.String()
}

// tokenUsageSummary returns a single-line summary for horizontal layout.
func (m *model) tokenUsageSummary() string {
	if len(m.sessionUsage) == 0 {
		return ""
	}

	var totalTokens int
	var totalCost float64
	for _, usage := range m.sessionUsage {
		totalTokens += usage.InputTokens + usage.OutputTokens
		totalCost += usage.Cost
	}

	if ctxText, ok := m.contextPercent(); ok {
		return fmt.Sprintf("Tokens: %s | Cost: $%s | %s", formatTokenCount(totalTokens), formatCost(totalCost), ctxText)
	}

	return fmt.Sprintf("Tokens: %s | Cost: $%s", formatTokenCount(totalTokens), formatCost(totalCost))
}

// agentInfo renders the current agent information
func (m *model) agentInfo() string {
	if m.currentAgent == "" {
		return ""
	}

	var content strings.Builder

	// Agent name with highlight and switching indicator
	agentTitle := "AGENT"
	if m.agentSwitching {
		agentTitle = "AGENT ↔" // switching indicator
	}
	content.WriteString(styles.HighlightStyle.Render(agentTitle))
	content.WriteString("\n")

	// Current agent name
	agentName := m.currentAgent
	if m.agentSwitching {
		agentName = "⟳ " + agentName // switching icon
	}
	content.WriteString(styles.MutedStyle.Render(agentName))

	// Team info if multiple agents available
	if len(m.availableAgents) > 1 {
		content.WriteString("\n")
		teamInfo := fmt.Sprintf("Team: %d agents", len(m.availableAgents))
		content.WriteString(styles.SubtleStyle.Render(teamInfo))
	}

	// Model info if available
	if m.agentModel != "" {
		content.WriteString("\n")
		content.WriteString(styles.SubtleStyle.Render("Model: " + m.agentModel))
	}

	// Agent description if available
	if m.agentDescription != "" {
		content.WriteString("\n")

		// Truncate description for sidebar display
		description := m.agentDescription
		maxDescWidth := max(m.width-4, 20) // Leave margin for styling
		if len(description) > maxDescWidth {
			description = description[:maxDescWidth-3] + "..."
		}

		content.WriteString(styles.SubtleStyle.Render(description))
	}

	return content.String()
}

// toolsetInfo renders the current toolset status information
func (m *model) toolsetInfo() string {
	if m.availableTools == 0 {
		return ""
	}

	var content strings.Builder

	// Tools header
	content.WriteString(styles.HighlightStyle.Render("TOOLS"))
	content.WriteString("\n")

	// Available tools count
	content.WriteString(styles.MutedStyle.Render(fmt.Sprintf("%d tools available", m.availableTools)))

	// Active/running tools
	if len(m.activeTools) > 0 {
		content.WriteString("\n")
		for i, tool := range m.activeTools {
			if i > 0 {
				content.WriteString(", ")
			}
			content.WriteString(styles.ActiveStyle.Render("[>] " + tool))
		}
	}

	// Tool execution summary
	runningCount := len(m.activeTools)
	completedCount := 0
	failedCount := 0

	for _, status := range m.toolExecutions {
		switch status {
		case "completed":
			completedCount++
		case "failed":
			failedCount++
		}
	}

	if runningCount > 0 || completedCount > 0 || failedCount > 0 {
		content.WriteString("\n")
		statusParts := []string{}

		if runningCount > 0 {
			statusParts = append(statusParts, fmt.Sprintf("%d running", runningCount))
		}
		if completedCount > 0 {
			statusParts = append(statusParts, fmt.Sprintf("%d completed", completedCount))
		}
		if failedCount > 0 {
			statusParts = append(statusParts, fmt.Sprintf("%d failed", failedCount))
		}

		if len(statusParts) > 0 {
			content.WriteString(styles.SubtleStyle.Render(strings.Join(statusParts, ", ")))
		}
	}

	return content.String()
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

func (m *model) SetMode(mode Mode) {
	m.mode = mode
}
