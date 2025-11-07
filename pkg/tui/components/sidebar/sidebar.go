package sidebar

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	sessions        map[string]*runtime.Usage // per-session self usage snapshots
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
			sessions: make(map[string]*runtime.Usage), // allocate map to avoid nil lookups
		},
		todoComp:     todotool.NewSidebarComponent(manager),                           // instantiate todo component
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

	wi := m.workingIndicator()
	titleGapWidth := m.width - lipgloss.Width(m.sessionTitle) - lipgloss.Width(wi) - 2
	title := fmt.Sprintf("%s%*s%s", m.sessionTitle, titleGapWidth, "", m.workingIndicator())

	gapWidth := m.width - lipgloss.Width(pwd) - lipgloss.Width(m.tokenUsage()) - 2
	return lipgloss.JoinVertical(lipgloss.Top, title, fmt.Sprintf("%s%*s%s", styles.MutedStyle.Render(pwd), gapWidth, "", m.tokenUsage()))
}

func (m *model) verticalView() string {
	topContent := m.sessionTitle + "\n"

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

func (m *model) tokenUsage() string { // renders aggregate usage summary line
	label, totals := m.renderTotals()                       // get friendly label plus computed totals
	totalTokens := totals.InputTokens + totals.OutputTokens // sum user + assistant tokens for display
	var usagePercent float64                                // default to zero percent until both limits/length available
	if totals.ContextLimit > 0 {                            // avoid divide-by-zero if limit unknown
		usagePercent = (float64(totals.ContextLength) / float64(totals.ContextLimit)) * 100 // compute context utilization percentage
	}

	percentageText := styles.MutedStyle.Render(fmt.Sprintf("%.0f%%", usagePercent))                  // style percentage for readability
	totalTokensText := styles.SubtleStyle.Render(fmt.Sprintf("(%s)", formatTokenCount(totalTokens))) // show compact token count
	costText := styles.MutedStyle.Render(fmt.Sprintf("$%.2f", totals.Cost))                          // render cumulative cost

	return fmt.Sprintf("%s %s %s %s", label, percentageText, totalTokensText, costText) // final combined line with prefix
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

	label := styles.SubtleStyle.Render("Session Total") // default label when only one session present
	if m.usageState.rootInclusive != nil {              // when root inclusive exists we can show team wording
		label = styles.SubtleStyle.Render("Team Total")                                                       // highlight that totals represent the whole team
		if m.usageState.activeSessionID != "" && m.usageState.activeSessionID != m.usageState.rootSessionID { // active child contributes live usage
			label = styles.SubtleStyle.Render("Team Total (incl. active child)") // clarify that active child is included
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
