package sidebar

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	// humanize provides comma-separated number formatting.
	"github.com/dustin/go-humanize"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
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

// teamTotalsLabel is the descriptor used when displaying aggregate usage.
const teamTotalsLabel = "Team Total"

// Model represents a sidebar component.
type Model interface {
	layout.Model
	layout.Sizeable

	// SetTokenUsage accepts enriched runtime events for usage tracking.
	SetTokenUsage(event *runtime.TokenUsageEvent)
	SetTodos(toolCall tools.ToolCall) error
	SetWorking(working bool) tea.Cmd
	SetMode(mode Mode)
	GetSize() (width, height int)
}

// model implements Model.
type model struct {
	// width stores the viewport width.
	width int
	// height stores the viewport height.
	height int
	// usageState tracks aggregated usage snapshots.
	usageState usageState
	// todoComp embeds the todo component.
	todoComp *todotool.SidebarComponent
	// working indicates whether the runtime is working.
	working bool
	// mcpInit reports the MCP initialization state.
	mcpInit bool
	// spinner renders the busy indicator.
	spinner spinner.Model
	// mode controls the layout orientation.
	mode Mode
	// sessionTitle keeps the current session title.
	sessionTitle string
	// singleAgentMode indicates whether the agent system contains only one agent.
	singleAgentMode bool
}

// usageState holds aggregated token usage snapshots grouped by agent.
type usageState struct {
	// sessionTotals stores the latest snapshot per session.
	sessionTotals map[string]*runtime.Usage
	// sessionAgents maps session IDs to agent names.
	sessionAgents map[string]string
	// agents keeps an ordered list of agent aggregates.
	agents []*agentUsage
	// agentIndex provides a quick lookup for agent positions.
	agentIndex map[string]int
	// agentNames tracks which agents have reported usage.
	agentNames map[string]struct{}
	// rootInclusive stores the inclusive usage snapshot emitted by the root session.
	rootInclusive *runtime.Usage
	// rootAgentName tracks the resolved root agent name for comparisons.
	rootAgentName string
}

type agentUsage struct {
	name  string
	usage runtime.Usage
}

func New(manager *service.TodoManager, agentCount int) Model {
	return &model{
		// width defaults to the initial layout width.
		width: 20,
		// height defaults to the initial layout height.
		height: 24,
		// usageState initializes usage tracking containers.
		usageState: usageState{
			sessionTotals: make(map[string]*runtime.Usage),
			sessionAgents: make(map[string]string),
			agents:        make([]*agentUsage, 0),
			agentIndex:    make(map[string]int),
			agentNames:    make(map[string]struct{}),
		},

		// todoComp instantiates the todo component.
		todoComp:     todotool.NewSidebarComponent(manager),    
		// spinner configures the busy indicator visuals.
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		// sessionTitle starts with a placeholder.
		sessionTitle:    "New session",
		singleAgentMode: agentCount == 1,
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

// SetTokenUsage updates usage state from runtime events.
func (m *model) SetTokenUsage(event *runtime.TokenUsageEvent) {
	if event == nil {
		// Nothing to update when the event is missing.
		return
	}

	// Legacy fallback: if new fields are missing, use event.Usage for both
	selfUsage := event.SelfUsage
	inclusiveUsage := event.InclusiveUsage
	if (selfUsage == nil || inclusiveUsage == nil) && event.Usage != nil {
		if selfUsage == nil {
			selfUsage = event.Usage
		}
		if inclusiveUsage == nil {
			inclusiveUsage = event.Usage
		}
	}

	agentName := resolveAgentName(event.AgentContext.AgentName, event.SessionID)

	if event.AgentContext.AgentName != "" && m.usageState.rootAgentName == "" {
		m.usageState.rootAgentName = event.AgentContext.AgentName
	}

	if event.SessionID != "" {
		if snapshot := selectSnapshot(selfUsage, inclusiveUsage); snapshot != nil {
			m.updateAgentTotals(agentName, event.SessionID, snapshot)
		}
	}

	if event.AgentContext.AgentName == m.usageState.rootAgentName && inclusiveUsage != nil {
		m.usageState.rootInclusive = cloneUsage(inclusiveUsage)
	}
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

// formatTokenCount formats a token count with grouping separators for readability
func formatTokenCount(count int) string {
	return humanize.Comma(int64(count))
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
func (m *model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := m.SetSize(msg.Width, msg.Height)
		return m, cmd
	case *runtime.MCPInitStartedEvent:
		m.mcpInit = true
		return m, m.spinner.Tick
	case *runtime.MCPInitFinishedEvent:
		m.mcpInit = false
		return m, nil
	case *runtime.SessionTitleEvent:
		m.sessionTitle = msg.Title
		return m, nil
	default:
		if m.working || m.mcpInit {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
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
	gapWidth := m.width - lipgloss.Width(pwd) - lipgloss.Width(usageSummary) - 2
	title := m.sessionTitle + " " + m.workingIndicator()
	return lipgloss.JoinVertical(lipgloss.Top, title, fmt.Sprintf("%s%*s%s", styles.MutedStyle.Render(pwd), gapWidth, "", usageSummary))
}

func (m *model) verticalView() string {
	topContent := m.sessionTitle + "\n"

	if pwd := getCurrentWorkingDirectory(); pwd != "" {
		topContent += styles.MutedStyle.Render(pwd) + "\n\n"
	}

	topContent += m.tokenUsageDetails()
	topContent += "\n" + m.workingIndicator()

	m.todoComp.SetSize(m.width)
	todoContent := strings.TrimSuffix(m.todoComp.Render(), "\n")

	// Calculate available height for content.
	// Account for borders when determining the available height.
	availableHeight := m.height - 2
	topHeight := strings.Count(topContent, "\n") + 1
	todoHeight := strings.Count(todoContent, "\n") + 1

	// Calculate padding needed to push todos to bottom
	paddingHeight := max(availableHeight-topHeight-todoHeight, 0)
	for range paddingHeight {
		topContent += "\n"
	}
	topContent += todoContent

	return styles.BaseStyle.
		Width(m.width).
		Height(m.height-2).
		Align(lipgloss.Left, lipgloss.Top).
		Render(topContent)
}

func (m *model) workingIndicator() string {
	if m.mcpInit || m.working {
		label := "Working..."
		if m.mcpInit {
			label = "Initializing MCP servers..."
		}
		indicator := styles.ActiveStyle.Render(m.spinner.View() + label)
		return indicator
	}

	return ""
}

// tokenUsageSummary generates a condensed usage view for horizontal layout.
func (m *model) tokenUsageSummary() string {
	totals := m.renderTotals()
	label := teamTotalsLabel
	totalTokens := formatTokenCount(totals.InputTokens + totals.OutputTokens)
	cost := fmt.Sprintf("$%.2f", totals.Cost)

	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	parts = append(parts, fmt.Sprintf("Tokens: %s", totalTokens))
	parts = append(parts, fmt.Sprintf("Cost: %s", cost))
	if percentageText, ok := m.singleAgentPercentageText(totals); ok {
		parts = append(parts, fmt.Sprintf("Context: %s", percentageText))
	}

	return styles.SubtleStyle.Render(strings.Join(parts, " | "))
}

// tokenUsageDetails renders the aggregate usage summary line and breakdown.
func (m *model) tokenUsageDetails() string {
	// Determine the aggregate totals.
	totals := m.renderTotals()
	label := teamTotalsLabel
	// Sum user and assistant tokens for display.
	totalTokens := totals.InputTokens + totals.OutputTokens

	// Assemble the multi-line output.
	var builder strings.Builder
	builder.WriteString(styles.SubtleStyle.Render("Total Usage"))
	if label != "" {
		builder.WriteString(fmt.Sprintf(" (%s)", label))
	}
	contextSuffix := ""
	if percentageText, ok := m.singleAgentPercentageText(totals); ok {
		contextSuffix = fmt.Sprintf(" | Context: %s", percentageText)
	}
	builder.WriteString(fmt.Sprintf("\n  Tokens: %s | Cost: $%.2f%s\n", formatTokenCount(totalTokens), totals.Cost, contextSuffix))
	builder.WriteString("--------------------------------\n")
	builder.WriteString(styles.SubtleStyle.Render("Agent Breakdown"))

	breakdown := m.sessionBreakdownLines()
	if len(breakdown) > 0 {
		builder.WriteString("\n")
		builder.WriteString(strings.Join(breakdown, "\n\n"))
	} else {
		builder.WriteString("\n  No session usage yet")
	}

	return builder.String()
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

// cloneUsage copies runtime usage snapshots safely.
func cloneUsage(u *runtime.Usage) *runtime.Usage {
	if u == nil {
		return nil
	}
	clone := *u
	return &clone
}

func selectSnapshot(primary, fallback *runtime.Usage) *runtime.Usage {
	if primary != nil {
		return cloneUsage(primary)
	}
	return cloneUsage(fallback)
}

func resolveAgentName(agentName, sessionID string) string {
	if agentName != "" {
		return agentName
	}
	return sessionID
}

func (m *model) updateAgentTotals(agentName, sessionID string, snapshot *runtime.Usage) {
	if sessionID == "" || snapshot == nil {
		return
	}

	prev := m.usageState.sessionTotals[sessionID]
	m.usageState.sessionTotals[sessionID] = cloneUsage(snapshot)

	if agentName == "" {
		agentName = m.usageState.sessionAgents[sessionID]
	} else {
		m.usageState.sessionAgents[sessionID] = agentName
	}
	if agentName == "" {
		return
	}
	m.usageState.agentNames[agentName] = struct{}{}

	agent := m.ensureAgent(agentName)
	applyUsageDelta(&agent.usage, snapshot, prev)
}

func (m *model) ensureAgent(agentName string) *agentUsage {
	if idx, ok := m.usageState.agentIndex[agentName]; ok {
		return m.usageState.agents[idx]
	}
	entry := &agentUsage{name: agentName}
	m.usageState.agentIndex[agentName] = len(m.usageState.agents)
	m.usageState.agents = append(m.usageState.agents, entry)
	return entry
}

func applyUsageDelta(target *runtime.Usage, next, prev *runtime.Usage) {
	if target == nil || next == nil {
		return
	}
	target.InputTokens += next.InputTokens
	target.OutputTokens += next.OutputTokens
	target.ContextLength += next.ContextLength
	if next.ContextLimit > target.ContextLimit {
		target.ContextLimit = next.ContextLimit
	}
	target.Cost += next.Cost

	if prev == nil {
		return
	}
	target.InputTokens -= prev.InputTokens
	target.OutputTokens -= prev.OutputTokens
	target.ContextLength -= prev.ContextLength
	target.Cost -= prev.Cost
}

// renderTotals resolves the totals for display.
func (m *model) renderTotals() *runtime.Usage {
	totals := m.computeTeamTotals()
	if totals == nil {
		totals = &runtime.Usage{}
	}

	return totals
}

// computeTeamTotals derives aggregate totals for the team line.
func (m *model) computeTeamTotals() *runtime.Usage {
	if len(m.usageState.agents) == 0 {
		return cloneUsage(m.usageState.rootInclusive)
	}

	var totals runtime.Usage
	for _, agent := range m.usageState.agents {
		if agent == nil {
			continue
		}
		totals.InputTokens += agent.usage.InputTokens
		totals.OutputTokens += agent.usage.OutputTokens
		totals.ContextLength += agent.usage.ContextLength
		if agent.usage.ContextLimit > totals.ContextLimit {
			totals.ContextLimit = agent.usage.ContextLimit
		}
		totals.Cost += agent.usage.Cost
	}

	return &totals
}

// sessionBreakdownLines renders per-agent self usage rows.
func (m *model) sessionBreakdownLines() []string {
	lines := make([]string, 0, len(m.usageState.agents)+1)

	if rootBlock := m.rootSessionBlock(); rootBlock != "" {
		lines = append(lines, rootBlock)
	}

	for _, agent := range m.usageState.agents {
		if agent == nil || agent.name == "" || agent.name == m.usageState.rootAgentName {
			continue
		}
		block := formatSessionBlock(agent.name, &agent.usage)
		if block != "" {
			lines = append(lines, block)
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return lines
}

// rootSessionBlock formats the root agent entry with aggregated usage.
func (m *model) rootSessionBlock() string {
	var rootUsage *runtime.Usage
	if idx, ok := m.usageState.agentIndex[m.usageState.rootAgentName]; ok {
		rootUsage = cloneUsage(&m.usageState.agents[idx].usage)
	}
	if rootUsage == nil {
		rootUsage = cloneUsage(m.usageState.rootInclusive)
	}
	if rootUsage == nil {
		return ""
	}

	name := m.usageState.rootAgentName
	if name == "" {
		name = "Root"
	}
	return formatSessionBlock(name, rootUsage)
}

// formatSessionBlock renders a single usage block for an agent.
func formatSessionBlock(agentName string, usage *runtime.Usage) string {
	if usage == nil {
		return ""
	}

	block := fmt.Sprintf("  %s\n     Tokens: %s | Cost: $%.2f", agentName, formatTokenCount(usage.InputTokens+usage.OutputTokens), usage.Cost)
	return block
}

// singleAgentPercentageText computes a context usage percentage when only one agent is active.
func (m *model) singleAgentPercentageText(totals *runtime.Usage) (string, bool) {
	if !m.singleAgentMode {
		return "", false
	}
	if totals == nil || totals.ContextLimit <= 0 {
		return "", false
	}
	if !m.isSingleAgentView() {
		return "", false
	}

	contextTokens := totals.ContextLength
	if contextTokens == 0 {
		contextTokens = totals.InputTokens + totals.OutputTokens
	}
	usagePercent := (float64(contextTokens) / float64(totals.ContextLimit)) * 100
	if usagePercent < 0 {
		usagePercent = 0
	}
	if usagePercent > 100 {
		usagePercent = 100
	}

	return styles.MutedStyle.Render(fmt.Sprintf("%.0f%%", usagePercent)), true
}

// isSingleAgentView returns true when only a single agent has reported usage.
func (m *model) isSingleAgentView() bool {
	if !m.singleAgentMode {
		return false
	}
	if len(m.usageState.agentNames) == 0 {
		return false
	}
	if len(m.usageState.agentNames) > 1 {
		return false
	}
	return true
}
