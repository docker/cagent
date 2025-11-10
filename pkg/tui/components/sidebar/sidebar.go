package sidebar

import (
	"fmt"
	"os"
	"sort" // ensure deterministic breakdown ordering
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

type usageState struct { // holds all token usage snapshots for sidebar
	sessions        map[string]*runtime.Usage // per-session aggregated self usage snapshots
	inclusive       map[string]*runtime.Usage // per-session inclusive (lifetime + live) usage snapshots
	sessionAgents   map[string]string         // optional agent name mapping per session
	rootInclusive   *runtime.Usage            // inclusive usage snapshot emitted by root
	rootSessionID   string                    // session ID associated with root agent
	rootAgentName   string                    // resolved root agent name for comparisons
	activeSessionID string                    // currently active session ID for highlighting
}

func New(manager *service.TodoManager) Model {
	return &model{
		width:  20, // default width matches initial layout
		height: 24, // default height matches initial layout
		usageState: usageState{ // initialize usage tracking containers
			sessions:      make(map[string]*runtime.Usage), // allocate map to avoid nil lookups
			inclusive:     make(map[string]*runtime.Usage), // allocate map for inclusive snapshots
			sessionAgents: make(map[string]string),         // track agent names per session
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

	if event.AgentContext.AgentName != "" && m.usageState.rootAgentName == "" { // capture root agent name from first event
		m.usageState.rootAgentName = event.AgentContext.AgentName // remember orchestrator name to identify later events
	}

	if event.SessionID != "" { // update currently active session ID
		m.usageState.activeSessionID = event.SessionID // track active session for totals/highlighting
	}

	if event.SessionID != "" {
		snapshot := cloneUsage(selfUsage)
		if snapshot == nil {
			snapshot = cloneUsage(inclusiveUsage)
		}
		if snapshot != nil {
			m.usageState.sessions[event.SessionID] = snapshot
		}

		if inclusiveUsage != nil { // store inclusive (lifetime) snapshot per session for legacy fallbacks
			m.usageState.inclusive[event.SessionID] = cloneUsage(inclusiveUsage)
		}
	}

	if event.AgentContext.AgentName != "" && event.SessionID != "" { // map session ID to agent name for breakdown rows
		m.usageState.sessionAgents[event.SessionID] = event.AgentContext.AgentName // remember descriptive label for later rendering
	}

	if event.AgentContext.AgentName == m.usageState.rootAgentName && inclusiveUsage != nil { // update root inclusive snapshot when orchestrator reports
		m.usageState.rootInclusive = cloneUsage(inclusiveUsage) // persist inclusive totals for team view
		if event.SessionID != "" {                              // also note root session ID for comparisons
			m.usageState.rootSessionID = event.SessionID // record root session identifier
		}
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

func (m *model) renderTotals() (string, *runtime.Usage) { // resolves label + totals for display
	totals := m.computeTeamTotals() // compute aggregate usage first
	if totals == nil {              // ensure downstream code always receives a struct
		totals = &runtime.Usage{} // fall back to zero snapshot
	}

	label := "Team Total" // totals always represent team-wide cumulative usage

	return label, totals // return computed label with totals
}

func (m *model) computeTeamTotals() *runtime.Usage { // derives aggregate totals for the team line
	totals := aggregateSelfUsage(m.usageState.sessions)
	if totals != nil {
		return totals
	}
	// Fallback to root inclusive snapshot if we haven't received per-session data yet (e.g., very early)
	return cloneUsage(m.usageState.rootInclusive)
}

func (m *model) sessionBreakdownLines() []string { // renders per-session self usage rows
	idSet := make(map[string]struct{}, len(m.usageState.sessions))
	ids := make([]string, 0, len(m.usageState.sessions))
	for id := range m.usageState.sessions { // iterate known sessions
		idSet[id] = struct{}{}
		ids = append(ids, id) // record id for sorting
	}
	for id := range m.usageState.inclusive {
		if _, seen := idSet[id]; !seen {
			idSet[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		if rootBlock := m.rootSessionBlock(); rootBlock != "" {
			return []string{rootBlock}
		}
		return nil
	}
	sort.Strings(ids) // ensure stable ordering regardless of map iteration

	lines := make([]string, 0, len(ids)+1) // include space for root block

	if rootBlock := m.rootSessionBlock(); rootBlock != "" { // prepend root block when available
		lines = append(lines, rootBlock)
	}

	for _, id := range ids { // build block for each session
		if id == m.usageState.rootSessionID { // skip root session since totals already shown above
			continue
		}
		// Show self-only usage for session rows (lifetime across passes, not inclusive of children)
		usage := m.usageState.sessions[id]
		if usage == nil { // fall back to inclusive snapshot when self-only data unavailable (legacy runtimes)
			usage = m.usageState.inclusive[id]
		}
		if usage == nil { // skip if snapshot still missing
			continue // nothing to render for this id
		}
		agentName := m.usageState.sessionAgents[id] // resolve display name
		if agentName == "" {                        // fallback when agent name unknown
			agentName = id // show session ID as identifier
		}

		if block := formatSessionBlock(agentName, usage, id == m.usageState.activeSessionID); block != "" { // compose + style block
			lines = append(lines, block) // add block to breakdown list
		}
	}

	return lines // return composed rows
}

func (m *model) rootSessionBlock() string { // formats root agent entry with self-only lifetime usage
	// Prefer the self-only snapshot published for the root session; fall back to inclusive, then nothing
	rootUsage := m.usageState.sessions[m.usageState.rootSessionID]
	if rootUsage == nil {
		rootUsage = m.usageState.rootInclusive
	}
	if rootUsage == nil {
		return ""
	}

	name := m.usageState.rootAgentName // prefer configured agent name
	if name == "" {
		name = "Root"
	}

	return formatSessionBlock(name, cloneUsage(rootUsage), m.usageState.activeSessionID == m.usageState.rootSessionID)
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
func aggregateSelfUsage(usages map[string]*runtime.Usage) *runtime.Usage {
	if len(usages) == 0 {
		return nil
	}
	var total runtime.Usage
	for _, usage := range usages {
		if usage == nil {
			continue
		}
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.ContextLength += usage.ContextLength
		if usage.ContextLimit > total.ContextLimit {
			total.ContextLimit = usage.ContextLimit
		}
		total.Cost += usage.Cost
	}
	return &total
}
