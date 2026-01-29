package sidebar

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/tab"
	"github.com/docker/cagent/pkg/tui/components/tool/todotool"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

type Mode int

const (
	ModeVertical Mode = iota
	ModeCollapsed
)

// Model represents a sidebar component
type Model interface {
	layout.Model
	layout.Sizeable
	layout.Positionable

	SetTokenUsage(event *runtime.TokenUsageEvent)
	SetTodos(result *tools.ToolCallResult) error
	SetMode(mode Mode)
	SetAgentInfo(agentName, model, description string)
	SetTeamInfo(availableAgents []runtime.AgentDetails)
	SetAgentSwitching(switching bool)
	SetToolsetInfo(availableTools int, loading bool)
	SetSessionStarred(starred bool)
	SetQueuedMessages(messages ...string)
	GetSize() (width, height int)
	LoadFromSession(sess *session.Session)
	// HandleClick checks if click is on the star and returns true if handled
	HandleClick(x, y int) bool
	// IsCollapsed returns whether the sidebar is collapsed
	IsCollapsed() bool
	// ToggleCollapsed toggles the collapsed state
	ToggleCollapsed()
	// SetCollapsed sets the collapsed state directly
	SetCollapsed(collapsed bool)
	// CollapsedHeight returns the number of lines needed for collapsed mode
	CollapsedHeight(contentWidth int) int
	// GetPreferredWidth returns the user's preferred width (for resize persistence)
	GetPreferredWidth() int
	// SetPreferredWidth sets the user's preferred width
	SetPreferredWidth(width int)
	// ClampWidth ensures width is within valid bounds for the given window width
	ClampWidth(width, windowInnerWidth int) int
}

// ragIndexingState tracks per-strategy indexing progress
type ragIndexingState struct {
	current int
	total   int
	spinner spinner.Spinner
}

// model implements Model
type model struct {
	width              int
	height             int
	xPos               int                       // absolute x position on screen
	yPos               int                       // absolute y position on screen
	layoutCfg          LayoutConfig              // layout configuration for spacing
	sessionUsage       map[string]*runtime.Usage // sessionID -> latest usage snapshot
	sessionAgent       map[string]string         // sessionID -> agent name
	todoComp           *todotool.SidebarComponent
	mcpInit            bool
	ragIndexing        map[string]*ragIndexingState // strategy name -> indexing state
	spinner            spinner.Spinner
	spinnerActive      bool // true when spinner is registered with animation coordinator
	mode               Mode
	sessionTitle       string
	sessionStarred     bool
	sessionHasContent  bool // true when session has been used (has messages)
	currentAgent       string
	agentModel         string
	agentDescription   string
	availableAgents    []runtime.AgentDetails
	agentSwitching     bool
	availableTools     int
	toolsLoading       bool // true when more tools may still be loading
	sessionState       *service.SessionState
	workingAgent       string // Name of the agent currently working (empty if none)
	scrollbar          *scrollbar.Model
	workingDirectory   string
	queuedMessages     []string // Truncated preview of queued messages
	streamCancelled    bool     // true after ESC cancel until next StreamStartedEvent
	reasoningSupported bool     // true if current model supports reasoning (default: true / fail-open)
	collapsed          bool     // true when sidebar is collapsed
	preferredWidth     int      // user's preferred width (persisted across collapse/expand)
}

// Option is a functional option for configuring the sidebar.
type Option func(*model)

// WithLayoutConfig sets a custom layout configuration.
func WithLayoutConfig(cfg LayoutConfig) Option {
	return func(m *model) { m.layoutCfg = cfg }
}

func New(sessionState *service.SessionState, opts ...Option) Model {
	m := &model{
		width:              20,
		layoutCfg:          DefaultLayoutConfig(),
		height:             24,
		sessionUsage:       make(map[string]*runtime.Usage),
		sessionAgent:       make(map[string]string),
		todoComp:           todotool.NewSidebarComponent(),
		spinner:            spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		sessionTitle:       "New session",
		ragIndexing:        make(map[string]*ragIndexingState),
		sessionState:       sessionState,
		scrollbar:          scrollbar.New(),
		workingDirectory:   getCurrentWorkingDirectory(),
		reasoningSupported: true,
		preferredWidth:     DefaultWidth,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return nil
}

// needsSpinner returns true if any spinner-driving state is active.
func (m *model) needsSpinner() bool {
	return m.workingAgent != "" || m.toolsLoading || m.mcpInit
}

// startSpinner registers the spinner with the animation coordinator if not already active.
// Safe to call multiple times - only the first call registers.
func (m *model) startSpinner() tea.Cmd {
	if m.spinnerActive {
		return nil // Already registered
	}
	m.spinnerActive = true
	return m.spinner.Init()
}

// stopSpinner unregisters the spinner from the animation coordinator if no state needs it.
// Only actually stops if currently active AND no spinner-driving state remains.
func (m *model) stopSpinner() {
	if !m.spinnerActive {
		return // Not registered
	}
	if m.needsSpinner() {
		return // Still needed by another state
	}
	m.spinnerActive = false
	m.spinner.Stop()
}

func (m *model) SetTokenUsage(event *runtime.TokenUsageEvent) {
	if event == nil || event.Usage == nil || event.SessionID == "" || event.AgentName == "" {
		return
	}

	// Store/replace by session ID (each event has cumulative totals for that session)
	usage := *event.Usage
	m.sessionUsage[event.SessionID] = &usage
	m.sessionAgent[event.SessionID] = event.AgentName

	// Mark session as having content once we receive token usage
	m.sessionHasContent = true
}

func (m *model) SetTodos(result *tools.ToolCallResult) error {
	return m.todoComp.SetTodos(result)
}

// SetAgentInfo sets the current agent information and updates the model in availableAgents
func (m *model) SetAgentInfo(agentName, modelID, description string) {
	m.currentAgent = agentName
	m.agentModel = modelID
	m.agentDescription = description
	m.reasoningSupported = modelsdev.ModelSupportsReasoning(context.Background(), modelID)

	// Update the model in availableAgents for the current agent
	// This is important when model routing selects a different model than configured
	// Extract just the model name from "provider/model" format to match TeamInfoEvent format
	for i := range m.availableAgents {
		if m.availableAgents[i].Name == agentName && modelID != "" {
			modelName := modelID
			if idx := strings.LastIndex(modelName, "/"); idx != -1 {
				modelName = modelName[idx+1:]
			}
			m.availableAgents[i].Model = modelName
			break
		}
	}
}

// SetTeamInfo sets the available agents in the team
func (m *model) SetTeamInfo(availableAgents []runtime.AgentDetails) {
	m.availableAgents = availableAgents
}

// SetAgentSwitching sets whether an agent switch is in progress
func (m *model) SetAgentSwitching(switching bool) {
	m.agentSwitching = switching
}

// SetToolsetInfo sets the number of available tools and loading state
func (m *model) SetToolsetInfo(availableTools int, loading bool) {
	m.availableTools = availableTools
	m.toolsLoading = loading
}

// SetSessionStarred sets the starred status of the current session
func (m *model) SetSessionStarred(starred bool) {
	m.sessionStarred = starred
}

// SetQueuedMessages sets the list of queued message previews to display
func (m *model) SetQueuedMessages(queuedMessages ...string) {
	m.queuedMessages = queuedMessages
}

// HandleClick checks if click is on the star and returns true if it was
// x and y are coordinates relative to the sidebar's top-left corner
// This does NOT toggle the state - caller should handle that
func (m *model) HandleClick(x, y int) bool {
	// Don't handle star clicks if session has no content (star isn't shown)
	if !m.sessionHasContent {
		return false
	}

	// Account for left padding - the star starts after the padding
	adjustedX := x - m.layoutCfg.PaddingLeft

	// Check if click is within the star area
	if adjustedX < 0 || adjustedX > starClickWidth {
		return false
	}

	if m.mode == ModeCollapsed {
		// In collapsed mode, star is at the beginning of first line (y=0)
		return y == 0
	}
	// In vertical mode, star is below tab title and TabStyle padding
	return y == verticalStarY
}

// LoadFromSession loads sidebar state from a restored session
func (m *model) LoadFromSession(sess *session.Session) {
	if sess == nil {
		return
	}

	// Load token usage from session
	if sess.InputTokens > 0 || sess.OutputTokens > 0 || sess.Cost > 0 {
		m.sessionUsage[sess.ID] = &runtime.Usage{
			InputTokens:  sess.InputTokens,
			OutputTokens: sess.OutputTokens,
			Cost:         sess.Cost,
		}
	}

	// Load session title
	if sess.Title != "" {
		m.sessionTitle = sess.Title
	}

	// Load starred status
	m.sessionStarred = sess.Starred

	// Session has content if it has messages or token usage
	m.sessionHasContent = len(sess.Messages) > 0 || sess.InputTokens > 0 || sess.OutputTokens > 0
}

// formatTokenCount formats a token count with K/M suffixes for readability
func formatTokenCount(count int64) string {
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
func (m *model) contextPercent() string {
	if len(m.sessionUsage) == 1 {
		for _, usage := range m.sessionUsage {
			if usage.ContextLimit > 0 {
				percent := (float64(usage.ContextLength) / float64(usage.ContextLimit)) * 100
				return fmt.Sprintf("%.0f%%", percent)
			}
		}
	}
	return "0%"
}

// getCurrentWorkingDirectory returns the current working directory with home directory replaced by ~/
func getCurrentWorkingDirectory() string {
	pwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Replace home directory with ~/
	if homeDir := paths.GetHomeDir(); homeDir != "" && strings.HasPrefix(pwd, homeDir) {
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
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if m.mode == ModeVertical {
			sb, cmd := m.scrollbar.Update(msg)
			m.scrollbar = sb
			return m, cmd
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.mode == ModeVertical {
			switch msg.Button.String() {
			case "wheelup":
				m.scrollbar.ScrollUp()
			case "wheeldown":
				m.scrollbar.ScrollDown()
			}
		}
		return m, nil
	case *runtime.TokenUsageEvent:
		m.SetTokenUsage(msg)
		return m, nil
	case *runtime.MCPInitStartedEvent:
		// Ignore if stream was cancelled (stale event from before cancellation)
		if m.streamCancelled {
			return m, nil
		}
		if !m.mcpInit {
			m.mcpInit = true
			cmd := m.startSpinner()
			return m, cmd
		}
		return m, nil
	case *runtime.MCPInitFinishedEvent:
		if m.mcpInit {
			m.mcpInit = false
			m.stopSpinner() // Will only stop if no other state needs it
		}
		return m, nil
	case *runtime.RAGIndexingStartedEvent:
		// Ignore if stream was cancelled (stale event from before cancellation)
		if m.streamCancelled {
			return m, nil
		}
		// Use composite key: "ragName/strategyName" to differentiate strategies within same RAG manager
		key := msg.RAGName + "/" + msg.StrategyName
		slog.Debug("Sidebar received RAG indexing started event", "rag", msg.RAGName, "strategy", msg.StrategyName, "key", key)
		state := &ragIndexingState{
			spinner: m.spinner.Reset(),
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
		if state, exists := m.ragIndexing[key]; exists {
			state.spinner.Stop()
			delete(m.ragIndexing, key)
		}
		return m, nil
	case *runtime.SessionTitleEvent:
		m.sessionTitle = msg.Title
		return m, nil
	case *runtime.StreamStartedEvent:
		// New stream starting - reset cancelled flag and enable spinner
		m.streamCancelled = false
		m.workingAgent = msg.AgentName
		cmd := m.startSpinner()
		return m, cmd
	case *runtime.StreamStoppedEvent:
		m.workingAgent = ""
		m.stopSpinner() // Will only stop if no other state needs it
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
		// Ignore loading state if stream was cancelled (stale event from before cancellation)
		if m.streamCancelled && msg.Loading {
			return m, nil
		}
		m.SetToolsetInfo(msg.AvailableTools, msg.Loading)
		if msg.Loading {
			cmd := m.startSpinner()
			return m, cmd
		}
		m.stopSpinner() // Will only stop if no other state needs it
		return m, nil
	case messages.StreamCancelledMsg:
		// Clear all spinner-driving state when stream is cancelled via ESC
		m.streamCancelled = true
		m.workingAgent = ""
		m.toolsLoading = false
		m.mcpInit = false
		// Force-stop main spinner if it was active (state is now cleared)
		if m.spinnerActive {
			m.spinnerActive = false
			m.spinner.Stop()
		}
		// Stop and clear any in-flight RAG indexing spinners
		for k, state := range m.ragIndexing {
			state.spinner.Stop()
			delete(m.ragIndexing, k)
		}
		return m, nil
	case messages.ThemeChangedMsg:
		// Theme changed - recreate spinners with new colors
		// The spinner pre-renders frames with colors, so we need to recreate it
		var cmds []tea.Cmd

		// Recreate main spinner
		wasActive := m.spinnerActive
		if wasActive {
			m.spinner.Stop()
		}
		m.spinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)
		if wasActive {
			cmd := m.spinner.Init()
			m.spinnerActive = true
			cmds = append(cmds, cmd)
		}

		// Recreate all RAG indexing spinners
		for _, state := range m.ragIndexing {
			state.spinner.Stop()
			state.spinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)
			cmds = append(cmds, state.spinner.Init())
		}

		return m, tea.Batch(cmds...)
	default:
		var cmds []tea.Cmd

		// Update main spinner when MCP is initializing, tools are loading, or an agent is working
		if m.mcpInit || m.toolsLoading || m.workingAgent != "" {
			model, cmd := m.spinner.Update(msg)
			m.spinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}

		// Update each RAG indexing spinner
		for _, state := range m.ragIndexing {
			model, cmd := state.spinner.Update(msg)
			state.spinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}

		return m, tea.Batch(cmds...)
	}
}

// View renders the component
func (m *model) View() string {
	var content string
	if m.mode == ModeVertical {
		content = m.verticalView()
	} else {
		content = m.collapsedView()
	}

	// Apply horizontal padding
	if m.layoutCfg.PaddingLeft > 0 || m.layoutCfg.PaddingRight > 0 {
		leftPad := strings.Repeat(" ", m.layoutCfg.PaddingLeft)
		rightPad := strings.Repeat(" ", m.layoutCfg.PaddingRight)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = leftPad + line + rightPad
		}
		content = strings.Join(lines, "\n")
	}

	return content
}

// starIndicator returns the star indicator string based on starred status.
// Returns empty string if session has no content yet.
func (m *model) starIndicator() string {
	if !m.sessionHasContent {
		return ""
	}
	return styles.StarIndicator(m.sessionStarred)
}

// collapsedLayout holds the computed layout decisions for collapsed mode.
// Computing this once avoids duplicating the layout logic between CollapsedHeight and collapsedView.
type collapsedLayout struct {
	titleWithStar    string
	workingIndicator string
	workingDir       string
	usageSummary     string

	// Layout decisions
	titleAndIndicatorOnOneLine bool
	wdAndUsageOnOneLine        bool
	contentWidth               int
}

func (m *model) computeCollapsedLayout(contentWidth int) collapsedLayout {
	h := collapsedLayout{
		titleWithStar:    m.starIndicator() + m.sessionTitle,
		workingIndicator: m.workingIndicatorCollapsed(),
		workingDir:       m.workingDirectory,
		usageSummary:     m.tokenUsageSummary(),
		contentWidth:     contentWidth,
	}

	titleWidth := lipgloss.Width(h.titleWithStar)
	wiWidth := lipgloss.Width(h.workingIndicator)
	wdWidth := lipgloss.Width(h.workingDir)
	usageWidth := lipgloss.Width(h.usageSummary)

	// Title and indicator fit on one line if:
	// - no working indicator AND title fits, OR
	// - both fit together with gap
	h.titleAndIndicatorOnOneLine = (h.workingIndicator == "" && titleWidth <= contentWidth) ||
		(h.workingIndicator != "" && titleWidth+minGap+wiWidth <= contentWidth)
	h.wdAndUsageOnOneLine = wdWidth+minGap+usageWidth <= contentWidth

	return h
}

func (h collapsedLayout) lineCount() int {
	lines := 1 // divider

	switch {
	case h.titleAndIndicatorOnOneLine:
		lines++
	case h.workingIndicator == "":
		// No working indicator but title wraps
		lines += linesNeeded(lipgloss.Width(h.titleWithStar), h.contentWidth)
	default:
		// Title and working indicator on separate lines, each may wrap
		lines += linesNeeded(lipgloss.Width(h.titleWithStar), h.contentWidth)
		lines += linesNeeded(lipgloss.Width(h.workingIndicator), h.contentWidth)
	}

	if h.wdAndUsageOnOneLine {
		lines++
	} else {
		lines += linesNeeded(lipgloss.Width(h.workingDir), h.contentWidth)
		if h.usageSummary != "" {
			lines += linesNeeded(lipgloss.Width(h.usageSummary), h.contentWidth)
		}
	}

	return lines
}

func (h collapsedLayout) render() string {
	var lines []string

	// Title line(s)
	switch {
	case h.titleAndIndicatorOnOneLine:
		if h.workingIndicator == "" {
			lines = append(lines, h.titleWithStar)
		} else {
			gap := h.contentWidth - lipgloss.Width(h.titleWithStar) - lipgloss.Width(h.workingIndicator)
			lines = append(lines, fmt.Sprintf("%s%*s%s", h.titleWithStar, gap, "", h.workingIndicator))
		}
	case h.workingIndicator == "":
		// No working indicator but title wraps - just output title (lipgloss will wrap)
		lines = append(lines, h.titleWithStar)
	default:
		// Title and working indicator on separate lines
		lines = append(lines, h.titleWithStar, h.workingIndicator)
	}

	// Working directory + usage line(s)
	if h.wdAndUsageOnOneLine {
		gap := h.contentWidth - lipgloss.Width(h.workingDir) - lipgloss.Width(h.usageSummary)
		lines = append(lines, fmt.Sprintf("%s%*s%s", styles.MutedStyle.Render(h.workingDir), gap, "", h.usageSummary))
	} else {
		lines = append(lines, styles.MutedStyle.Render(h.workingDir))
		if h.usageSummary != "" {
			lines = append(lines, h.usageSummary)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Top, lines...)
}

// linesNeeded calculates how many lines are needed to display text of given width
// within a container of contentWidth. Returns at least 1 line.
func linesNeeded(textWidth, contentWidth int) int {
	if contentWidth <= 0 || textWidth <= 0 {
		return 1
	}
	return max(1, (textWidth+contentWidth-1)/contentWidth)
}

// CollapsedHeight returns the number of lines needed for collapsed mode.
func (m *model) CollapsedHeight(outerWidth int) int {
	contentWidth := outerWidth - m.layoutCfg.PaddingLeft - m.layoutCfg.PaddingRight
	if contentWidth < 1 {
		contentWidth = 1
	}
	return m.computeCollapsedLayout(contentWidth).lineCount()
}

func (m *model) collapsedView() string {
	return m.computeCollapsedLayout(m.contentWidth(false)).render()
}

func (m *model) verticalView() string {
	visibleLines := m.height

	// Two-pass rendering: first check if scrollbar is needed
	// Pass 1: render without scrollbar to count lines
	contentWidthNoScroll := m.contentWidth(false)
	lines := m.renderSections(contentWidthNoScroll)
	totalLines := len(lines)
	needsScrollbar := totalLines > visibleLines

	// Pass 2: if scrollbar needed, re-render with narrower content width
	if needsScrollbar {
		contentWidthWithScroll := m.contentWidth(true)
		lines = m.renderSections(contentWidthWithScroll)
		totalLines = len(lines)
	}

	// Update scrollbar dimensions
	m.scrollbar.SetDimensions(visibleLines, totalLines)

	// Get scroll offset from scrollbar
	scrollOffset := m.scrollbar.GetScrollOffset()

	// Extract visible portion
	endIdx := min(scrollOffset+visibleLines, totalLines)
	visibleContent := lines[scrollOffset:endIdx]

	// Pad to fill height if content is shorter
	for len(visibleContent) < visibleLines {
		visibleContent = append(visibleContent, "")
	}

	// Render with scrollbar gap if needed
	if needsScrollbar {
		scrollbarGap := strings.Repeat(" ", m.layoutCfg.ScrollbarGap)
		scrollbarView := m.scrollbar.View()
		return lipgloss.JoinHorizontal(lipgloss.Top,
			strings.Join(visibleContent, "\n"),
			scrollbarGap,
			scrollbarView,
		)
	}

	return strings.Join(visibleContent, "\n")
}

// renderSections renders all sidebar sections and returns them as lines.
func (m *model) renderSections(contentWidth int) []string {
	var lines []string

	appendSection := func(section string) {
		if section != "" {
			lines = append(lines, strings.Split(section, "\n")...)
		}
	}

	appendSection(m.sessionInfo(contentWidth))
	appendSection(m.tokenUsage(contentWidth))
	appendSection(m.queueSection(contentWidth))
	appendSection(m.agentInfo(contentWidth))
	appendSection(m.toolsetInfo(contentWidth))

	m.todoComp.SetSize(contentWidth)
	appendSection(strings.TrimSuffix(m.todoComp.Render(), "\n"))

	return lines
}

// ragStrategyInfo holds a parsed RAG strategy entry
type ragStrategyInfo struct {
	strategyName string
	state        *ragIndexingState
}

// groupedRAGIndexing returns RAG indexing states grouped and sorted by RAG name and strategy
func (m *model) groupedRAGIndexing() (ragNames []string, ragGroups map[string][]ragStrategyInfo) {
	ragGroups = make(map[string][]ragStrategyInfo)

	for key, state := range m.ragIndexing {
		parts := strings.Split(key, "/")
		if len(parts) == 2 {
			ragName := parts[0]
			ragGroups[ragName] = append(ragGroups[ragName], ragStrategyInfo{parts[1], state})
		}
	}

	// Sort RAG names and strategies for stable display
	ragNames = slices.Sorted(maps.Keys(ragGroups))
	for _, name := range ragNames {
		slices.SortFunc(ragGroups[name], func(a, b ragStrategyInfo) int {
			return strings.Compare(a.strategyName, b.strategyName)
		})
	}

	return ragNames, ragGroups
}

func (m *model) workingIndicator() string {
	var indicators []string

	if m.mcpInit {
		indicators = append(indicators, styles.ActiveStyle.Render(m.spinner.View()+" Initializing MCP servers…"))
	}

	ragNames, ragGroups := m.groupedRAGIndexing()
	for _, ragName := range ragNames {
		strategies := ragGroups[ragName]
		displayRagName := strings.ReplaceAll(ragName, "_", " ")

		// RAG source header
		header := fmt.Sprintf("Indexing %s", styles.BoldStyle.Render(displayRagName))
		indicators = append(indicators, styles.ActiveStyle.Render(header))

		// Each strategy with its spinner and progress
		for _, strategy := range strategies {
			displayStratName := strings.ReplaceAll(strategy.strategyName, "-", " ")
			progress := m.formatProgress(strategy.state)
			line := fmt.Sprintf("  %s %s%s", strategy.state.spinner.View(), styles.BoldStyle.Render(displayStratName), progress)
			indicators = append(indicators, line)
		}
	}

	if len(indicators) == 0 {
		return ""
	}

	return strings.Join(indicators, "\n")
}

// workingIndicatorCollapsed returns a single-line version of the working indicator for collapsed mode
func (m *model) workingIndicatorCollapsed() string {
	var labels []string

	if m.mcpInit {
		labels = append(labels, "Initializing MCP servers…")
	}

	ragNames, ragGroups := m.groupedRAGIndexing()
	for _, ragName := range ragNames {
		strategies := ragGroups[ragName]
		displayRagName := strings.ReplaceAll(ragName, "_", " ")

		labels = append(labels, fmt.Sprintf("Indexing %s", styles.BoldStyle.Render(displayRagName)))

		for _, strategy := range strategies {
			displayStratName := strings.ReplaceAll(strategy.strategyName, "-", " ")
			progress := m.formatProgress(strategy.state)
			labels = append(labels, fmt.Sprintf("  • %s%s", styles.BoldStyle.Render(displayStratName), progress))
		}
	}

	if len(labels) == 0 {
		return ""
	}

	return styles.ActiveStyle.Render(m.spinner.View() + " " + strings.Join(labels, " | "))
}

func (m *model) formatProgress(state *ragIndexingState) string {
	if state.total > 0 {
		return fmt.Sprintf(" [%d/%d]", state.current, state.total)
	}
	return ""
}

func (m *model) tokenUsage(contentWidth int) string {
	var totalTokens int64
	var totalCost float64
	for _, usage := range m.sessionUsage {
		totalTokens += usage.InputTokens + usage.OutputTokens
		totalCost += usage.Cost
	}

	var tokenUsage strings.Builder
	fmt.Fprintf(&tokenUsage, "%s", formatTokenCount(totalTokens))
	if ctxText := m.contextPercent(); ctxText != "" {
		fmt.Fprintf(&tokenUsage, " (%s)", ctxText)
	}
	fmt.Fprintf(&tokenUsage, " %s", styles.TabAccentStyle.Render("$"+formatCost(totalCost)))

	return m.renderTab("Token Usage", tokenUsage.String(), contentWidth)
}

// tokenUsageSummary returns a single-line summary for horizontal layout.
func (m *model) tokenUsageSummary() string {
	if len(m.sessionUsage) == 0 {
		return ""
	}

	var totalTokens int64
	var totalCost float64
	for _, usage := range m.sessionUsage {
		totalTokens += usage.InputTokens + usage.OutputTokens
		totalCost += usage.Cost
	}

	if ctxText := m.contextPercent(); ctxText != "" {
		return fmt.Sprintf("Tokens: %s | Cost: $%s | Context: %s", formatTokenCount(totalTokens), formatCost(totalCost), ctxText)
	}

	return fmt.Sprintf("Tokens: %s | Cost: $%s", formatTokenCount(totalTokens), formatCost(totalCost))
}

func (m *model) sessionInfo(contentWidth int) string {
	lines := []string{
		m.starIndicator() + m.sessionTitle,
		"",
	}

	if m.workingDirectory != "" {
		lines = append(lines, styles.TabAccentStyle.Render("█")+styles.TabPrimaryStyle.Render(" "+m.workingDirectory))
	}

	return m.renderTab("Session", strings.Join(lines, "\n"), contentWidth)
}

// queueSection renders the queued messages section
func (m *model) queueSection(contentWidth int) string {
	if len(m.queuedMessages) == 0 {
		return ""
	}

	maxMsgWidth := contentWidth - treePrefixWidth
	var lines []string

	for i, msg := range m.queuedMessages {
		// Determine prefix based on position
		var prefix string
		if i == len(m.queuedMessages)-1 {
			prefix = styles.MutedStyle.Render("└ ")
		} else {
			prefix = styles.MutedStyle.Render("├ ")
		}

		// Truncate message and add prefix
		truncated := toolcommon.TruncateText(msg, maxMsgWidth)
		lines = append(lines, prefix+truncated)
	}

	// Add hint for clearing
	lines = append(lines, styles.MutedStyle.Render("  Ctrl+X to clear"))

	title := fmt.Sprintf("Queue (%d)", len(m.queuedMessages))
	return m.renderTab(title, strings.Join(lines, "\n"), contentWidth)
}

// agentInfo renders the current agent information
func (m *model) agentInfo(contentWidth int) string {
	// Read current agent from session state so sidebar updates when agent is switched
	currentAgent := m.sessionState.CurrentAgentName()
	if currentAgent == "" {
		return ""
	}

	agentTitle := "Agent"
	if len(m.availableAgents) > 1 {
		agentTitle = "Agents"
	}
	if m.agentSwitching {
		agentTitle += " ↔"
	}

	var content strings.Builder
	for i, agent := range m.availableAgents {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}
		isCurrent := agent.Name == currentAgent
		m.renderAgentEntry(&content, agent, isCurrent, i, contentWidth)
	}

	return m.renderTab(agentTitle, content.String(), contentWidth)
}

func (m *model) renderAgentEntry(content *strings.Builder, agent runtime.AgentDetails, isCurrent bool, index, contentWidth int) {
	var prefix string
	if isCurrent {
		if m.workingAgent == agent.Name {
			// Style the spinner with the same green as the agent name
			prefix = styles.TabAccentStyle.Render(m.spinner.View()) + " "
		} else {
			prefix = styles.TabAccentStyle.Render("▶") + " "
		}
	}
	// Agent name
	agentNameText := prefix + styles.TabAccentStyle.Render(agent.Name)
	// Shortcut hint (^1, ^2, etc.) - show for agents 1-9
	var shortcutHint string
	if index >= 0 && index < 9 {
		shortcutHint = styles.MutedStyle.Render(fmt.Sprintf("^%d", index+1))
	}
	// Calculate space needed to right-align the shortcut
	nameWidth := lipgloss.Width(agentNameText)
	hintWidth := lipgloss.Width(shortcutHint)
	spaceWidth := max(contentWidth-nameWidth-hintWidth, 1)
	if shortcutHint != "" {
		content.WriteString(agentNameText + strings.Repeat(" ", spaceWidth) + shortcutHint)
	} else {
		content.WriteString(agentNameText)
	}

	maxWidth := contentWidth - treePrefixWidth

	if desc := agent.Description; desc != "" {
		content.WriteString("\n")
		content.WriteString(styles.MutedStyle.Render("├ "))
		content.WriteString(toolcommon.TruncateText(desc, maxWidth))
	}

	content.WriteString("\n")
	content.WriteString(styles.MutedStyle.Render("├ "))
	content.WriteString(toolcommon.TruncateText("Provider: "+agent.Provider, maxWidth))
	content.WriteString("\n")
	content.WriteString(styles.MutedStyle.Render("└ "))
	content.WriteString(toolcommon.TruncateText("Model: "+agent.Model, maxWidth))
}

// toolsetInfo renders the current toolset status information
func (m *model) toolsetInfo(contentWidth int) string {
	var lines []string

	// Tools status line
	lines = append(lines, m.renderToolsStatus())

	// Toggle indicators with shortcuts
	// Only show "Thinking enabled" if the model supports reasoning
	toggles := []struct {
		enabled  bool
		label    string
		shortcut string
	}{
		{m.sessionState.YoloMode(), "YOLO mode enabled", "^y"},
		{m.sessionState.Thinking() && m.reasoningSupported, "Thinking enabled", "/think"},
		{m.sessionState.HideToolResults(), "Tool output hidden", "^o"},
		{m.sessionState.SplitDiffView(), "Split Diff View enabled", "^t"},
	}

	for _, toggle := range toggles {
		if toggle.enabled {
			lines = append(lines, m.renderToggleIndicator(toggle.label, toggle.shortcut, contentWidth))
		}
	}

	if working := m.workingIndicator(); working != "" {
		lines = append(lines, working)
	}

	return m.renderTab("Tools", lipgloss.JoinVertical(lipgloss.Top, lines...), contentWidth)
}

// renderToolsStatus renders the tools available/loading status line
func (m *model) renderToolsStatus() string {
	if m.toolsLoading {
		if m.availableTools > 0 {
			return m.spinner.View() + styles.TabPrimaryStyle.Render(fmt.Sprintf(" %d tools available…", m.availableTools))
		}
		return m.spinner.View() + styles.TabPrimaryStyle.Render(" Loading tools…")
	}
	if m.availableTools > 0 {
		return styles.TabAccentStyle.Render("█") + styles.TabPrimaryStyle.Render(fmt.Sprintf(" %d tools available", m.availableTools))
	}
	return ""
}

// renderToggleIndicator renders a toggle status with its keyboard shortcut
func (m *model) renderToggleIndicator(label, shortcut string, contentWidth int) string {
	indicator := styles.TabAccentStyle.Render("✓") + styles.TabPrimaryStyle.Render(" "+label)
	shortcutStyled := lipgloss.PlaceHorizontal(contentWidth-lipgloss.Width(indicator), lipgloss.Right, styles.MutedStyle.Render(shortcut))
	return indicator + shortcutStyled
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.updateScrollbarPosition()
	return nil
}

// SetPosition sets the absolute position of the component on screen
func (m *model) SetPosition(x, y int) tea.Cmd {
	m.xPos = x
	m.yPos = y
	m.updateScrollbarPosition()
	return nil
}

// updateScrollbarPosition updates the scrollbar's position based on sidebar position and size
func (m *model) updateScrollbarPosition() {
	// Scrollbar is at the right edge of the sidebar content
	// width-1 because the scrollbar is 1 char wide and at the rightmost position
	m.scrollbar.SetPosition(m.xPos+m.width-1, m.yPos)
}

// GetSize returns the current dimensions
func (m *model) GetSize() (width, height int) {
	return m.width, m.height
}

func (m *model) SetMode(mode Mode) {
	m.mode = mode
}

func (m *model) renderTab(title, content string, contentWidth int) string {
	return tab.Render(title, content, contentWidth)
}

// metrics computes the layout metrics for the current render.
// scrollbarVisible should be true if the scrollbar will be shown.
func (m *model) metrics(scrollbarVisible bool) Metrics {
	return m.layoutCfg.Compute(m.width, scrollbarVisible)
}

// contentWidth returns the width available for content in the current mode.
// For horizontal mode, scrollbar is never shown.
// For vertical mode, this is a preliminary estimate; actual scrollbar visibility
// is determined during render.
func (m *model) contentWidth(scrollbarVisible bool) int {
	return m.metrics(scrollbarVisible).ContentWidth
}

// IsCollapsed returns whether the sidebar is collapsed
func (m *model) IsCollapsed() bool {
	return m.collapsed
}

// ToggleCollapsed toggles the collapsed state of the sidebar.
// When expanding, if the preferred width is below minimum (e.g., after drag-to-collapse),
// it resets to the default width.
func (m *model) ToggleCollapsed() {
	m.collapsed = !m.collapsed
	if !m.collapsed && m.preferredWidth < MinWidth {
		m.preferredWidth = DefaultWidth
	}
}

// SetCollapsed sets the collapsed state directly.
// When expanding, if the preferred width is below minimum (e.g., after drag-to-collapse),
// it resets to the default width.
func (m *model) SetCollapsed(collapsed bool) {
	m.collapsed = collapsed
	if !collapsed && m.preferredWidth < MinWidth {
		m.preferredWidth = DefaultWidth
	}
}

// GetPreferredWidth returns the user's preferred width
func (m *model) GetPreferredWidth() int {
	return m.preferredWidth
}

// SetPreferredWidth sets the user's preferred width
func (m *model) SetPreferredWidth(width int) {
	m.preferredWidth = width
}

// ClampWidth ensures width is within valid bounds for the given window inner width
func (m *model) ClampWidth(width, windowInnerWidth int) int {
	maxWidth := min(int(float64(windowInnerWidth)*MaxWidthPercent), windowInnerWidth-20)
	return max(MinWidth, min(width, maxWidth))
}
