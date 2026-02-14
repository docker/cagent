// Package tui provides the top-level TUI model with tab and session management.
package tui

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
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
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
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/service/supervisor"
	"github.com/docker/cagent/pkg/tui/service/tuistate"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/userconfig"
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
	appPaddingHorizontal = 2 * styles.AppPadding
)

// Model is the top-level TUI model that wraps the chat page.
type appModel struct {
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

	// pendingRestores maps runtime tab IDs (supervisor routing keys) to
	// persisted session-store IDs. When a tab with a pending restore is first
	// switched to, the persisted session is loaded via replaceActiveSession —
	// the same code path as the /sessions command.
	//
	// This map also serves as the authoritative source for "which persisted
	// session ID does this tab represent?" until the restore completes, at
	// which point the app's live session ID takes over.
	pendingRestores map[string]string

	// pendingSidebarCollapsed maps runtime tab IDs to their persisted sidebar
	// collapsed state. Consumed when a chat page is first created for a
	// restored tab (in handleSwitchTab) and then removed from the map.
	pendingSidebarCollapsed map[string]bool

	// pendingActiveTab is the tab ID to switch to on Init(). Set when the
	// previously focused tab differs from the initial tab.
	pendingActiveTab string

	ready bool
	err   error
}

// New creates a new Model.
func New(ctx context.Context, spawner SessionSpawner, initialApp *app.App, initialWorkingDir string, cleanup func()) tea.Model {
	// Initialize supervisor
	sv := supervisor.New(spawner)

	// Initialize tab bar with configurable title length from user settings
	tabTitleMaxLen := userconfig.Get().GetTabTitleMaxLength()
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

	m := &appModel{
		supervisor:              sv,
		tabBar:                  tb,
		tuiStore:                ts,
		chatPages:               map[string]chat.Page{sessID: initialChatPage},
		sessionStates:           map[string]*service.SessionState{sessID: initialSessionState},
		editors:                 map[string]editor.Editor{sessID: initialEditor},
		application:             initialApp,
		sessionState:            initialSessionState,
		chatPage:                initialChatPage,
		editor:                  initialEditor,
		history:                 historyStore,
		pendingRestores:         make(map[string]string),
		pendingSidebarCollapsed: make(map[string]bool),
		notification:            notification.New(),
		dialogMgr:               dialog.New(),
		completions:             completion.New(),
		workingSpinner:          spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		focusedPanel:            PanelEditor,
		editorLines:             3,
	}

	// Initialize status bar (pass m as help provider)
	m.statusBar = statusbar.New(m)

	// Add the initial session to the supervisor
	sv.AddSession(ctx, initialApp, initialApp.Session(), initialWorkingDir, cleanup)

	// Restore persisted tabs or persist the initial one.
	m.restoreTabs(ctx, ts, sv, spawner, initialApp, sessID, initialWorkingDir)

	// Initialize tab bar with current tabs
	tabs, activeIdx := sv.GetTabs()
	tb.SetTabs(tabs, activeIdx)
	m.statusBar.SetShowNewTab(tb.Height() == 0)

	// Make sure to stop on context cancellation.
	// Note: chatPages/editors cleanup is handled by cleanupAll() on the
	// normal exit path (ExitConfirmedMsg). We don't iterate those maps
	// here to avoid racing with the Bubble Tea event loop.
	go func() {
		<-ctx.Done()
		if ts != nil {
			_ = ts.Close()
		}
		sv.Shutdown()
	}()

	return m
}

// SetProgram sets the tea.Program for the supervisor to send routed messages.
func (m *appModel) SetProgram(p *tea.Program) {
	m.supervisor.SetProgram(p)
}

// reapplyKeyboardEnhancements forwards the cached keyboard enhancements message
// to the active chat page and editor so new/replaced instances pick up the
// terminal's key disambiguation support.
func (m *appModel) reapplyKeyboardEnhancements() {
	if m.keyboardEnhancements == nil {
		return
	}
	updated, _ := m.chatPage.Update(*m.keyboardEnhancements)
	m.chatPage = updated.(chat.Page)
	editorModel, _ := m.editor.Update(*m.keyboardEnhancements)
	m.editor = editorModel.(editor.Editor)
}

// initSessionComponents creates a new chat page, session state, and editor for
// the given app and stores them in the per-session maps under tabID. The active
// convenience pointers (m.chatPage, m.sessionState, m.editor) are also updated.
func (m *appModel) initSessionComponents(tabID string, a *app.App, sess *session.Session) {
	ss := service.NewSessionState(sess)
	cp := chat.New(a, ss)
	ed := editor.New(a, m.history)

	m.chatPages[tabID] = cp
	m.sessionStates[tabID] = ss
	m.editors[tabID] = ed

	m.application = a
	m.sessionState = ss
	m.chatPage = cp
	m.editor = ed
}

// initAndFocusComponents returns a batch of commands that initializes and focuses
// the active chat page and editor, then resizes everything.
func (m *appModel) initAndFocusComponents() tea.Cmd {
	m.reapplyKeyboardEnhancements()
	return tea.Batch(
		m.chatPage.Init(),
		m.editor.Init(),
		m.editor.Focus(),
		m.resizeAll(),
	)
}

// persistActiveTab writes the active tab ID to the tuistate store.
func (m *appModel) persistActiveTab(persistedID string) {
	if m.tuiStore == nil {
		return
	}
	if err := m.tuiStore.SetActiveTab(context.Background(), persistedID); err != nil {
		slog.Warn("Failed to set active tab", "error", err)
	}
}

// persistFreshTab clears the tab store and writes a single initial tab.
func (m *appModel) persistFreshTab(ctx context.Context, sessionID, workingDir string) {
	if m.tuiStore == nil {
		return
	}
	if err := m.tuiStore.ClearTabs(ctx); err != nil {
		slog.Warn("Failed to clear tabs", "error", err)
	}
	if err := m.tuiStore.AddTab(ctx, sessionID, workingDir); err != nil {
		slog.Warn("Failed to persist initial tab", "error", err)
	}
	if err := m.tuiStore.SetActiveTab(ctx, sessionID); err != nil {
		slog.Warn("Failed to set active tab", "error", err)
	}
}

// restoreTabs restores previously persisted tabs (if enabled) or persists the
// initial session as the sole tab. The tuistate DB always stores persisted
// session-store IDs. Runtime tab/routing IDs are ephemeral; the pendingRestores
// map bridges the two:  pendingRestores[runtimeTabID] = persistedSessionID
func (m *appModel) restoreTabs(
	ctx context.Context,
	ts *tuistate.Store,
	sv *supervisor.Supervisor,
	spawner SessionSpawner,
	initialApp *app.App,
	initialTabID, initialWorkingDir string,
) {
	if ts == nil {
		return
	}

	var savedTabs []tuistate.TabEntry
	var savedActiveID string
	if *userconfig.Get().RestoreTabs {
		savedTabs, savedActiveID, _ = ts.GetTabs(ctx)
	}

	if len(savedTabs) == 0 {
		m.persistFreshTab(ctx, initialTabID, initialWorkingDir)
		return
	}

	sessionStore := initialApp.SessionStore()
	restoredFirst := false

	for _, saved := range savedTabs {
		// Validate the saved session still exists.
		if sessionStore != nil && saved.SessionID != "" {
			if _, err := sessionStore.GetSession(ctx, saved.SessionID); err != nil {
				slog.Warn("Saved session no longer exists, removing stale tab",
					"session_id", saved.SessionID, "error", err)
				_ = ts.RemoveTab(ctx, saved.SessionID)
				continue
			}
		}

		// Determine the runtime tab ID to use.
		var runtimeID string
		if !restoredFirst {
			restoredFirst = true
			runtimeID = initialTabID
		} else {
			a, newSess, spawnCleanup, err := spawner(ctx, saved.WorkingDir)
			if err != nil {
				slog.Warn("Failed to restore tab", "working_dir", saved.WorkingDir, "error", err)
				_ = ts.RemoveTab(ctx, saved.SessionID)
				continue
			}
			runtimeID = sv.AddSession(ctx, a, newSess, saved.WorkingDir, spawnCleanup)
		}

		// Stash persisted session ID for lazy loading on first switch.
		m.pendingRestores[runtimeID] = saved.SessionID
		if saved.SidebarCollapsed {
			m.pendingSidebarCollapsed[runtimeID] = true
		}

		// If this was the active tab, queue a switch on Init().
		if saved.SessionID == savedActiveID {
			if restoredFirst && runtimeID == initialTabID {
				_ = ts.SetActiveTab(ctx, saved.SessionID)
			} else {
				m.pendingActiveTab = runtimeID
			}
		}

		// Peek at the session title so the tab bar shows a name before lazy load.
		if sessionStore != nil && saved.SessionID != "" {
			if oldSess, err := sessionStore.GetSession(ctx, saved.SessionID); err == nil && oldSess.Title != "" {
				sv.SetRunnerTitle(runtimeID, oldSess.Title)
			}
		}
	}

	// If all saved tabs were stale, persist the initial session.
	if !restoredFirst {
		m.persistFreshTab(ctx, initialTabID, initialWorkingDir)
	}
}

// Init initializes the model.
func (m *appModel) Init() tea.Cmd {
	// If a different tab should be active on startup, switch to it directly.
	// The initial tab's pending restore stays lazy — it will be loaded via
	// handleSwitchTab when the user eventually opens it, just like every
	// other non-active restored tab.
	if m.pendingActiveTab != "" {
		tabID := m.pendingActiveTab
		m.pendingActiveTab = ""
		_, switchCmd := m.handleSwitchTab(tabID)
		return tea.Batch(m.dialogMgr.Init(), switchCmd)
	}

	// If the initial tab has a pending session restore, go through
	// replaceActiveSession — the same code path as the /sessions command.
	activeID := m.supervisor.ActiveID()
	if oldSessionID, ok := m.pendingRestores[activeID]; ok {
		delete(m.pendingRestores, activeID)
		if store := m.application.SessionStore(); store != nil {
			if sess, err := store.GetSession(context.Background(), oldSessionID); err == nil {
				_, cmd := m.replaceActiveSession(context.Background(), sess)

				if m.tuiStore != nil && sess.WorkingDir != "" {
					if err := m.tuiStore.UpdateTabWorkingDir(context.Background(), oldSessionID, sess.WorkingDir); err != nil {
						slog.Warn("Failed to update persisted working dir", "error", err)
					}
				}

				cmd = tea.Batch(cmd, m.applySidebarCollapsed(activeID))
				m.persistActiveTab(sess.ID)

				return tea.Batch(m.dialogMgr.Init(), cmd)
			}
		}
	}

	return tea.Batch(
		m.dialogMgr.Init(),
		m.chatPage.Init(),
		m.editor.Init(),
		m.editor.Focus(),
		m.application.SendFirstMessage(),
	)
}

// Update handles messages.
func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// Forward frame to tab bar for running indicator animation
		m.tabBar.SetAnimFrame(msg.Frame)
		if animation.HasActive() {
			cmds = append(cmds, animation.StartTick())
		}
		return m, tea.Batch(cmds...)

	// --- Tab management ---

	case messages.TabsUpdatedMsg:
		prevHeight := m.tabBar.Height()
		m.tabBar.SetTabs(msg.Tabs, msg.ActiveIdx)
		m.statusBar.SetShowNewTab(m.tabBar.Height() == 0)
		if m.tabBar.Height() != prevHeight {
			cmd := m.resizeAll()
			return m, cmd
		}
		return m, nil

	case messages.SpawnSessionMsg:
		return m.handleSpawnSession(msg.WorkingDir)

	case messages.SwitchTabMsg:
		return m.handleSwitchTab(msg.SessionID)

	case messages.CloseTabMsg:
		return m.handleCloseTab(msg.SessionID)

	case messages.ToggleSidebarMsg:
		if m.tuiStore != nil {
			persistedID := m.persistedSessionID(m.supervisor.ActiveID())
			if err := m.tuiStore.ToggleSidebarCollapsed(context.Background(), persistedID); err != nil {
				slog.Warn("Failed to persist sidebar collapsed state", "error", err)
			}
		}
		return m, nil

	// --- Focus requests from content view ---

	case messages.RequestFocusMsg:
		switch msg.Target {
		case messages.PanelMessages:
			if m.focusedPanel != PanelContent {
				m.focusedPanel = PanelContent
				m.statusBar.InvalidateCache()
				m.editor.Blur()
			}
			if msg.ClickX != 0 || msg.ClickY != 0 {
				return m, m.chatPage.FocusMessageAt(msg.ClickX, msg.ClickY)
			}
			return m, m.chatPage.FocusMessages()
		case messages.PanelSidebarTitle:
			if m.focusedPanel != PanelContent {
				m.focusedPanel = PanelContent
				m.statusBar.InvalidateCache()
				m.chatPage.BlurMessages()
				m.editor.Blur()
			}
			return m, nil
		case messages.PanelEditor:
			if m.focusedPanel != PanelEditor {
				m.focusedPanel = PanelEditor
				m.statusBar.InvalidateCache()
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

	// --- Terminal bell ---

	case messages.BellMsg:
		// Ring the terminal bell to alert the user that an inactive tab needs attention.
		// The BEL character (\a) is written to stderr which is typically the terminal.
		_, _ = fmt.Fprint(os.Stderr, "\a")
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
		// /new spawns a new tab when a session spawner is configured.
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
		return m.handleToggleSplitDiff()

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
		return m, notification.InfoCmd("Speech-to-text is not yet supported")

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
func (m *appModel) handleRoutedMsg(msg messages.RoutedMsg) (tea.Model, tea.Cmd) {
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

	// Update session state for inactive sessions
	if event, isRuntimeEvent := msg.Inner.(runtime.Event); isRuntimeEvent {
		if sessionState, ok := m.sessionStates[msg.SessionID]; ok {
			if agentName := event.GetAgentName(); agentName != "" {
				sessionState.SetCurrentAgentName(agentName)
			}
		}
	}

	// Update the inactive chat page (discard cmds — UI effects aren't needed for hidden pages)
	updated, _ := chatPage.Update(msg.Inner)
	m.chatPages[msg.SessionID] = updated.(chat.Page)
	return m, nil
}

// handleWorkingStateChanged updates the editor working indicator and resize handle spinner.
func (m *appModel) handleWorkingStateChanged(msg messages.WorkingStateChangedMsg) (tea.Model, tea.Cmd) {
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
func (m *appModel) handleOpenSessionBrowser() (tea.Model, tea.Cmd) {
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
func (m *appModel) handleLoadSession(sessionID string) (tea.Model, tea.Cmd) {
	store := m.application.SessionStore()
	if store == nil {
		return m, notification.ErrorCmd("No session store configured")
	}

	sess, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
	}

	// Check if this session is already open in another tab — switch instead of duplicating.
	if tabID := m.findTabByPersistedID(sessionID); tabID != "" {
		return m.handleSwitchTab(tabID)
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
		activeID := m.supervisor.ActiveID()
		oldPersistedID := m.persistedSessionID(activeID)

		model, cmd := m.replaceActiveSession(ctx, sess)

		// Update tuistate: replace old persisted ID with the loaded session's ID
		if m.tuiStore != nil {
			if err := m.tuiStore.UpdateTabSessionID(ctx, oldPersistedID, sess.ID); err != nil {
				slog.Warn("Failed to update tab session ID after in-place load", "error", err)
			}
			if sess.WorkingDir != "" {
				if err := m.tuiStore.UpdateTabWorkingDir(ctx, sess.ID, sess.WorkingDir); err != nil {
					slog.Warn("Failed to update tab working dir after in-place load", "error", err)
				}
			}
		}
		m.persistActiveTab(sess.ID)
		return model, cmd
	}

	slog.Debug("Loading session into new tab", "session_id", sessionID)

	// Spawn a new tab.
	newSessionID, err := m.supervisor.SpawnSession(ctx, workingDir)
	if err != nil {
		return m, notification.ErrorCmd("Failed to create tab: " + err.Error())
	}

	// Persist the new tab using the loaded session's persisted ID (not the ephemeral tab ID).
	if m.tuiStore != nil {
		if err := m.tuiStore.AddTab(ctx, sess.ID, workingDir); err != nil {
			slog.Warn("Failed to persist loaded session tab", "error", err)
		}
	}

	// Switch to the new tab so m.application points to the new app.
	model, switchCmd := m.handleSwitchTab(newSessionID)

	// Replace the blank session with the loaded one and rebuild all components.
	m.application.ReplaceSession(ctx, sess)
	m.initSessionComponents(newSessionID, m.application, sess)

	if sess.Title != "" {
		m.supervisor.SetRunnerTitle(newSessionID, sess.Title)
	}

	m.persistActiveTab(sess.ID)

	return model, tea.Batch(
		switchCmd,
		m.initAndFocusComponents(),
	)
}

// replaceActiveSession replaces the current (empty) tab's session with a loaded one in-place.
// If the loaded session's working directory differs from the runner's current one,
// a fresh runtime is spawned via the supervisor so that tools operate in the correct directory.
func (m *appModel) replaceActiveSession(ctx context.Context, sess *session.Session) (tea.Model, tea.Cmd) {
	activeID := m.supervisor.ActiveID()

	slog.Debug("Replacing empty session in-place", "tab_id", activeID, "loaded_session", sess.ID)

	// Cleanup old chat page and editor for the active session
	if cp, ok := m.chatPages[activeID]; ok {
		cp.Cleanup()
	}
	if ed, ok := m.editors[activeID]; ok {
		ed.Cleanup()
	}

	// If the loaded session's working directory differs from the runner's,
	// we need a fresh runtime whose tools operate in the correct directory.
	runner := m.supervisor.GetRunner(activeID)
	sessWorkingDir := sess.WorkingDir
	if sessWorkingDir != "" && runner != nil && sessWorkingDir != runner.WorkingDir {
		newApp, _, spawnCleanup, err := m.supervisor.Spawner()(ctx, sessWorkingDir)
		if err == nil {
			slog.Debug("Respawning runtime for working dir mismatch",
				"tab_id", activeID,
				"old_dir", runner.WorkingDir,
				"new_dir", sessWorkingDir)
			m.supervisor.ReplaceRunnerApp(ctx, activeID, newApp, sessWorkingDir, spawnCleanup)
			m.application = newApp
		} else {
			slog.Warn("Failed to respawn runtime for working dir, using existing",
				"working_dir", sessWorkingDir, "error", err)
		}
	}

	// Replace the session in the app and rebuild all per-session components.
	m.application.ReplaceSession(ctx, sess)
	m.initSessionComponents(activeID, m.application, sess)

	if sess.Title != "" {
		m.supervisor.SetRunnerTitle(activeID, sess.Title)
	}

	cmd := m.initAndFocusComponents()
	return m, cmd
}

// handleSpawnSession spawns a new session.
func (m *appModel) handleSpawnSession(workingDir string) (tea.Model, tea.Cmd) {
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

	// Persist the new tab (for new tabs, persisted ID == runtime tab ID).
	if m.tuiStore != nil {
		if err := m.tuiStore.AddTab(ctx, sessionID, workingDir); err != nil {
			slog.Warn("Failed to persist new tab", "error", err)
		}
	}

	// Switch to the new session
	return m.handleSwitchTab(sessionID)
}

// openWorkingDirPicker opens the working directory picker dialog.
func (m *appModel) openWorkingDirPicker() (tea.Model, tea.Cmd) {
	var recentDirs, favoriteDirs []string
	if m.tuiStore != nil {
		recentDirs, _ = m.tuiStore.GetRecentDirs(context.Background(), 10)
		favoriteDirs, _ = m.tuiStore.GetFavoriteDirs(context.Background())
	}

	// Use the active session's working directory so the picker reflects it
	// instead of the process CWD.
	var sessionWorkingDir string
	if runner := m.supervisor.GetRunner(m.supervisor.ActiveID()); runner != nil {
		sessionWorkingDir = runner.WorkingDir
	}

	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewWorkingDirPickerDialog(recentDirs, favoriteDirs, m.tuiStore, sessionWorkingDir),
	})
}

// handleSwitchTab switches to a different session.
// Existing chat pages and editors are preserved (not recreated) so that in-flight streaming
// content and draft text are retained when switching back to a tab.
func (m *appModel) handleSwitchTab(sessionID string) (tea.Model, tea.Cmd) {
	runner := m.supervisor.SwitchTo(sessionID)
	if runner == nil {
		return m, notification.ErrorCmd("Session not found")
	}

	// Blur current editor before switching
	m.editor.Blur()

	// If this tab has a pending session restore, load it through
	// replaceActiveSession — the same code path as the /sessions command.
	if oldSessionID, ok := m.pendingRestores[sessionID]; ok {
		delete(m.pendingRestores, sessionID)
		m.application = runner.App
		if store := runner.App.SessionStore(); store != nil {
			if sess, err := store.GetSession(context.Background(), oldSessionID); err == nil {
				m.persistActiveTab(sess.ID)
				model, cmd := m.replaceActiveSession(context.Background(), sess)

				if m.tuiStore != nil && sess.WorkingDir != "" {
					if err := m.tuiStore.UpdateTabWorkingDir(context.Background(), oldSessionID, sess.WorkingDir); err != nil {
						slog.Warn("Failed to update persisted working dir", "error", err)
					}
				}

				cmd = tea.Batch(cmd, m.applySidebarCollapsed(sessionID))
				return model, cmd
			}
		}
		// Fall through to normal tab switch if session couldn't be loaded.
	}

	// Get or create per-session components.
	_, pageExists := m.chatPages[sessionID]
	_, editorExists := m.editors[sessionID]

	if !pageExists || !editorExists {
		// Create all missing components at once.
		m.initSessionComponents(sessionID, runner.App, runner.App.Session())
		m.applySidebarCollapsed(sessionID)
	} else {
		// Reuse existing components — just update convenience pointers.
		m.application = runner.App
		m.sessionState = m.sessionStates[sessionID]
		m.chatPage = m.chatPages[sessionID]
		m.editor = m.editors[sessionID]
	}

	m.reapplyKeyboardEnhancements()
	m.persistActiveTab(m.persistedSessionID(sessionID))

	// Sync editor working state and reset working spinner.
	m.editor.SetWorking(m.chatPage.IsWorking())
	m.workingSpinner.Stop()
	m.workingSpinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)

	var cmds []tea.Cmd

	if !pageExists || !editorExists {
		if !pageExists {
			cmds = append(cmds, m.chatPage.Init())
		}
		if !editorExists {
			cmds = append(cmds, m.editor.Init())
		}
		cmds = append(cmds, m.editor.Focus(), m.resizeAll())
	} else {
		cmds = append(cmds, m.resizeAll(), m.chatPage.ScrollToBottom(), m.editor.Focus())
	}

	if m.chatPage.IsWorking() {
		cmds = append(cmds, m.workingSpinner.Init())
	}
	if pendingCmd := m.replayPendingEvent(sessionID); pendingCmd != nil {
		cmds = append(cmds, pendingCmd)
	}

	return m, tea.Batch(cmds...)
}

// applySidebarCollapsed applies and consumes the persisted sidebar collapsed state
// for the given tab ID. Returns a resize command if the state was applied, nil otherwise.
func (m *appModel) applySidebarCollapsed(sessionID string) tea.Cmd {
	collapsed, ok := m.pendingSidebarCollapsed[sessionID]
	if !ok {
		return nil
	}
	m.chatPage.SetSidebarSettings(chat.SidebarSettings{Collapsed: collapsed})
	delete(m.pendingSidebarCollapsed, sessionID)
	return m.resizeAll()
}

// replayPendingEvent checks if a session has a pending attention event (e.g. tool confirmation,
// max iterations, elicitation) that was received while the tab was inactive.
// If found, it opens the appropriate dialog. The event was already processed by the chat page
// (updating the message list), but the dialog command was discarded for inactive sessions.
func (m *appModel) replayPendingEvent(sessionID string) tea.Cmd {
	pendingEvent := m.supervisor.ConsumePendingEvent(sessionID)
	if pendingEvent == nil {
		return nil
	}

	sessionState, ok := m.sessionStates[sessionID]
	if !ok {
		return nil
	}

	switch ev := pendingEvent.(type) {
	case *runtime.ToolCallConfirmationEvent:
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewToolConfirmationDialog(ev, sessionState),
		})

	case *runtime.MaxIterationsReachedEvent:
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewMaxIterationsDialog(ev.MaxIterations, m.application),
		})

	case *runtime.ElicitationRequestEvent:
		return m.replayElicitationEvent(ev)
	}

	return nil
}

// replayElicitationEvent opens the appropriate elicitation dialog for a pending event.
func (m *appModel) replayElicitationEvent(ev *runtime.ElicitationRequestEvent) tea.Cmd {
	// Check if this is an OAuth flow
	if ev.Meta != nil {
		if elicitationType, ok := ev.Meta["cagent/type"].(string); ok && elicitationType == "oauth_flow" {
			var serverURL string
			if url, ok := ev.Meta["cagent/server_url"].(string); ok {
				serverURL = url
			}
			return core.CmdHandler(dialog.OpenDialogMsg{
				Model: dialog.NewOAuthAuthorizationDialog(serverURL, m.application),
			})
		}
	}

	switch ev.Mode {
	case "url":
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewURLElicitationDialog(ev.Message, ev.URL),
		})
	default:
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewElicitationDialog(ev.Message, ev.Schema, ev.Meta),
		})
	}
}

// handleCloseTab closes a session tab.
func (m *appModel) handleCloseTab(sessionID string) (tea.Model, tea.Cmd) {
	wasActive := sessionID == m.supervisor.ActiveID()

	// Capture the working dir before closing so we can reuse it if this is the last tab.
	var closedWorkingDir string
	if runner := m.supervisor.GetRunner(sessionID); runner != nil {
		closedWorkingDir = runner.WorkingDir
	}

	// Compute persisted session-store ID *before* closing (runner goes away).
	persistedID := m.persistedSessionID(sessionID)

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
	delete(m.pendingRestores, sessionID)
	delete(m.pendingSidebarCollapsed, sessionID)

	var cmds []tea.Cmd
	// Remove from persistent store using the persisted session-store ID.
	if m.tuiStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := m.tuiStore.RemoveTab(ctx, persistedID); err != nil {
			slog.Error("Failed to remove tab from store", "error", err)
			cmds = append(cmds, notification.ErrorCmd(fmt.Sprintf("Failed to remove tab from tui state db: %v", err)))
		}
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

	return m, tea.Batch(cmds...)
}

// handleWindowResize handles window resize.
func (m *appModel) handleWindowResize(width, height int) tea.Cmd {
	m.wWidth, m.wHeight = width, height

	m.statusBar.SetWidth(width)
	m.tabBar.SetWidth(width - appPaddingHorizontal)

	m.width = width
	m.height = height

	if !m.ready {
		m.ready = true
	}

	return m.resizeAll()
}

// resizeAll recalculates all component sizes based on current window dimensions.
func (m *appModel) resizeAll() tea.Cmd {
	var cmds []tea.Cmd

	width, height := m.width, m.height

	// Calculate fixed heights
	tabBarHeight := m.tabBar.Height()
	statusBarHeight := m.statusBar.Height()
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
func (m *appModel) Help() help.KeyMap {
	return core.NewSimpleHelp(m.Bindings())
}

// Bindings returns the key bindings shown in the status bar.
func (m *appModel) Bindings() []key.Binding {
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
func (m *appModel) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Dialog gets priority when open
	if m.dialogMgr.Open() {
		u, cmd := m.dialogMgr.Update(msg)
		m.dialogMgr = u.(dialog.Manager)
		return m, cmd
	}

	// Tab bar keys (Ctrl+t, Ctrl+p, Ctrl+n, Ctrl+w)
	if cmd := m.tabBar.Update(msg); cmd != nil {
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
func (m *appModel) switchFocus() (tea.Model, tea.Cmd) {
	switch m.focusedPanel {
	case PanelEditor:
		// Check if editor has a suggestion to accept first
		if cmd := m.editor.AcceptSuggestion(); cmd != nil {
			return m, cmd
		}
		m.focusedPanel = PanelContent
		m.statusBar.InvalidateCache()
		m.editor.Blur()
		return m, m.chatPage.FocusMessages()
	case PanelContent:
		m.focusedPanel = PanelEditor
		m.statusBar.InvalidateCache()
		m.chatPage.BlurMessages()
		return m, m.editor.Focus()
	}
	return m, nil
}

// handleMouseClick routes mouse clicks to the appropriate component based on Y coordinate.
func (m *appModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Dialogs use full-window coordinates (they're positioned over the entire screen)
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

	case regionResizeHandle:
		if msg.Button == tea.MouseLeft {
			m.isDragging = true
		}
		return m, nil

	case regionTabBar:
		// Adjust coordinates for tab bar (relative to its start, accounting for padding)
		adjustedMsg := msg
		adjustedMsg.X = msg.X - styles.AppPadding
		adjustedMsg.Y = msg.Y - m.contentHeight - 1
		if cmd := m.tabBar.Update(adjustedMsg); cmd != nil {
			return m, cmd
		}
		return m, nil

	case regionEditor:
		// Focus editor on click
		if m.focusedPanel != PanelEditor {
			m.focusedPanel = PanelEditor
			m.statusBar.InvalidateCache()
			m.chatPage.BlurMessages()
		}
		// Adjust coordinates for editor padding
		adjustedMsg := msg
		adjustedMsg.X = msg.X - styles.AppPadding
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, tea.Batch(cmd, m.editor.Focus())

	case regionStatusBar:
		if msg.Button == tea.MouseLeft && m.statusBar.ClickedNewTab(msg.X) {
			return m.handleSpawnSession("")
		}
	}

	return m, nil
}

// handleMouseMotion routes mouse motion events with adjusted coordinates.
func (m *appModel) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
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
	region := m.hitTestRegion(msg.Y)
	m.isHoveringHandle = region == regionResizeHandle
	switch region {
	case regionContent:
		updated, cmd := m.chatPage.Update(msg)
		m.chatPage = updated.(chat.Page)
		return m, cmd
	case regionEditor:
		adjustedMsg := msg
		adjustedMsg.X = msg.X - styles.AppPadding
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd
	}

	return m, nil
}

// handleMouseRelease routes mouse release events with adjusted coordinates.
func (m *appModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
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
		adjustedMsg.X = msg.X - styles.AppPadding
		adjustedMsg.Y = msg.Y - m.editorTop()
		editorModel, cmd := m.editor.Update(adjustedMsg)
		m.editor = editorModel.(editor.Editor)
		return m, cmd
	}

	return m, nil
}

// handleMouseWheel routes mouse wheel events with adjusted coordinates.
func (m *appModel) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
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
func (m *appModel) handleWheelCoalesced(msg messages.WheelCoalescedMsg) (tea.Model, tea.Cmd) {
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
func (m *appModel) hitTestRegion(y int) layoutRegion {
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
func (m *appModel) editorTop() int {
	return m.contentHeight + 1 + m.tabBar.Height()
}

// handleEditorResize adjusts editor height based on drag position.
func (m *appModel) handleEditorResize(y int) tea.Cmd {
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
func (m *appModel) renderResizeHandle(width int) string {
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

	var result string
	switch {
	case m.chatPage.IsWorking():
		// Truncate right side and append spinner (handle stays centered)
		workingText := "Working…"
		if queueLen := m.chatPage.QueueLength(); queueLen > 0 {
			workingText = fmt.Sprintf("Working… (%d queued)", queueLen)
		}
		suffix := " " + m.workingSpinner.View() + " " + styles.SpinnerDotsHighlightStyle.Render(workingText)
		cancelKeyPart := styles.HighlightWhiteStyle.Render("Esc")
		suffix += " (" + cancelKeyPart + " to interrupt)"
		suffixWidth := lipgloss.Width(suffix)
		result = lipgloss.NewStyle().MaxWidth(innerWidth-suffixWidth).Render(fullLine) + suffix

	case m.chatPage.QueueLength() > 0:
		queueText := fmt.Sprintf("%d queued", m.chatPage.QueueLength())
		suffix := " " + styles.WarningStyle.Render(queueText) + " "
		suffixWidth := lipgloss.Width(suffix)
		result = lipgloss.NewStyle().MaxWidth(innerWidth-suffixWidth).Render(fullLine) + suffix

	default:
		result = fullLine
	}

	return lipgloss.NewStyle().Padding(0, styles.AppPadding).Render(result)
}

// View renders the model.
func (m *appModel) View() tea.View {
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

	// Combine: content | resize handle | [tab bar] | editor | status bar
	viewParts := []string{
		contentView,
		resizeHandle,
	}
	if tabBarView != "" {
		viewParts = append(viewParts, lipgloss.NewStyle().
			Padding(0, styles.AppPadding).
			Render(tabBarView))
	}
	viewParts = append(viewParts, editorView)
	if statusBarView != "" {
		viewParts = append(viewParts, statusBarView)
	}
	baseView := lipgloss.JoinVertical(lipgloss.Top, viewParts...)

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
func (m *appModel) windowTitle() string {
	if sessionTitle := m.sessionState.SessionTitle(); sessionTitle != "" {
		return sessionTitle + " - cagent"
	}
	return "cagent"
}

// cleanupAll cleans up all sessions, editors, and resources.
func (m *appModel) cleanupAll() {
	for _, cp := range m.chatPages {
		cp.Cleanup()
	}
	for _, ed := range m.editors {
		ed.Cleanup()
	}
}

// persistedSessionID returns the session-store ID that should be used for
// tuistate persistence for the given runtime tab ID.
//
// If the tab has a pending restore (session not yet lazily loaded), the
// persisted ID from pendingRestores is returned — this is the original
// session-store ID that was saved across restarts. Otherwise the live
// session ID from the app is used.
func (m *appModel) persistedSessionID(tabID string) string {
	if persistedID, ok := m.pendingRestores[tabID]; ok {
		return persistedID
	}
	if runner := m.supervisor.GetRunner(tabID); runner != nil {
		return runner.App.Session().ID
	}
	return tabID
}

// findTabByPersistedID scans all open tabs and returns the runtime tab ID
// whose persisted session-store ID matches the given ID. Returns "" if not found.
func (m *appModel) findTabByPersistedID(persistedID string) string {
	// Check pending restores first (tabs not yet lazily loaded).
	for tabID, pid := range m.pendingRestores {
		if pid == persistedID {
			return tabID
		}
	}
	// Check live sessions.
	tabs, _ := m.supervisor.GetTabs()
	for _, tab := range tabs {
		if runner := m.supervisor.GetRunner(tab.SessionID); runner != nil {
			if runner.App.Session().ID == persistedID {
				return tab.SessionID
			}
		}
	}
	return ""
}

// openExternalEditor opens the current editor content in an external editor.
func (m *appModel) openExternalEditor() (tea.Model, tea.Cmd) {
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
