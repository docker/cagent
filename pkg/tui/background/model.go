// Package background provides concurrent agents feature support.
package background

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/animation"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/components/tabbar"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/service/supervisor"
	"github.com/docker/cagent/pkg/tui/service/tuistate"
	"github.com/docker/cagent/pkg/tui/styles"
)

// SessionSpawner creates new sessions with their own runtime.
// This is an alias to the supervisor package's SessionSpawner type.
type SessionSpawner = supervisor.SessionSpawner

// FocusedPanel represents which panel is currently focused
type FocusedPanel string

const (
	PanelContent FocusedPanel = "content"
	PanelEditor  FocusedPanel = "editor"

	// resizeHandleWidth is the width of the draggable center portion of the resize handle
	resizeHandleWidth = 8
	// appPaddingHorizontal is total horizontal padding from AppStyle (left + right)
	appPaddingHorizontal = 2
)

// Model is the multi-session TUI model that wraps the single-session chat page.
type Model struct {
	supervisor *supervisor.Supervisor
	tabBar     *tabbar.TabBar
	tuiStore   *tuistate.Store

	// Per-session chat pages (kept alive for streaming continuity)
	chatPages     map[string]chat.Page
	sessionStates map[string]*service.SessionState

	// Per-session editors (preserved across tab switches for draft text)
	editors map[string]editor.Editor

	// Active session (convenience pointers to the currently visible session)
	application  *app.App
	sessionState *service.SessionState
	chatPage     chat.Page
	editor       editor.Editor

	// Shared history for command history across all editors
	history *history.History

	// UI components
	notification notification.Manager
	dialogMgr    dialog.Manager
	statusBar    statusbar.StatusBar
	completions  completion.Manager

	// Working state indicator (resize handle spinner)
	workingSpinner spinner.Spinner

	// Window state
	wWidth, wHeight int
	width, height   int

	// Content area height (height minus editor, tab bar, resize handle, status bar)
	contentHeight int

	// Editor resize state
	editorLines      int
	isDragging       bool
	isHoveringHandle bool

	// Focus state
	focusedPanel FocusedPanel

	// keyboardEnhancements stores the last keyboard enhancements message
	keyboardEnhancements *tea.KeyboardEnhancementsMsg

	// keyboardEnhancementsSupported tracks whether the terminal supports keyboard enhancements
	keyboardEnhancementsSupported bool

	ready bool
	err   error
}

// New creates a new multi-session model.
func New(ctx context.Context, spawner SessionSpawner, initialApp *app.App, initialWorkingDir string, cleanup func()) tea.Model {
	// Initialize supervisor
	sv := supervisor.New(spawner)

	// Initialize tab bar with configurable title length from user settings
	tabTitleMaxLen := config.GetUserSettings().GetTabTitleMaxLength()
	tb := tabbar.New(tabTitleMaxLen)

	// Initialize tab store
	var ts *tuistate.Store
	var tsErr error
	ts, tsErr = tuistate.New()
	if tsErr != nil {
		slog.Warn("Failed to open TUI state store, tabs won't persist", "error", tsErr)
	}

	// Initialize shared command history
	historyStore, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize command history: %v\n", err)
	}

	initialSessionState := service.NewSessionState(initialApp.Session())
	initialChatPage := chat.New(initialApp, initialSessionState)
	initialEditor := editor.New(initialApp, historyStore)
	sessID := initialApp.Session().ID

	m := &Model{
		supervisor:     sv,
		tabBar:         tb,
		tuiStore:       ts,
		chatPages:      map[string]chat.Page{sessID: initialChatPage},
		sessionStates:  map[string]*service.SessionState{sessID: initialSessionState},
		editors:        map[string]editor.Editor{sessID: initialEditor},
		application:    initialApp,
		sessionState:   initialSessionState,
		chatPage:       initialChatPage,
		editor:         initialEditor,
		history:        historyStore,
		notification:   notification.New(),
		dialogMgr:      dialog.New(),
		completions:    completion.New(),
		workingSpinner: spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		focusedPanel:   PanelEditor,
		editorLines:    3,
	}

	// Initialize status bar (pass m as help provider)
	m.statusBar = statusbar.New(m)

	// Add the initial session to the supervisor
	sv.AddSession(ctx, initialApp, initialApp.Session(), initialWorkingDir, cleanup)

	// Persist initial tab
	if ts != nil {
		sess := initialApp.Session()
		_ = ts.AddTab(ctx, sess.ID, initialWorkingDir)
		_ = ts.SetActiveTab(ctx, sess.ID)
	}

	// Initialize tab bar with current tabs
	tabs, activeIdx := sv.GetTabs()
	tb.SetTabs(tabs, activeIdx)

	// Make sure to stop on context cancellation
	go func() {
		<-ctx.Done()
		for _, cp := range m.chatPages {
			cp.Cleanup()
		}
		for _, ed := range m.editors {
			ed.Cleanup()
		}
		if ts != nil {
			_ = ts.ClearTabs(context.Background())
			_ = ts.Close()
		}
		sv.Shutdown()
	}()

	return m
}

// SetProgram sets the tea.Program for the supervisor to send routed messages.
func (m *Model) SetProgram(p *tea.Program) {
	m.supervisor.SetProgram(p)
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.dialogMgr.Init(),
		m.chatPage.Init(),
		m.editor.Init(),
		m.editor.Focus(),
		m.application.SendFirstMessage(),
	)
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// --- Routing & Animation ---

	case messages.RoutedMsg:
		return m.handleRoutedMsg(msg)

	case animation.TickMsg:
		var cmds []tea.Cmd
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		cmds = append(cmds, cmd)
		// Update working spinner
		if m.chatPage.IsWorking() {
			var model layout.Model
			model, cmd = m.workingSpinner.Update(msg)
			m.workingSpinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}
		if animation.HasActive() {
			cmds = append(cmds, animation.StartTick())
		}
		return m, tea.Batch(cmds...)

	// --- Tab management ---

	case messages.TabsUpdatedMsg:
		m.tabBar.SetTabs(msg.Tabs, msg.ActiveIdx)
		return m, nil

	case messages.SpawnSessionMsg:
		return m.handleSpawnSession(msg.WorkingDir)

	case messages.SwitchTabMsg:
		return m.handleSwitchTab(msg.SessionID)

	case messages.CloseTabMsg:
		return m.handleCloseTab(msg.SessionID)

	// --- Focus requests from content view ---

	case messages.RequestFocusMsg:
		switch msg.Target {
		case messages.PanelMessages:
			if m.focusedPanel != PanelContent {
				m.focusedPanel = PanelContent
				m.editor.Blur()
				return m, m.chatPage.FocusMessages()
			}
		case messages.PanelEditor:
			if m.focusedPanel != PanelEditor {
				m.focusedPanel = PanelEditor
				m.chatPage.BlurMessages()
				return m, m.editor.Focus()
			}
		}
		return m, nil

	// --- Working state from content view ---

	case messages.WorkingStateChangedMsg:
		return m.handleWorkingStateChanged(msg)

	// --- Statusbar invalidation ---

	case messages.InvalidateStatusBarMsg:
		m.statusBar.InvalidateCache()
		return m, nil

	// --- Window / Terminal ---

	case tea.WindowSizeMsg:
		m.wWidth, m.wHeight = msg.Width, msg.Height
		cmd := m.handleWindowResize(msg.Width, msg.Height)
		return m, cmd

	case tea.KeyboardEnhancementsMsg:
		m.keyboardEnhancements = &msg
		m.keyboardEnhancementsSupported = msg.Flags != 0
		// Forward to content view
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		// Forward to editor
		editorModel, editorCmd := m.editor.Update(msg)
		m.editor = editorModel.(editor.Editor)
		return m, tea.Batch(cmd, editorCmd)

	// --- Keyboard input ---

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)

	case tea.PasteMsg:
		if m.dialogMgr.Open() {
			u, cmd := m.dialogMgr.Update(msg)
			m.dialogMgr = u.(dialog.Manager)
			return m, cmd
		}
		// Forward paste to editor
		editorModel, cmd := m.editor.Update(msg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd

	// --- Mouse ---

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case messages.WheelCoalescedMsg:
		return m.handleWheelCoalesced(msg)

	// --- Dialog lifecycle ---

	case dialog.OpenDialogMsg, dialog.CloseDialogMsg:
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd

	case dialog.ExitConfirmedMsg:
		m.cleanupAll()
		return m, tea.Quit

	case dialog.RuntimeResumeMsg:
		m.application.Resume(msg.Request)
		return m, nil

	case dialog.MultiChoiceResultMsg:
		if msg.DialogID == dialog.ToolRejectionDialogID {
			if msg.Result.IsCancelled {
				return m, nil
			}
			resumeMsg := dialog.HandleToolRejectionResult(msg.Result)
			if resumeMsg != nil {
				return m, tea.Sequence(
					core.CmdHandler(dialog.CloseDialogMsg{}),
					core.CmdHandler(*resumeMsg),
				)
			}
		}
		return m, nil

	// --- Notifications ---

	case notification.ShowMsg, notification.HideMsg:
		updated, cmd := m.notification.Update(msg)
		m.notification = updated
		return m, cmd

	// --- Runtime event specializations ---

	case *runtime.TeamInfoEvent:
		m.sessionState.SetAvailableAgents(msg.AvailableAgents)
		m.sessionState.SetCurrentAgentName(msg.CurrentAgent)
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	case *runtime.AgentInfoEvent:
		m.sessionState.SetCurrentAgentName(msg.AgentName)
		m.application.TrackCurrentAgentModel(msg.Model)
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	case *runtime.SessionTitleEvent:
		m.sessionState.SetSessionTitle(msg.Title)
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	// --- New session (slash command /new) ---

	case messages.NewSessionMsg:
		// In background mode, /new spawns a new tab.
		return m.handleSpawnSession("")

	// --- Exit ---

	case messages.ExitSessionMsg:
		m.cleanupAll()
		return m, tea.Quit

	case messages.ExitAfterFirstResponseMsg:
		m.cleanupAll()
		return m, tea.Quit

	// --- SendMsg from editor ---

	case messages.SendMsg:
		// Forward send messages to the active content view
		if m.history != nil {
			_ = m.history.Add(msg.Content)
		}
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	// --- File attachments (routed to editor) ---

	case messages.InsertFileRefMsg:
		m.editor.AttachFile(msg.FilePath)
		return m, nil

	// --- Agent management ---

	case messages.SwitchAgentMsg:
		return m.handleSwitchAgent(msg.AgentName)

	// --- Session browser ---

	case messages.OpenSessionBrowserMsg:
		return m.handleOpenSessionBrowser()

	case messages.LoadSessionMsg:
		return m.handleLoadSession(msg.SessionID)

	case messages.BranchFromEditMsg:
		return m.handleBranchFromEdit(msg)

	// --- Session commands (slash commands, command palette) ---

	case messages.ToggleYoloMsg:
		return m.handleToggleYolo()

	case messages.ToggleThinkingMsg:
		return m.handleToggleThinking()

	case messages.ToggleHideToolResultsMsg:
		return m.handleToggleHideToolResults()

	case messages.ToggleSplitDiffMsg:
		updated, cmd := m.chatPage.Update(editfile.ToggleDiffViewMsg{})
		m.chatPage = updated.(chat.Page)
		return m, cmd

	case messages.ClearQueueMsg:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	case messages.CompactSessionMsg:
		return m.handleCompactSession(msg.AdditionalPrompt)

	case messages.CopySessionToClipboardMsg:
		return m.handleCopySessionToClipboard()

	case messages.CopyLastResponseToClipboardMsg:
		return m.handleCopyLastResponseToClipboard()

	case messages.EvalSessionMsg:
		return m.handleEvalSession(msg.Filename)

	case messages.ExportSessionMsg:
		return m.handleExportSession(msg.Filename)

	case messages.ToggleSessionStarMsg:
		sessionID := msg.SessionID
		if sessionID == "" {
			if sess := m.application.Session(); sess != nil {
				sessionID = sess.ID
			} else {
				return m, nil
			}
		}
		return m.handleToggleSessionStar(sessionID)

	case messages.SetSessionTitleMsg:
		return m.handleSetSessionTitle(msg.Title)

	case messages.RegenerateTitleMsg:
		return m.handleRegenerateTitle()

	case messages.ShowCostDialogMsg:
		return m.handleShowCostDialog()

	case messages.ShowPermissionsDialogMsg:
		return m.handleShowPermissionsDialog()

	case messages.AgentCommandMsg:
		return m.handleAgentCommand(msg.Command)

	case messages.StartShellMsg:
		return m.startShell()

	// --- Model picker ---

	case messages.OpenModelPickerMsg:
		return m.handleOpenModelPicker()

	case messages.ChangeModelMsg:
		return m.handleChangeModel(msg.ModelRef)

	// --- Theme picker ---

	case messages.OpenThemePickerMsg:
		return m.handleOpenThemePicker()

	case messages.ChangeThemeMsg:
		return m.handleChangeTheme(msg.ThemeRef)

	case messages.ThemePreviewMsg:
		return m.handleThemePreview(msg.ThemeRef)

	case messages.ThemeCancelPreviewMsg:
		return m.handleThemeCancelPreview(msg.OriginalRef)

	case messages.ThemeChangedMsg:
		return m.applyThemeChanged()

	case messages.ThemeFileChangedMsg:
		return m.handleThemeFileChanged(msg.ThemeRef)

	// --- Speech-to-text ---

	case messages.StartSpeakMsg:
		return m, notification.InfoCmd("Speech-to-text is not yet supported in multi-tab mode")

	case messages.StopSpeakMsg, messages.SpeakTranscriptMsg:
		return m, nil

	// --- MCP prompts ---

	case messages.ShowMCPPromptInputMsg:
		return m.handleShowMCPPromptInput(msg.PromptName, msg.PromptInfo)

	case messages.MCPPromptMsg:
		return m.handleMCPPrompt(msg.PromptName, msg.Arguments)

	// --- File attachments ---

	case messages.AttachFileMsg:
		return m.handleAttachFile(msg.FilePath)

	case messages.SendAttachmentMsg:
		m.application.RunWithMessage(context.Background(), nil, msg.Content)
		return m, nil

	// --- URL opening ---

	case messages.OpenURLMsg:
		return m.handleOpenURL(msg.URL)

	// --- Elicitation ---

	case messages.ElicitationResponseMsg:
		return m.handleElicitationResponse(msg.Action, msg.Content)

	// --- Errors ---

	case error:
		m.err = msg
		return m, nil

	default:
		// Handle runtime events for active session
		if event, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			if agentName := event.GetAgentName(); agentName != "" {
				m.sessionState.SetCurrentAgentName(agentName)
			}
			updated, cmd := m.chatPage.Update(msg)
			m.chatPage = updated.(chat.Page)
			return m, cmd
		}

		// Forward to dialog if open
		if m.dialogMgr.Open() {
			u, cmd := m.dialogMgr.Update(msg)
			m.dialogMgr = u.(dialog.Manager)

			updated, cmdChatPage := m.chatPage.Update(msg)
			m.chatPage = updated.(chat.Page)

			return m, tea.Batch(cmd, cmdChatPage)
		}

		// Forward to both completion manager and editor
		updatedComp, cmdCompletions := m.completions.Update(msg)
		m.completions = updatedComp.(completion.Manager)

		editorModel, cmdEditor := m.editor.Update(msg)
		m.editor = editorModel.(editor.Editor)

		updated, cmdChatPage := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)

		return m, tea.Batch(cmdCompletions, cmdEditor, cmdChatPage)
	}
}

// handleRoutedMsg processes messages routed to specific sessions.
func (m *Model) handleRoutedMsg(msg messages.RoutedMsg) (tea.Model, tea.Cmd) {
	activeID := m.supervisor.ActiveID()

	if msg.SessionID == activeID {
		// Active session: forward through Update for full processing (spinners, cmds, etc.)
		return m.Update(msg.Inner)
	}

	// Background session: update its chat page directly so streaming content accumulates.
	// UI-only cmds (spinners, scroll) are discarded since the page isn't visible.
	chatPage, ok := m.chatPages[msg.SessionID]
	if !ok {
		return m, nil
	}

	// Update session state for background sessions
	if event, isRuntimeEvent := msg.Inner.(runtime.Event); isRuntimeEvent {
		if sessionState, ok := m.sessionStates[msg.SessionID]; ok {
			if agentName := event.GetAgentName(); agentName != "" {
				sessionState.SetCurrentAgentName(agentName)
			}
		}
	}

	// Update the background chat page (discard cmds — UI effects aren't needed for hidden pages)
	updated, _ := chatPage.Update(msg.Inner)
	m.chatPages[msg.SessionID] = updated.(chat.Page)
	return m, nil
}

// handleWorkingStateChanged updates the editor working indicator and resize handle spinner.
func (m *Model) handleWorkingStateChanged(msg messages.WorkingStateChangedMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Update editor working state
	cmds = append(cmds, m.editor.SetWorking(msg.Working))

	// Start/stop working spinner
	if msg.Working {
		cmds = append(cmds, m.workingSpinner.Init())
	} else {
		m.workingSpinner.Stop()
	}

	return m, tea.Batch(cmds...)
}

// handleOpenSessionBrowser opens the session browser dialog.
func (m *Model) handleOpenSessionBrowser() (tea.Model, tea.Cmd) {
	store := m.application.SessionStore()
	if store == nil {
		return m, notification.InfoCmd("No session store configured")
	}

	sessions, err := store.GetSessionSummaries(context.Background())
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to load sessions: %v", err))
	}
	if len(sessions) == 0 {
		return m, notification.InfoCmd("No previous sessions found")
	}

	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewSessionBrowserDialog(sessions),
	})
}

// handleLoadSession loads a saved session into the current tab (if empty) or a new tab.
func (m *Model) handleLoadSession(sessionID string) (tea.Model, tea.Cmd) {
	store := m.application.SessionStore()
	if store == nil {
		return m, notification.ErrorCmd("No session store configured")
	}

	sess, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
	}

	// Determine working directory from the loaded session.
	workingDir := sess.WorkingDir
	if workingDir == "" {
		workingDir = m.application.Session().WorkingDir
	}

	ctx := context.Background()

	// If the current session is empty (no messages, no title — the default state
	// when opening the TUI or creating a new tab), replace it in-place instead of
	// spawning yet another tab.
	currentSess := m.application.Session()
	if len(currentSess.Messages) == 0 && currentSess.Title == "" {
		return m.replaceActiveSession(ctx, sess)
	}

	slog.Debug("Loading session into new tab", "session_id", sessionID)

	// Spawn a new tab.
	newSessionID, err := m.supervisor.SpawnSession(ctx, workingDir)
	if err != nil {
		return m, notification.ErrorCmd("Failed to create tab: " + err.Error())
	}

	if m.tuiStore != nil {
		_ = m.tuiStore.AddTab(ctx, newSessionID, workingDir)
	}

	// Switch to the new tab so m.application points to the new app.
	model, switchCmd := m.handleSwitchTab(newSessionID)

	// Replace the blank session with the loaded one and rebuild the chat page.
	m.application.ReplaceSession(ctx, sess)
	m.sessionState = service.NewSessionState(sess)
	m.chatPage = chat.New(m.application, m.sessionState)
	m.chatPages[newSessionID] = m.chatPage
	m.sessionStates[newSessionID] = m.sessionState

	// Propagate the loaded session's title to the supervisor so the tab bar shows it.
	if sess.Title != "" {
		m.supervisor.SetRunnerTitle(newSessionID, sess.Title)
	}

	// Create a fresh editor for this session
	m.editor = editor.New(m.application, m.history)
	m.editors[newSessionID] = m.editor

	if m.keyboardEnhancements != nil {
		updated, _ := m.chatPage.Update(*m.keyboardEnhancements)
		m.chatPage = updated.(chat.Page)
		editorModel, _ := m.editor.Update(*m.keyboardEnhancements)
		m.editor = editorModel.(editor.Editor)
	}

	return model, tea.Batch(
		switchCmd,
		m.chatPage.Init(),
		m.editor.Init(),
		m.editor.Focus(),
		m.resizeAll(),
	)
}

// replaceActiveSession replaces the current (empty) tab's session with a loaded one in-place.
func (m *Model) replaceActiveSession(ctx context.Context, sess *session.Session) (tea.Model, tea.Cmd) {
	activeID := m.supervisor.ActiveID()

	slog.Debug("Replacing empty session in-place", "tab_id", activeID, "loaded_session", sess.ID)

	// Cleanup old chat page and editor for the active session
	if cp, ok := m.chatPages[activeID]; ok {
		cp.Cleanup()
	}
	if ed, ok := m.editors[activeID]; ok {
		ed.Cleanup()
	}

	// Replace the session in the app
	m.application.ReplaceSession(ctx, sess)

	// Rebuild session state, chat page, and editor
	m.sessionState = service.NewSessionState(sess)
	m.chatPage = chat.New(m.application, m.sessionState)
	m.editor = editor.New(m.application, m.history)

	m.chatPages[activeID] = m.chatPage
	m.sessionStates[activeID] = m.sessionState
	m.editors[activeID] = m.editor

	// Propagate the loaded session's title to the tab bar
	if sess.Title != "" {
		m.supervisor.SetRunnerTitle(activeID, sess.Title)
	}

	if m.keyboardEnhancements != nil {
		updated, _ := m.chatPage.Update(*m.keyboardEnhancements)
		m.chatPage = updated.(chat.Page)
		editorModel, _ := m.editor.Update(*m.keyboardEnhancements)
		m.editor = editorModel.(editor.Editor)
	}

	return m, tea.Batch(
		m.chatPage.Init(),
		m.editor.Init(),
		m.editor.Focus(),
		m.resizeAll(),
	)
}

// handleSpawnSession spawns a new session.
func (m *Model) handleSpawnSession(workingDir string) (tea.Model, tea.Cmd) {
	// If no working dir specified, open the picker
	if workingDir == "" {
		return m.openWorkingDirPicker()
	}

	// Spawn the new session
	ctx := context.Background()
	sessionID, err := m.supervisor.SpawnSession(ctx, workingDir)
	if err != nil {
		return m, notification.ErrorCmd("Failed to spawn session: " + err.Error())
	}

	// Persist the new tab
	if m.tuiStore != nil {
		_ = m.tuiStore.AddTab(ctx, sessionID, workingDir)
	}

	// Switch to the new session
	return m.handleSwitchTab(sessionID)
}

// openWorkingDirPicker opens the working directory picker dialog.
func (m *Model) openWorkingDirPicker() (tea.Model, tea.Cmd) {
	var recentDirs, favoriteDirs []string
	if m.tuiStore != nil {
		recentDirs, _ = m.tuiStore.GetRecentDirs(context.Background(), 10)
		favoriteDirs, _ = m.tuiStore.GetFavoriteDirs(context.Background())
	}

	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewWorkingDirPickerDialog(recentDirs, favoriteDirs, m.tuiStore),
	})
}

// handleSwitchTab switches to a different session.
// Existing chat pages and editors are preserved (not recreated) so that in-flight streaming
// content and draft text are retained when switching back to a tab.
func (m *Model) handleSwitchTab(sessionID string) (tea.Model, tea.Cmd) {
	runner := m.supervisor.SwitchTo(sessionID)
	if runner == nil {
		return m, notification.ErrorCmd("Session not found")
	}

	// Blur current editor before switching
	m.editor.Blur()

	// Get or create session state
	sessionState, stateExists := m.sessionStates[sessionID]
	if !stateExists {
		sessionState = service.NewSessionState(runner.App.Session())
		m.sessionStates[sessionID] = sessionState
	}

	// Get or create chat page
	chatPage, pageExists := m.chatPages[sessionID]
	if !pageExists {
		chatPage = chat.New(runner.App, sessionState)
		m.chatPages[sessionID] = chatPage
	}

	// Get or create editor
	ed, editorExists := m.editors[sessionID]
	if !editorExists {
		ed = editor.New(runner.App, m.history)
		m.editors[sessionID] = ed
	}

	// Update active convenience pointers
	m.application = runner.App
	m.sessionState = sessionState
	m.chatPage = chatPage
	m.editor = ed

	// Reapply keyboard enhancements
	if m.keyboardEnhancements != nil {
		updated, _ := m.chatPage.Update(*m.keyboardEnhancements)
		m.chatPage = updated.(chat.Page)
		editorModel, _ := m.editor.Update(*m.keyboardEnhancements)
		m.editor = editorModel.(editor.Editor)
	}

	// Persist active tab
	if m.tuiStore != nil {
		_ = m.tuiStore.SetActiveTab(context.Background(), sessionID)
	}

	// Update editor working state to match the session
	m.editor.SetWorking(m.chatPage.IsWorking())

	// Reset working spinner state
	if m.chatPage.IsWorking() {
		m.workingSpinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)
	} else {
		m.workingSpinner.Stop()
		m.workingSpinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)
	}

	if !pageExists || !editorExists {
		// New page or editor: initialize and resize
		var cmds []tea.Cmd
		if !pageExists {
			cmds = append(cmds, m.chatPage.Init())
		}
		if !editorExists {
			cmds = append(cmds, m.editor.Init())
		}
		cmds = append(cmds, m.editor.Focus(), m.resizeAll())
		if m.chatPage.IsWorking() {
			cmds = append(cmds, m.workingSpinner.Init())
		}
		return m, tea.Batch(cmds...)
	}

	// Existing: resize and restore auto-scroll
	var cmds []tea.Cmd
	cmds = append(cmds, m.resizeAll(), m.chatPage.ScrollToBottom(), m.editor.Focus())
	if m.chatPage.IsWorking() {
		cmds = append(cmds, m.workingSpinner.Init())
	}
	return m, tea.Batch(cmds...)
}

// handleCloseTab closes a session tab.
func (m *Model) handleCloseTab(sessionID string) (tea.Model, tea.Cmd) {
	wasActive := sessionID == m.supervisor.ActiveID()

	// Capture the working dir before closing so we can reuse it if this is the last tab.
	var closedWorkingDir string
	if runner := m.supervisor.GetRunner(sessionID); runner != nil {
		closedWorkingDir = runner.WorkingDir
	}

	nextActiveID := m.supervisor.CloseSession(sessionID)

	// Clean up per-session state
	if cp, ok := m.chatPages[sessionID]; ok {
		cp.Cleanup()
		delete(m.chatPages, sessionID)
	}
	if ed, ok := m.editors[sessionID]; ok {
		ed.Cleanup()
		delete(m.editors, sessionID)
	}
	delete(m.sessionStates, sessionID)

	// Remove from persistent store
	if m.tuiStore != nil {
		_ = m.tuiStore.RemoveTab(context.Background(), sessionID)
	}

	// If we closed all tabs, spawn a new one reusing the previous working dir.
	// We always provide a concrete dir to avoid showing the picker — pressing Esc
	// in the picker with zero tabs would leave the TUI in a broken state.
	if m.supervisor.Count() == 0 {
		workingDir := closedWorkingDir
		if workingDir == "" {
			workingDir, _ = os.Getwd()
		}
		if workingDir == "" {
			workingDir = "/"
		}
		return m.handleSpawnSession(workingDir)
	}

	// If the closed tab was active, switch to the next one
	if wasActive && nextActiveID != "" {
		return m.handleSwitchTab(nextActiveID)
	}

	return m, nil
}

// handleWindowResize handles window resize.
func (m *Model) handleWindowResize(width, height int) tea.Cmd {
	m.wWidth, m.wHeight = width, height

	m.statusBar.SetWidth(width)
	m.tabBar.SetWidth(width)

	m.width = width
	m.height = height

	if !m.ready {
		m.ready = true
	}

	return m.resizeAll()
}

// resizeAll recalculates all component sizes based on current window dimensions.
func (m *Model) resizeAll() tea.Cmd {
	var cmds []tea.Cmd

	width, height := m.width, m.height

	// Calculate fixed heights
	tabBarHeight := m.tabBar.Height()
	statusBarHeight := 1
	if view := m.statusBar.View(); view != "" {
		statusBarHeight = lipgloss.Height(view)
	}
	resizeHandleHeight := 1

	// Calculate editor height
	innerWidth := width - appPaddingHorizontal
	minLines := 4
	maxLines := max(minLines, (height-6)/2)
	m.editorLines = max(minLines, min(m.editorLines, maxLines))

	targetEditorHeight := m.editorLines - 1
	cmds = append(cmds, m.editor.SetSize(innerWidth, targetEditorHeight))
	_, editorHeight := m.editor.GetSize()
	// The editor's View() adds MarginBottom(1) which isn't included in GetSize(),
	// so account for it in the layout calculation.
	editorRenderedHeight := editorHeight + 1

	// Content gets remaining space
	m.contentHeight = max(1, height-tabBarHeight-statusBarHeight-resizeHandleHeight-editorRenderedHeight)

	// Update dialog (uses full window dimensions for overlay positioning)
	u, cmd := m.dialogMgr.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m.dialogMgr = u.(dialog.Manager)
	cmds = append(cmds, cmd)

	// Update chat page (content area)
	cmd = m.chatPage.SetSize(width, m.contentHeight)
	cmds = append(cmds, cmd)

	// Update completion manager with editor height for popup positioning
	m.completions.SetEditorBottom(editorHeight + tabBarHeight)
	m.completions.Update(tea.WindowSizeMsg{Width: width, Height: height})

	// Update notification
	m.notification.SetSize(width, height)

	return tea.Batch(cmds...)
}

// Help returns help information for the status bar.
func (m *Model) Help() help.KeyMap {
	return core.NewSimpleHelp(m.Bindings())
}

// Bindings returns the key bindings shown in the status bar.
func (m *Model) Bindings() []key.Binding {
	quitBinding := key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("Ctrl+c", "quit"),
	)
	tabBinding := key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "switch focus"),
	)

	bindings := []key.Binding{quitBinding, tabBinding}
	bindings = append(bindings, m.tabBar.Bindings()...)

	// Show newline help based on keyboard enhancement support
	if m.keyboardEnhancementsSupported {
		bindings = append(bindings, key.NewBinding(
			key.WithKeys("shift+enter"),
			key.WithHelp("Shift+Enter", "newline"),
		))
	} else {
		bindings = append(bindings, key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("Ctrl+j", "newline"),
		))
	}

	if m.focusedPanel == PanelContent {
		bindings = append(bindings, m.chatPage.Bindings()...)
	} else {
		editorName := getEditorDisplayNameFromEnv(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
		bindings = append(bindings, key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("Ctrl+g", fmt.Sprintf("edit in %s", editorName)),
		))
	}
	return bindings
}

// handleKeyPress handles all keyboard input with proper priority routing.
func (m *Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Tab bar keys first (Ctrl+Tab, Ctrl+Shift+Tab, Ctrl+W)
	if cmd := m.tabBar.Update(msg); cmd != nil {
		return m, cmd
	}

	// Dialog gets priority when open
	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	// Completion popup gets priority when open
	if m.completions.Open() {
		if core.IsNavigationKey(msg) {
			u, cmd := m.completions.Update(msg)
			m.completions = u.(completion.Manager)
			return m, cmd
		}
		// For all other keys (typing), send to both completion (for filtering) and editor
		var cmds []tea.Cmd
		u, completionCmd := m.completions.Update(msg)
		m.completions = u.(completion.Manager)
		cmds = append(cmds, completionCmd)

		editorModel, cmd := m.editor.Update(msg)
		m.editor = editorModel.(editor.Editor)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	// Global keyboard shortcuts
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		return m, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewExitConfirmationDialog(),
		})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+z"))):
		return m, tea.Suspend

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+k"))):
		categories := commands.BuildCommandCategories(context.Background(), m.application)
		return m, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewCommandPaletteDialog(categories),
		})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+y"))):
		return m, core.CmdHandler(messages.ToggleYoloMsg{})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+o"))):
		return m, core.CmdHandler(messages.ToggleHideToolResultsMsg{})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+s"))):
		return m.handleCycleAgent()

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+m"))):
		return m.handleOpenModelPicker()

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+x"))):
		return m, core.CmdHandler(messages.ClearQueueMsg{})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+g"))):
		return m.openExternalEditor()

	// Toggle sidebar (propagates to content view regardless of focus)
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	// Focus switching: Tab key toggles between content and editor
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		return m.switchFocus()

	// Esc: cancel stream (works regardless of focus)
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		// Forward to content view for stream cancellation
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	}

	// Focus-based routing
	switch m.focusedPanel {
	case PanelEditor:
		editorModel, cmd := m.editor.Update(msg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd
	case PanelContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	}

	return m, nil
}

// switchFocus toggles between content and editor panels.
func (m *Model) switchFocus() (tea.Model, tea.Cmd) {
	switch m.focusedPanel {
	case PanelEditor:
		// Check if editor has a suggestion to accept first
		if cmd := m.editor.AcceptSuggestion(); cmd != nil {
			return m, cmd
		}
		m.focusedPanel = PanelContent
		m.editor.Blur()
		return m, m.chatPage.FocusMessages()
	case PanelContent:
		m.focusedPanel = PanelEditor
		m.chatPage.BlurMessages()
		return m, m.editor.Focus()
	}
	return m, nil
}

// handleMouseClick routes mouse clicks to the appropriate component based on Y coordinate.
func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Dialogs use full-window coordinates (they're positioned over the entire screen)
	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	region := m.hitTestRegion(msg.Y)

	switch region {
	case regionContent:
		// Focus content on click
		if m.focusedPanel != PanelContent {
			m.focusedPanel = PanelContent
			m.editor.Blur()
			m.chatPage.FocusMessages()
		}
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd

	case regionResizeHandle:
		if msg.Button == tea.MouseLeft {
			m.isDragging = true
		}
		return m, nil

	case regionTabBar:
		// Adjust Y for tab bar (relative to its start)
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - m.contentHeight - 1
		if cmd := m.tabBar.Update(adjustedMsg); cmd != nil {
			return m, cmd
		}
		return m, nil

	case regionEditor:
		// Focus editor on click
		if m.focusedPanel != PanelEditor {
			m.focusedPanel = PanelEditor
			m.chatPage.BlurMessages()
		}
		// Adjust Y for editor
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, tea.Batch(cmd, m.editor.Focus())
	}

	return m, nil
}

// handleMouseMotion routes mouse motion events with adjusted coordinates.
func (m *Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.isDragging {
		cmd := m.handleEditorResize(msg.Y)
		return m, cmd
	}

	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	// Update hover state for resize handle
	m.isHoveringHandle = m.hitTestRegion(msg.Y) == regionResizeHandle

	region := m.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	case regionEditor:
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd
	}

	return m, nil
}

// handleMouseRelease routes mouse release events with adjusted coordinates.
func (m *Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.isDragging {
		m.isDragging = false
		return m, nil
	}

	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	region := m.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	case regionEditor:
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd
	}

	return m, nil
}

// handleMouseWheel routes mouse wheel events with adjusted coordinates.
func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	region := m.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	case regionEditor:
		m.editor.ScrollByWheel(1)
		if msg.Button == tea.MouseWheelUp {
			m.editor.ScrollByWheel(-1)
		}
		return m, nil
	}

	return m, nil
}

// handleWheelCoalesced routes coalesced wheel events with adjusted coordinates.
func (m *Model) handleWheelCoalesced(msg messages.WheelCoalescedMsg) (tea.Model, tea.Cmd) {
	if msg.Delta == 0 {
		return m, nil
	}

	if m.dialogMgr.Open() {
		// Convert coalesced delta back to individual MouseWheelMsg events
		steps := msg.Delta
		button := tea.MouseWheelDown
		if steps < 0 {
			steps = -steps
			button = tea.MouseWheelUp
		}
		var cmds []tea.Cmd
		for range steps {
			u, cmd := m.dialogMgr.Update(tea.MouseWheelMsg{X: msg.X, Y: msg.Y, Button: button})
			m.dialogMgr = u.(dialog.Manager)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	}

	region := m.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	case regionEditor:
		m.editor.ScrollByWheel(msg.Delta)
		return m, nil
	}

	return m, nil
}

// layoutRegion represents a vertical region in the TUI layout.
type layoutRegion int

const (
	regionContent layoutRegion = iota
	regionResizeHandle
	regionTabBar
	regionEditor
	regionStatusBar
)

// hitTestRegion determines which layout region a Y coordinate falls in.
func (m *Model) hitTestRegion(y int) layoutRegion {
	tabBarHeight := m.tabBar.Height()

	resizeHandleTop := m.contentHeight
	tabBarTop := resizeHandleTop + 1
	editorTop := tabBarTop + tabBarHeight

	switch {
	case y < resizeHandleTop:
		return regionContent
	case y < tabBarTop:
		return regionResizeHandle
	case y < editorTop:
		return regionTabBar
	default:
		_, editorHeight := m.editor.GetSize()
		if y < editorTop+editorHeight {
			return regionEditor
		}
		return regionStatusBar
	}
}

// editorTop returns the Y coordinate where the editor starts.
func (m *Model) editorTop() int {
	return m.contentHeight + 1 + m.tabBar.Height()
}

// handleEditorResize adjusts editor height based on drag position.
func (m *Model) handleEditorResize(y int) tea.Cmd {
	// Calculate target lines from drag position
	editorPadding := styles.EditorStyle.GetVerticalFrameSize()
	targetLines := m.height - y - 1 - editorPadding - m.tabBar.Height()
	minLines := 4
	maxLines := max(minLines, (m.height-6)/2)
	newLines := max(minLines, min(targetLines, maxLines))
	if newLines != m.editorLines {
		m.editorLines = newLines
		return m.resizeAll()
	}
	return nil
}

// renderResizeHandle renders the draggable separator between content and bottom panel.
func (m *Model) renderResizeHandle(width int) string {
	if width <= 0 {
		return ""
	}

	innerWidth := width - appPaddingHorizontal

	// Use brighter style when actively dragging
	centerStyle := styles.ResizeHandleHoverStyle
	if m.isDragging {
		centerStyle = styles.ResizeHandleActiveStyle
	}

	// Show a small centered highlight when hovered or dragging
	centerPart := strings.Repeat("─", min(resizeHandleWidth, innerWidth))
	handle := centerStyle.Render(centerPart)

	// Always center handle on full width
	fullLine := lipgloss.PlaceHorizontal(
		max(0, innerWidth), lipgloss.Center, handle,
		lipgloss.WithWhitespaceChars("─"),
		lipgloss.WithWhitespaceStyle(styles.ResizeHandleStyle),
	)

	if m.chatPage.IsWorking() {
		// Truncate right side and append spinner (handle stays centered)
		workingText := "Working…"
		if queueLen := m.chatPage.QueueLength(); queueLen > 0 {
			workingText = fmt.Sprintf("Working… (%d queued)", queueLen)
		}
		suffix := " " + m.workingSpinner.View() + " " + styles.SpinnerDotsHighlightStyle.Render(workingText)
		cancelKeyPart := styles.HighlightWhiteStyle.Render("Esc")
		suffix += " (" + cancelKeyPart + " to interrupt)"
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(innerWidth - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	// Show queue count even when not working (messages waiting to be processed)
	if queueLen := m.chatPage.QueueLength(); queueLen > 0 {
		queueText := fmt.Sprintf("%d queued", queueLen)
		suffix := " " + styles.WarningStyle.Render(queueText) + " "
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(innerWidth - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	return fullLine
}

// View renders the model.
func (m *Model) View() tea.View {
	windowTitle := m.windowTitle()

	if m.err != nil {
		return toFullscreenView(styles.ErrorStyle.Render(m.err.Error()), windowTitle)
	}

	if !m.ready {
		return toFullscreenView(
			styles.CenterStyle.
				Width(m.wWidth).
				Height(m.wHeight).
				Render(styles.MutedStyle.Render("Loading…")),
			windowTitle,
		)
	}

	// Content area (messages + sidebar) -- swaps per tab
	contentView := m.chatPage.View()

	// Resize handle (between content and bottom panel)
	resizeHandle := m.renderResizeHandle(m.width)

	// Tab bar (above editor)
	tabBarView := m.tabBar.View()

	// Editor (fixed position, per-session state)
	editorView := m.editor.View()

	// Status bar
	statusBarView := m.statusBar.View()

	// Combine: content | resize handle | tab bar | editor | status bar
	baseView := lipgloss.JoinVertical(lipgloss.Top,
		contentView,
		resizeHandle,
		tabBarView,
		editorView,
		statusBarView,
	)

	// Handle overlays
	hasOverlays := m.dialogMgr.Open() || m.notification.Open() || m.completions.Open()

	if hasOverlays {
		baseLayer := lipgloss.NewLayer(baseView)
		var allLayers []*lipgloss.Layer
		allLayers = append(allLayers, baseLayer)

		if m.dialogMgr.Open() {
			dialogLayers := m.dialogMgr.GetLayers()
			allLayers = append(allLayers, dialogLayers...)
		}

		if m.notification.Open() {
			allLayers = append(allLayers, m.notification.GetLayer())
		}

		if m.completions.Open() {
			allLayers = append(allLayers, m.completions.GetLayers()...)
		}

		canvas := lipgloss.NewCanvas(allLayers...)
		return toFullscreenView(canvas.Render(), windowTitle)
	}

	return toFullscreenView(baseView, windowTitle)
}

// windowTitle returns the terminal window title.
func (m *Model) windowTitle() string {
	if sessionTitle := m.sessionState.SessionTitle(); sessionTitle != "" {
		return sessionTitle + " - cagent"
	}
	return "cagent"
}

// cleanupAll cleans up all sessions, editors, and resources.
func (m *Model) cleanupAll() {
	for _, cp := range m.chatPages {
		cp.Cleanup()
	}
	for _, ed := range m.editors {
		ed.Cleanup()
	}
}

// openExternalEditor opens the current editor content in an external editor.
func (m *Model) openExternalEditor() (tea.Model, tea.Cmd) {
	content := m.editor.Value()

	// Create a temporary file with the current content
	tmpFile, err := os.CreateTemp("", "cagent-*.md")
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to create temp file: %v", err))
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to write temp file: %v", err))
	}
	tmpFile.Close()

	// Get the editor command (VISUAL, EDITOR, or platform default)
	editorCmd := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editorCmd == "" {
		if goruntime.GOOS == "windows" {
			editorCmd = "notepad"
		} else {
			editorCmd = "vi"
		}
	}

	// Parse editor command (may include arguments like "code --wait")
	parts := strings.Fields(editorCmd)
	args := append(parts[1:], tmpPath)
	cmd := exec.Command(parts[0], args...)

	ed := m.editor
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			os.Remove(tmpPath)
			return notification.ShowMsg{Text: fmt.Sprintf("Editor error: %v", err), Type: notification.TypeError}
		}

		updatedContent, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)

		if readErr != nil {
			return notification.ShowMsg{Text: fmt.Sprintf("Failed to read edited file: %v", readErr), Type: notification.TypeError}
		}

		// Trim trailing newline that editors often add
		c := strings.TrimSuffix(string(updatedContent), "\n")

		if strings.TrimSpace(c) == "" {
			ed.SetValue("")
		} else {
			ed.SetValue(c)
		}

		return nil
	})
}

// getEditorDisplayNameFromEnv returns a friendly display name for the configured editor.
func getEditorDisplayNameFromEnv(visual, editorEnv string) string {
	editorCmd := cmp.Or(visual, editorEnv)
	if editorCmd == "" {
		if goruntime.GOOS == "windows" {
			return "Notepad"
		}
		return "Vi"
	}

	parts := strings.Fields(editorCmd)
	if len(parts) == 0 {
		return "$EDITOR"
	}

	baseName := filepath.Base(parts[0])

	editorPrefixes := []struct {
		prefix string
		name   string
	}{
		{"code", "VSCode"},
		{"cursor", "Cursor"},
		{"nvim", "Neovim"},
		{"vim", "Vim"},
		{"vi", "Vi"},
		{"nano", "Nano"},
		{"emacs", "Emacs"},
		{"subl", "Sublime Text"},
		{"sublime", "Sublime Text"},
		{"atom", "Atom"},
		{"gedit", "gedit"},
		{"kate", "Kate"},
		{"notepad++", "Notepad++"},
		{"notepad", "Notepad"},
		{"textmate", "TextMate"},
		{"mate", "TextMate"},
		{"zed", "Zed"},
	}

	for _, e := range editorPrefixes {
		if strings.HasPrefix(baseName, e.prefix) {
			return e.name
		}
	}

	if baseName != "" {
		return strings.ToUpper(baseName[:1]) + baseName[1:]
	}

	return "$EDITOR"
}

func toFullscreenView(content, windowTitle string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background
	view.WindowTitle = windowTitle
	return view
}
