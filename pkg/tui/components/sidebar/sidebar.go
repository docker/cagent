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
	"github.com/docker/cagent/pkg/tui/components/todo"
	"github.com/docker/cagent/pkg/tui/core/layout"
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
	todoComp     *todo.Component // embedded todo component
	working      bool            // indicates if runtime is working
	mcpInit      bool            // indicates MCP initialization state
	spinner      spinner.Model   // spinner for busy indicator
	mode         Mode            // layout mode
	sessionTitle string          // current session title
}

type usageState struct { // holds all token usage snapshots for sidebar
	sessions        map[string]*runtime.Usage // per-session self usage snapshots
	sessionAgents   map[string]string         // optional agent name mapping per session
	rootInclusive   *runtime.Usage            // inclusive usage snapshot emitted by root
	rootSessionID   string                    // session ID associated with root agent
	rootAgentName   string                    // resolved root agent name for comparisons
	activeSessionID string                    // currently active session ID for highlighting
}

func New() Model {
	return &model{
		width:  20, // default width matches initial layout
		height: 24, // default height matches initial layout
		usageState: usageState{ // initialize usage tracking containers
			sessions:      make(map[string]*runtime.Usage), // allocate map to avoid nil lookups
			sessionAgents: make(map[string]string),         // track agent names per session
		},
		todoComp:     todo.NewComponent(),                           // instantiate todo component
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

	if event.AgentContext.AgentName != "" && m.usageState.rootAgentName == "" { // capture root agent name from first event
		m.usageState.rootAgentName = event.AgentContext.AgentName // remember orchestrator name to identify later events
	}

	if event.SessionID != "" { // update currently active session ID
		m.usageState.activeSessionID = event.SessionID // track active session for totals/highlighting
	}

	if event.SelfUsage != nil && event.SessionID != "" { // store self snapshot per session
		m.usageState.sessions[event.SessionID] = cloneUsage(event.SelfUsage) // clone to avoid aliasing runtime memory
	}

	if event.AgentContext.AgentName != "" && event.SessionID != "" { // map session ID to agent name for breakdown rows
		m.usageState.sessionAgents[event.SessionID] = event.AgentContext.AgentName // remember descriptive label for later rendering
	}

	if event.AgentContext.AgentName == m.usageState.rootAgentName && event.InclusiveUsage != nil { // update root inclusive snapshot when orchestrator reports
		m.usageState.rootInclusive = cloneUsage(event.InclusiveUsage) // persist inclusive totals for team view
		if event.SessionID != "" {                                    // also note root session ID for comparisons
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
	gapWidth := m.width - lipgloss.Width(pwd) - lipgloss.Width(m.tokenUsage()) - 2
	title := m.sessionTitle + " " + m.workingIndicator()
	return lipgloss.JoinVertical(lipgloss.Top, title, fmt.Sprintf("%s%*s%s", styles.MutedStyle.Render(pwd), gapWidth, "", m.tokenUsage()))
}

func (m *model) verticalView() string {
	topContent := m.sessionTitle

	if pwd := getCurrentWorkingDirectory(); pwd != "" {
		topContent += styles.MutedStyle.Render(pwd) + "\n\n"
	}

	topContent += m.tokenUsage()
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

func (m *model) tokenUsage() string { // renders aggregate usage summary line + breakdown
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

	label := "Session Total"               // default label when only one session present
	if m.usageState.rootInclusive != nil { // when root inclusive exists we can show team wording
		label = "Team Total"                                                                                  // highlight that totals represent the whole team
		if m.usageState.activeSessionID != "" && m.usageState.activeSessionID != m.usageState.rootSessionID { // active child contributes live usage
			label = "Team Total (incl. active child)" // clarify that active child is included
		}
	}

	return label, totals // return computed label with totals
}

func (m *model) computeTeamTotals() *runtime.Usage { // derives aggregate totals for the team line
	base := cloneUsage(m.usageState.rootInclusive) // start with root inclusive snapshot, if any
	active := m.currentSessionUsage()              // get self usage for currently active session

	if base == nil { // when root has not reported yet
		return cloneUsage(active) // either return active session usage or nil
	}

	if active != nil && m.usageState.activeSessionID != "" && m.usageState.activeSessionID != m.usageState.rootSessionID { // only add active child when it differs from root session
		base = mergeUsageTotals(base, active) // merge child self usage into inclusive total for live view
	}

	return base // return computed totals (may still be nil if nothing reported)
}

func (m *model) currentSessionUsage() *runtime.Usage { // fetches usage snapshot for active session
	if m.usageState.activeSessionID == "" { // when no active session tracked
		return nil // nothing to return
	}
	return m.usageState.sessions[m.usageState.activeSessionID] // look up snapshot in map (may be nil)
}

func mergeUsageTotals(base, delta *runtime.Usage) *runtime.Usage { // adds token/cost fields from delta into base
	if base == nil { // handle nil base by cloning delta
		return cloneUsage(delta) // ensure caller gets independent struct
	}
	if delta == nil { // nothing to add if delta missing
		return base // return base unchanged
	}
	base.InputTokens += delta.InputTokens       // accumulate input tokens
	base.OutputTokens += delta.OutputTokens     // accumulate output tokens
	base.ContextLength += delta.ContextLength   // accumulate context length for completeness
	if delta.ContextLimit > base.ContextLimit { // prefer higher limit to avoid regressions
		base.ContextLimit = delta.ContextLimit // update context limit when child limit is larger
	}
	base.Cost += delta.Cost // accumulate cost for overall spend
	return base             // return augmented total
}

func (m *model) sessionBreakdownLines() []string { // renders per-session self usage rows
	if len(m.usageState.sessions) == 0 { // nothing to render when map empty
		return nil // keep caller logic simple
	}

	ids := make([]string, 0, len(m.usageState.sessions)) // gather session IDs for deterministic ordering
	for id := range m.usageState.sessions {              // iterate known sessions
		ids = append(ids, id) // record id for sorting
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
		usage := m.usageState.sessions[id] // fetch stored snapshot
		if usage == nil {                  // skip if snapshot missing
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

func (m *model) rootSessionBlock() string { // formats root agent entry with exclusive usage
	exclusive := m.computeRootExclusiveUsage() // derive exclusive self usage
	if exclusive == nil {
		return ""
	}

	name := m.usageState.rootAgentName // prefer configured agent name
	if name == "" {
		name = "Root"
	}

	return formatSessionBlock(name, exclusive, m.usageState.activeSessionID == m.usageState.rootSessionID)
}

func (m *model) computeRootExclusiveUsage() *runtime.Usage { // subtracts child usage from root inclusive totals
	if m.usageState.rootInclusive == nil {
		return nil
	}

	exclusive := cloneUsage(m.usageState.rootInclusive) // operate on a copy
	for id, usage := range m.usageState.sessions {
		if id == m.usageState.rootSessionID || usage == nil {
			continue // skip root entry and nil snapshots
		}
		exclusive = subtractUsage(exclusive, usage) // remove child contribution
	}

	return exclusive
}

func subtractUsage(base, delta *runtime.Usage) *runtime.Usage { // subtracts usage safely
	if base == nil || delta == nil {
		return base
	}

	base.InputTokens -= delta.InputTokens
	if base.InputTokens < 0 {
		base.InputTokens = 0
	}
	base.OutputTokens -= delta.OutputTokens
	if base.OutputTokens < 0 {
		base.OutputTokens = 0
	}
	base.ContextLength = base.InputTokens + base.OutputTokens
	base.Cost -= delta.Cost
	if base.Cost < 0 {
		base.Cost = 0
	}

	return base
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
