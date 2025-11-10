package sidebar

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize" // provides comma-separated number formatting

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

// Model represents a sidebar component
type Model interface { // interface defines sidebar contract
	layout.Model
	layout.Sizeable

	SetTokenUsage(event *runtime.TokenUsageEvent) // accepts enriched runtime events for usage tracking
	SetTodos(toolCall tools.ToolCall) error
	SetWorking(working bool) tea.Cmd
	SetMode(mode Mode)
	GetSize() (width, height int)
}

// model implements Model
type model struct { // tea model for sidebar component
	width        int             // viewport width
	height       int             // viewport height
	usageState   usageState      // aggregated usage tracking state
	todoComp     *todotool.SidebarComponent // embedded todo component
	working      bool            // indicates if runtime is working
	mcpInit      bool            // indicates MCP initialization state
	spinner      spinner.Model   // spinner for busy indicator
	mode         Mode            // layout mode
	sessionTitle string          // current session title
}

type usageState struct { // holds aggregated token usage snapshots grouped by agent
	sessionTotals map[string]*runtime.Usage // latest snapshot per session
	sessionAgents map[string]string         // sessionID -> agent name
	agents        []*agentUsage             // ordered list of agent aggregates
	agentIndex    map[string]int            // quick lookup for agent position
	rootInclusive *runtime.Usage            // inclusive usage snapshot emitted by root (fallback)
	rootAgentName string                    // resolved root agent name for comparisons
	activeAgent   string                    // currently active agent for highlighting
}

type agentUsage struct {
	name  string
	usage runtime.Usage
}

func New(manager *service.TodoManager) Model {
	return &model{
		width:  20, // default width matches initial layout
		height: 24, // default height matches initial layout
		usageState: usageState{ // initialize usage tracking containers
			sessionTotals: make(map[string]*runtime.Usage),
			sessionAgents: make(map[string]string),
			agents:        make([]*agentUsage, 0),
			agentIndex:    make(map[string]int),
		},
		todoComp:     todotool.NewSidebarComponent(manager),                      // instantiate todo component
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)), // configure spinner visuals
		sessionTitle: "New session",                                 // initial placeholder title
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) SetTokenUsage(event *runtime.TokenUsageEvent) { // updates usage state from runtime events
	if event == nil { // guard against nil events
		return // nothing to do when event missing
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

	agentName := event.AgentContext.AgentName
	if agentName == "" {
		agentName = event.SessionID
	}

	if event.AgentContext.AgentName != "" && m.usageState.rootAgentName == "" {
		m.usageState.rootAgentName = event.AgentContext.AgentName
	}
	if agentName != "" {
		m.usageState.activeAgent = agentName
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

	// Calculate available height for content
	availableHeight := m.height - 2 // Account for borders
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

func (m *model) tokenUsageSummary() string { // condensed single-line usage view for horizontal layout
	label, totals := m.renderTotals()
	totalTokens := formatTokenCount(totals.InputTokens + totals.OutputTokens)
	cost := fmt.Sprintf("$%.2f", totals.Cost)

	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	parts = append(parts, fmt.Sprintf("Tokens: %s", totalTokens))
	parts = append(parts, fmt.Sprintf("Cost: %s", cost))

	return styles.SubtleStyle.Render(strings.Join(parts, " | "))
}

func (m *model) tokenUsageDetails() string { // renders aggregate usage summary line + breakdown
	label, totals := m.renderTotals()                       // get friendly label plus computed totals
	totalTokens := totals.InputTokens + totals.OutputTokens // sum user + assistant tokens for display

	// var usagePercent float64
	// if totals.ContextLimit > 0 {
	// 	usagePercent = (float64(totals.ContextLength) / float64(totals.ContextLimit)) * 100
	// }
	// percentageText := styles.MutedStyle.Render(fmt.Sprintf("%.0f%%", usagePercent))

	var builder strings.Builder                                   // assemble multiline output
	builder.WriteString(styles.SubtleStyle.Render("TOTAL USAGE")) // heading for total usage
	if label != "" {                                              // append contextual label when available
		builder.WriteString(fmt.Sprintf(" (%s)", label)) // show whether totals are team/session scoped
	}
	builder.WriteString(fmt.Sprintf("\n  Tokens: %s | Cost: $%.2f\n", formatTokenCount(totalTokens), totals.Cost)) // display totals line
	builder.WriteString("--------------------------------\n")                                                      // visual separator
	builder.WriteString(styles.SubtleStyle.Render("SESSION BREAKDOWN"))                                            // heading for per-session details

	breakdown := m.sessionBreakdownLines() // fetch breakdown blocks
	if len(breakdown) > 0 {                // append breakdown when data available
		builder.WriteString("\n")                            // ensure newline before blocks
		builder.WriteString(strings.Join(breakdown, "\n\n")) // place blank line between blocks
	} else {
		builder.WriteString("\n  No session usage yet") // fallback text when no sessions reported
	}

	return builder.String() // return composed view
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

func cloneUsage(u *runtime.Usage) *runtime.Usage { // helper to copy runtime usage structs safely
	if u == nil { // avoid panics on nil usage snapshots
		return nil // nothing to clone when nil
	}
	clone := *u   // copy by value to detach from original pointer
	return &clone // return pointer to independent copy
}

func selectSnapshot(primary, fallback *runtime.Usage) *runtime.Usage {
	if primary != nil {
		return cloneUsage(primary)
	}
	return cloneUsage(fallback)
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

func (m *model) renderTotals() (string, *runtime.Usage) { // resolves label + totals for display
	totals := m.computeTeamTotals() // compute aggregate usage first
	if totals == nil {              // ensure downstream code always receives a struct
		totals = &runtime.Usage{} // fall back to zero snapshot
	}

	label := "Team Total" // totals always represent team-wide cumulative usage

	return label, totals // return computed label with totals
}

func (m *model) computeTeamTotals() *runtime.Usage { // derives aggregate totals for the team line
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

func (m *model) sessionBreakdownLines() []string { // renders per-agent self usage rows
	lines := make([]string, 0, len(m.usageState.agents)+1)

	if rootBlock := m.rootSessionBlock(); rootBlock != "" {
		lines = append(lines, rootBlock)
	}

	for _, agent := range m.usageState.agents {
		if agent == nil || agent.name == "" || agent.name == m.usageState.rootAgentName {
			continue
		}
		block := formatSessionBlock(agent.name, &agent.usage, agent.name == m.usageState.activeAgent)
		if block != "" {
			lines = append(lines, block)
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return lines
}

func (m *model) rootSessionBlock() string { // formats root agent entry with aggregated usage
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
	return formatSessionBlock(name, rootUsage, m.usageState.activeAgent == name)
}

func formatSessionBlock(agentName string, usage *runtime.Usage, isActive bool) string { // helper to render a single block
	if usage == nil {
		return ""
	}

	block := fmt.Sprintf("  %s\n     Tokens: %s | Cost: $%.2f", agentName, formatTokenCount(usage.InputTokens+usage.OutputTokens), usage.Cost)
	if isActive {
		return styles.ActiveStyle.Render(block)
	}
	return block
}
