package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/audio/transcribe"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/animation"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/subscription"
)

// FocusedPanel represents which panel is currently focused
type FocusedPanel string

const (
	panelContent FocusedPanel = "content"
	panelEditor  FocusedPanel = "editor"

	// appPaddingHorizontal is total horizontal padding from AppStyle (left + right)
	appPaddingHorizontal = 2
	// resizeHandleWidth is the width of the draggable center portion of the resize handle
	resizeHandleWidth = 8
)

// appModel represents the main application model
type appModel struct {
	application     *app.App
	wWidth, wHeight int
	width, height   int
	keyMap          KeyMap

	chatPage  chat.Page
	editor    editor.Editor
	statusBar statusbar.StatusBar

	// Working state indicator (resize handle spinner)
	workingSpinner spinner.Spinner

	notification notification.Manager
	dialog       dialog.Manager
	completions  completion.Manager

	sessionState *service.SessionState

	transcriber *transcribe.Transcriber

	// External event subscriptions (Elm Architecture pattern)
	themeWatcher      *styles.ThemeWatcher
	themeSubscription *subscription.ChannelSubscription[string]
	themeSubStarted   bool

	// Content area height
	contentHeight int

	// Editor resize state
	editorLines int
	isDragging  bool

	// Focus state
	focusedPanel FocusedPanel

	// keyboardEnhancements stores the last keyboard enhancements message from the terminal.
	keyboardEnhancements *tea.KeyboardEnhancementsMsg

	// keyboardEnhancementsSupported tracks whether the terminal supports keyboard enhancements
	keyboardEnhancementsSupported bool

	ready bool
	err   error
}

// KeyMap defines global key bindings
type KeyMap struct {
	Quit                  key.Binding
	Suspend               key.Binding
	CommandPalette        key.Binding
	ToggleYolo            key.Binding
	ToggleHideToolResults key.Binding
	CycleAgent            key.Binding
	ModelPicker           key.Binding
	Speak                 key.Binding
	ClearQueue            key.Binding
}

// DefaultKeyMap returns the default global key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("Ctrl+c", "quit"),
		),
		Suspend: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("Ctrl+z", "suspend"),
		),
		CommandPalette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("Ctrl+p", "commands"),
		),
		ToggleYolo: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("Ctrl+y", "toggle yolo mode"),
		),
		ToggleHideToolResults: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("Ctrl+o", "toggle tool output"),
		),
		CycleAgent: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("Ctrl+s", "cycle agent"),
		),
		ModelPicker: key.NewBinding(
			key.WithKeys("ctrl+m"),
			key.WithHelp("Ctrl+m", "models"),
		),
		Speak: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("Ctrl+l", "speak"),
		),
		ClearQueue: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("Ctrl+x", "clear queue"),
		),
	}
}

// New creates and initializes a new TUI application model
func New(ctx context.Context, a *app.App) tea.Model {
	sessionState := service.NewSessionState(a.Session())

	// Create a channel for theme file change events
	themeEventCh := make(chan string, 1)

	// Initialize shared command history
	historyStore, err := history.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize command history: %v\n", err)
	}

	t := &appModel{
		keyMap:         DefaultKeyMap(),
		dialog:         dialog.New(),
		notification:   notification.New(),
		completions:    completion.New(),
		application:    a,
		sessionState:   sessionState,
		transcriber:    transcribe.New(os.Getenv("OPENAI_API_KEY")),
		workingSpinner: spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		focusedPanel:   panelEditor,
		editorLines:    3,
		// Set up theme subscription using the subscription package
		themeSubscription: subscription.NewChannelSubscription(themeEventCh, func(themeRef string) tea.Msg {
			return messages.ThemeFileChangedMsg{ThemeRef: themeRef}
		}),
	}

	// Create theme watcher with callback that sends to the subscription channel
	t.themeWatcher = styles.NewThemeWatcher(func(themeRef string) {
		select {
		case themeEventCh <- themeRef:
		default:
		}
	})

	t.statusBar = statusbar.New(t)
	t.chatPage = chat.New(a, sessionState)
	t.editor = editor.New(a, historyStore)

	// Start watching the current theme (if it's a user theme file)
	currentTheme := styles.CurrentTheme()
	if currentTheme != nil && currentTheme.Ref != "" {
		_ = t.themeWatcher.Watch(currentTheme.Ref)
	}

	// Make sure to stop the progress bar and theme watcher when the app quits abruptly.
	go func() {
		<-ctx.Done()
		t.chatPage.Cleanup()
		t.editor.Cleanup()
		t.themeWatcher.Stop()
	}()

	return t
}

// Init initializes the application
func (a *appModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.dialog.Init(),
		a.chatPage.Init(),
		a.editor.Init(),
		a.editor.Focus(),
		a.application.SendFirstMessage(),
	}

	// Start theme subscription only once
	if !a.themeSubStarted {
		a.themeSubStarted = true
		cmds = append(cmds, a.themeSubscription.Listen())
	}

	return tea.Sequence(cmds...)
}

// Help returns help information
func (a *appModel) Help() help.KeyMap {
	return core.NewSimpleHelp(a.Bindings())
}

func (a *appModel) Bindings() []key.Binding {
	bindings := []key.Binding{
		a.keyMap.Quit,
		a.keyMap.CommandPalette,
		a.keyMap.ModelPicker,
	}

	tabBinding := key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "switch focus"),
	)
	bindings = append(bindings, tabBinding)

	// Show newline help based on keyboard enhancement support
	if a.keyboardEnhancementsSupported {
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

	if a.focusedPanel == panelContent {
		bindings = append(bindings, a.chatPage.Bindings()...)
	} else {
		bindings = append(bindings, key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("Ctrl+g", fmt.Sprintf("edit in %s", editorDisplayName())),
		))
	}
	return bindings
}

// Update handles incoming messages and updates the application state
func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case animation.TickMsg:
		var cmds []tea.Cmd
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		cmds = append(cmds, cmd)
		if a.chatPage.IsWorking() {
			var model layout.Model
			model, cmd = a.workingSpinner.Update(msg)
			a.workingSpinner = model.(spinner.Spinner)
			cmds = append(cmds, cmd)
		}
		if animation.HasActive() {
			cmds = append(cmds, animation.StartTick())
		}
		return a, tea.Batch(cmds...)

	case dialog.OpenDialogMsg, dialog.CloseDialogMsg:
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd

	case *runtime.TeamInfoEvent:
		a.sessionState.SetAvailableAgents(msg.AvailableAgents)
		a.sessionState.SetCurrentAgentName(msg.CurrentAgent)
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case *runtime.AgentInfoEvent:
		a.sessionState.SetCurrentAgentName(msg.AgentName)
		a.application.TrackCurrentAgentModel(msg.Model)
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case *runtime.SessionTitleEvent:
		a.sessionState.SetSessionTitle(msg.Title)
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.SwitchAgentMsg:
		return a.handleSwitchAgent(msg.AgentName)

	case tea.WindowSizeMsg:
		a.wWidth, a.wHeight = msg.Width, msg.Height
		cmd := a.handleWindowResize(msg.Width, msg.Height)
		return a, cmd

	case tea.KeyboardEnhancementsMsg:
		a.keyboardEnhancements = &msg
		a.keyboardEnhancementsSupported = msg.Flags != 0
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		editorModel, editorCmd := a.editor.Update(msg)
		a.editor = editorModel.(editor.Editor)
		return a, tea.Batch(cmd, editorCmd)

	case notification.ShowMsg, notification.HideMsg:
		updated, cmd := a.notification.Update(msg)
		a.notification = updated
		return a, cmd

	case messages.InvalidateStatusBarMsg:
		// Invalidate statusbar cache when bindings change (e.g., inline edit mode)
		a.statusBar.InvalidateCache()
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKeyPressMsg(msg)

	case tea.PasteMsg:
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}
		editorModel, cmd := a.editor.Update(msg)
		a.editor = editorModel.(editor.Editor)
		return a, cmd

	case tea.MouseWheelMsg:
		cmd := a.handleWheelMsg(msg)
		return a, cmd

	case messages.WheelCoalescedMsg:
		if msg.Delta == 0 {
			return a, nil
		}
		if a.dialog.Open() {
			cmd := a.handleDialogWheelDelta(msg)
			return a, cmd
		}
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case tea.MouseClickMsg:
		return a.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return a.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return a.handleMouseRelease(msg)

	// --- Focus requests from content view ---
	case messages.RequestFocusMsg:
		switch msg.Target {
		case messages.PanelMessages:
			if a.focusedPanel != panelContent {
				a.focusedPanel = panelContent
				a.editor.Blur()
				return a, a.chatPage.FocusMessages()
			}
		case messages.PanelEditor:
			if a.focusedPanel != panelEditor {
				a.focusedPanel = panelEditor
				a.chatPage.BlurMessages()
				return a, a.editor.Focus()
			}
		}
		return a, nil

	// --- Working state from content view ---
	case messages.WorkingStateChangedMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, a.editor.SetWorking(msg.Working))
		if msg.Working {
			cmds = append(cmds, a.workingSpinner.Init())
		} else {
			a.workingSpinner.Stop()
		}
		return a, tea.Batch(cmds...)

	// --- SendMsg from editor ---
	case messages.SendMsg:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	// --- File attachments (routed to editor) ---
	case messages.InsertFileRefMsg:
		a.editor.AttachFile(msg.FilePath)
		return a, nil

	case messages.ExitSessionMsg:
		a.chatPage.Cleanup()
		a.editor.Cleanup()
		return a, tea.Quit

	case messages.NewSessionMsg:
		return a.handleNewSession()

	case messages.OpenSessionBrowserMsg:
		return a.handleOpenSessionBrowser()

	case messages.LoadSessionMsg:
		return a.handleLoadSession(msg.SessionID)

	case messages.BranchFromEditMsg:
		return a.handleBranchFromEdit(msg)

	case messages.ToggleSessionStarMsg:
		sessionID := msg.SessionID
		if sessionID == "" {
			if sess := a.application.Session(); sess != nil {
				sessionID = sess.ID
			} else {
				return a, nil
			}
		}
		return a.handleToggleSessionStar(sessionID)

	case messages.SetSessionTitleMsg:
		return a.handleSetSessionTitle(msg.Title)

	case messages.RegenerateTitleMsg:
		return a.handleRegenerateTitle()

	case messages.StartShellMsg:
		return a.startShell()

	case messages.EvalSessionMsg:
		return a.handleEvalSession(msg.Filename)

	case messages.ExportSessionMsg:
		return a.handleExportSession(msg.Filename)

	case messages.CompactSessionMsg:
		return a.handleCompactSession(msg.AdditionalPrompt)

	case messages.CopySessionToClipboardMsg:
		return a.handleCopySessionToClipboard()

	case messages.CopyLastResponseToClipboardMsg:
		return a.handleCopyLastResponseToClipboard()

	case messages.ToggleYoloMsg:
		return a.handleToggleYolo()

	case messages.ToggleThinkingMsg:
		return a.handleToggleThinking()

	case messages.ToggleHideToolResultsMsg:
		return a.handleToggleHideToolResults()

	case messages.ToggleSplitDiffMsg:
		updated, cmd := a.chatPage.Update(editfile.ToggleDiffViewMsg{})
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.ClearQueueMsg:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.ShowCostDialogMsg:
		return a.handleShowCostDialog()

	case messages.ShowPermissionsDialogMsg:
		return a.handleShowPermissionsDialog()

	case messages.AgentCommandMsg:
		return a.handleAgentCommand(msg.Command)

	case messages.ShowMCPPromptInputMsg:
		return a.handleShowMCPPromptInput(msg.PromptName, msg.PromptInfo)

	case messages.MCPPromptMsg:
		return a.handleMCPPrompt(msg.PromptName, msg.Arguments)

	case messages.OpenURLMsg:
		return a.handleOpenURL(msg.URL)

	case messages.AttachFileMsg:
		return a.handleAttachFile(msg.FilePath)

	case messages.StartSpeakMsg:
		if !a.transcriber.IsSupported() {
			return a, notification.InfoCmd("Speech-to-text is only supported on macOS")
		}
		return a.handleStartSpeak()

	case messages.StopSpeakMsg:
		return a.handleStopSpeak()

	case messages.SpeakTranscriptMsg:
		return a.handleSpeakTranscript(msg.Delta)

	case messages.OpenModelPickerMsg:
		return a.handleOpenModelPicker()

	case messages.ChangeModelMsg:
		return a.handleChangeModel(msg.ModelRef)

	case messages.OpenThemePickerMsg:
		return a.handleOpenThemePicker()

	case messages.ChangeThemeMsg:
		return a.handleChangeTheme(msg.ThemeRef)

	case messages.ThemePreviewMsg:
		return a.handleThemePreview(msg.ThemeRef)

	case messages.ThemeCancelPreviewMsg:
		return a.handleThemeCancelPreview(msg.OriginalRef)

	case messages.ThemeChangedMsg:
		return a.applyThemeChanged()

	case messages.ThemeFileChangedMsg:
		theme, err := styles.LoadTheme(msg.ThemeRef)
		if err != nil {
			return a, tea.Batch(
				a.themeSubscription.Listen(),
				notification.ErrorCmd(fmt.Sprintf("Failed to hot-reload theme: %v", err)),
			)
		}
		styles.ApplyTheme(theme)
		return a, tea.Batch(
			a.themeSubscription.Listen(),
			notification.SuccessCmd("Theme hot-reloaded"),
			core.CmdHandler(messages.ThemeChangedMsg{}),
		)

	case messages.ElicitationResponseMsg:
		return a.handleElicitationResponse(msg.Action, msg.Content)

	case messages.SendAttachmentMsg:
		a.application.RunWithMessage(context.Background(), nil, msg.Content)
		return a, nil

	case speakTranscriptAndContinue:
		a.editor.InsertText(msg.delta)
		cmd := a.listenForTranscripts(msg.ch)
		return a, cmd

	case dialog.MultiChoiceResultMsg:
		if msg.DialogID == dialog.ToolRejectionDialogID {
			if msg.Result.IsCancelled {
				return a, nil
			}
			resumeMsg := dialog.HandleToolRejectionResult(msg.Result)
			if resumeMsg != nil {
				return a, tea.Sequence(
					core.CmdHandler(dialog.CloseDialogMsg{}),
					core.CmdHandler(*resumeMsg),
				)
			}
		}
		return a, nil

	case dialog.RuntimeResumeMsg:
		a.application.Resume(msg.Request)
		return a, nil

	case dialog.ExitConfirmedMsg:
		a.chatPage.Cleanup()
		a.editor.Cleanup()
		return a, tea.Quit

	case messages.ExitAfterFirstResponseMsg:
		a.chatPage.Cleanup()
		a.editor.Cleanup()
		return a, tea.Quit

	case error:
		a.err = msg
		return a, nil

	default:
		if event, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			if agentName := event.GetAgentName(); agentName != "" {
				a.sessionState.SetCurrentAgentName(agentName)
			}
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			return a, cmd
		}

		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)

			updated, cmdChatPage := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)

			return a, tea.Batch(dialogCmd, cmdChatPage)
		}

		updatedComp, cmdCompletions := a.completions.Update(msg)
		a.completions = updatedComp.(completion.Manager)

		editorModel, cmdEditor := a.editor.Update(msg)
		a.editor = editorModel.(editor.Editor)

		updated, cmdChatPage := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)

		return a, tea.Batch(cmdCompletions, cmdEditor, cmdChatPage)
	}
}

// handleWindowResize processes window resize events
func (a *appModel) handleWindowResize(width, height int) tea.Cmd {
	a.width, a.height = width, height

	a.statusBar.SetWidth(width)

	if !a.ready {
		a.ready = true
	}

	return a.resizeAll()
}

// resizeAll recalculates all component sizes.
func (a *appModel) resizeAll() tea.Cmd {
	var cmds []tea.Cmd

	width, height := a.width, a.height

	// Calculate fixed heights
	statusBarHeight := 1
	if statusBarView := a.statusBar.View(); statusBarView != "" {
		statusBarHeight = lipgloss.Height(statusBarView)
	}
	resizeHandleHeight := 1

	// Calculate editor height
	innerWidth := width - appPaddingHorizontal
	minLines := 4
	maxLines := max(minLines, (height-6)/2)
	a.editorLines = max(minLines, min(a.editorLines, maxLines))

	targetEditorHeight := a.editorLines - 1
	cmds = append(cmds, a.editor.SetSize(innerWidth, targetEditorHeight))
	_, editorHeight := a.editor.GetSize()
	// The editor's View() adds MarginBottom(1) which isn't included in GetSize(),
	// so account for it in the layout calculation.
	editorRenderedHeight := editorHeight + 1

	// Content gets remaining space
	a.contentHeight = max(1, height-statusBarHeight-resizeHandleHeight-editorRenderedHeight)

	// Update dialog system
	u, cmd := a.dialog.Update(tea.WindowSizeMsg{Width: width, Height: height})
	a.dialog = u.(dialog.Manager)
	cmds = append(cmds, cmd)

	// Update chat page (content area)
	cmd = a.chatPage.SetSize(width, a.contentHeight)
	cmds = append(cmds, cmd)

	// Update completion manager
	a.completions.SetEditorBottom(editorHeight)
	a.completions.Update(tea.WindowSizeMsg{Width: width, Height: height})

	// Update notification size
	a.notification.SetSize(width, height)

	return tea.Batch(cmds...)
}

// layoutRegion represents a vertical region in the TUI layout.
type layoutRegion int

const (
	regionContent layoutRegion = iota
	regionResizeHandle
	regionEditor
	regionStatusBar
)

// hitTestRegion determines which layout region a Y coordinate falls in.
func (a *appModel) hitTestRegion(y int) layoutRegion {
	resizeHandleTop := a.contentHeight
	editorTop := resizeHandleTop + 1

	switch {
	case y < resizeHandleTop:
		return regionContent
	case y < editorTop:
		return regionResizeHandle
	default:
		_, editorHeight := a.editor.GetSize()
		if y < editorTop+editorHeight {
			return regionEditor
		}
		return regionStatusBar
	}
}

// editorTop returns the Y coordinate where the editor starts.
func (a *appModel) editorTop() int {
	return a.contentHeight + 1
}

// handleEditorResize adjusts editor height based on drag position.
func (a *appModel) handleEditorResize(y int) tea.Cmd {
	editorPadding := styles.EditorStyle.GetVerticalFrameSize()
	targetLines := a.height - y - 1 - editorPadding
	minLines := 4
	maxLines := max(minLines, (a.height-6)/2)
	newLines := max(minLines, min(targetLines, maxLines))
	if newLines != a.editorLines {
		a.editorLines = newLines
		return a.resizeAll()
	}
	return nil
}

func (a *appModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd
	}

	region := a.hitTestRegion(msg.Y)

	switch region {
	case regionContent:
		if a.focusedPanel != panelContent {
			a.focusedPanel = panelContent
			a.editor.Blur()
			a.chatPage.FocusMessages()
		}
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case regionResizeHandle:
		if msg.Button == tea.MouseLeft {
			a.isDragging = true
		}
		return a, nil

	case regionEditor:
		if a.focusedPanel != panelEditor {
			a.focusedPanel = panelEditor
			a.chatPage.BlurMessages()
		}
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - a.editorTop()
		editorModel, cmd := a.editor.Update(adjustedMsg)
		a.editor = editorModel.(editor.Editor)
		return a, tea.Batch(cmd, a.editor.Focus())
	}

	return a, nil
}

func (a *appModel) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if a.isDragging {
		cmd := a.handleEditorResize(msg.Y)
		return a, cmd
	}

	if a.dialog.Open() {
		u, cmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, cmd
	}

	region := a.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd
	case regionEditor:
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - a.editorTop()
		editorModel, cmd := a.editor.Update(adjustedMsg)
		a.editor = editorModel.(editor.Editor)
		return a, cmd
	}

	return a, nil
}

func (a *appModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if a.isDragging {
		a.isDragging = false
		return a, nil
	}

	if a.dialog.Open() {
		u, cmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, cmd
	}

	region := a.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd
	case regionEditor:
		adjustedMsg := msg
		adjustedMsg.Y = msg.Y - a.editorTop()
		editorModel, cmd := a.editor.Update(adjustedMsg)
		a.editor = editorModel.(editor.Editor)
		return a, cmd
	}

	return a, nil
}

func (a *appModel) handleKeyPressMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Check if we should stop transcription on Enter or Escape
	if a.transcriber.IsRunning() {
		switch msg.String() {
		case "enter":
			model, cmd := a.handleStopSpeak()
			sendCmd := a.editor.SendContent()
			return model, tea.Batch(cmd, sendCmd)
		case "esc":
			return a.handleStopSpeak()
		}
	}

	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd
	}

	if a.completions.Open() {
		if core.IsNavigationKey(msg) {
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			return a, completionCmd
		}

		var cmds []tea.Cmd
		u, completionCmd := a.completions.Update(msg)
		a.completions = u.(completion.Manager)
		cmds = append(cmds, completionCmd)

		editorModel, cmd := a.editor.Update(msg)
		a.editor = editorModel.(editor.Editor)
		cmds = append(cmds, cmd)

		return a, tea.Batch(cmds...)
	}

	switch {
	case key.Matches(msg, a.keyMap.Quit):
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewExitConfirmationDialog(),
		})

	case key.Matches(msg, a.keyMap.Suspend):
		return a, tea.Suspend

	case key.Matches(msg, a.keyMap.CommandPalette):
		categories := commands.BuildCommandCategories(context.Background(), a.application)
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewCommandPaletteDialog(categories),
		})

	case key.Matches(msg, a.keyMap.ToggleYolo):
		return a, core.CmdHandler(messages.ToggleYoloMsg{})

	case key.Matches(msg, a.keyMap.ToggleHideToolResults):
		return a, core.CmdHandler(messages.ToggleHideToolResultsMsg{})

	case key.Matches(msg, a.keyMap.CycleAgent):
		return a.handleCycleAgent()

	case key.Matches(msg, a.keyMap.ModelPicker):
		return a.handleOpenModelPicker()

	case key.Matches(msg, a.keyMap.Speak):
		if a.transcriber.IsSupported() {
			return a.handleStartSpeak()
		}
		return a, notification.InfoCmd("Speech-to-text is only supported on macOS")

	case key.Matches(msg, a.keyMap.ClearQueue):
		return a, core.CmdHandler(messages.ClearQueueMsg{})

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+g"))):
		return a.openExternalEditor()

	// Toggle sidebar (propagates to content view regardless of focus)
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	// Focus switching
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		return a.switchFocus()

	// Esc: cancel stream
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	default:
		// Handle ctrl+1 through ctrl+9 for quick agent switching
		if index := parseCtrlNumberKey(msg); index >= 0 {
			return a.handleSwitchToAgentByIndex(index)
		}

		// Focus-based routing
		switch a.focusedPanel {
		case panelEditor:
			editorModel, cmd := a.editor.Update(msg)
			a.editor = editorModel.(editor.Editor)
			return a, cmd
		case panelContent:
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			return a, cmd
		}

		return a, nil
	}
}

// switchFocus toggles between content and editor panels.
func (a *appModel) switchFocus() (tea.Model, tea.Cmd) {
	switch a.focusedPanel {
	case panelEditor:
		if cmd := a.editor.AcceptSuggestion(); cmd != nil {
			return a, cmd
		}
		a.focusedPanel = panelContent
		a.editor.Blur()
		return a, a.chatPage.FocusMessages()
	case panelContent:
		a.focusedPanel = panelEditor
		a.chatPage.BlurMessages()
		return a, a.editor.Focus()
	}
	return a, nil
}

// parseCtrlNumberKey checks if msg is ctrl+1 through ctrl+9 and returns the index (0-8), or -1 if not matched
func parseCtrlNumberKey(msg tea.KeyPressMsg) int {
	s := msg.String()
	if len(s) == 6 && s[:5] == "ctrl+" && s[5] >= '1' && s[5] <= '9' {
		return int(s[5] - '1')
	}
	return -1
}

func (a *appModel) handleWheelMsg(msg tea.MouseWheelMsg) tea.Cmd {
	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return dialogCmd
	}

	region := a.hitTestRegion(msg.Y)
	switch region {
	case regionContent:
		updated, chatCmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return chatCmd
	case regionEditor:
		if msg.Button == tea.MouseWheelUp {
			a.editor.ScrollByWheel(-1)
		} else {
			a.editor.ScrollByWheel(1)
		}
		return nil
	}

	return nil
}

func (a *appModel) handleDialogWheelDelta(msg messages.WheelCoalescedMsg) tea.Cmd {
	steps := msg.Delta
	button := tea.MouseWheelDown
	if steps < 0 {
		steps = -steps
		button = tea.MouseWheelUp
	}

	var cmds []tea.Cmd
	for range steps {
		u, dialogCmd := a.dialog.Update(tea.MouseWheelMsg{X: msg.X, Y: msg.Y, Button: button})
		a.dialog = u.(dialog.Manager)
		if dialogCmd != nil {
			cmds = append(cmds, dialogCmd)
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// renderResizeHandle renders the separator between content and editor.
func (a *appModel) renderResizeHandle(width int) string {
	if width <= 0 {
		return ""
	}

	innerWidth := width - appPaddingHorizontal

	centerStyle := styles.ResizeHandleHoverStyle
	if a.isDragging {
		centerStyle = styles.ResizeHandleActiveStyle
	}

	centerPart := strings.Repeat("─", min(resizeHandleWidth, innerWidth))
	handle := centerStyle.Render(centerPart)

	fullLine := lipgloss.PlaceHorizontal(
		max(0, innerWidth), lipgloss.Center, handle,
		lipgloss.WithWhitespaceChars("─"),
		lipgloss.WithWhitespaceStyle(styles.ResizeHandleStyle),
	)

	if a.chatPage.IsWorking() {
		workingText := "Working…"
		if queueLen := a.chatPage.QueueLength(); queueLen > 0 {
			workingText = fmt.Sprintf("Working… (%d queued)", queueLen)
		}
		suffix := " " + a.workingSpinner.View() + " " + styles.SpinnerDotsHighlightStyle.Render(workingText)
		cancelKeyPart := styles.HighlightWhiteStyle.Render("Esc")
		suffix += " (" + cancelKeyPart + " to interrupt)"
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(innerWidth - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	if queueLen := a.chatPage.QueueLength(); queueLen > 0 {
		queueText := fmt.Sprintf("%d queued", queueLen)
		suffix := " " + styles.WarningStyle.Render(queueText) + " "
		suffixWidth := lipgloss.Width(suffix)
		truncated := lipgloss.NewStyle().MaxWidth(innerWidth - suffixWidth).Render(fullLine)
		return truncated + suffix
	}

	return fullLine
}

// View renders the complete application interface
func (a *appModel) View() tea.View {
	windowTitle := a.windowTitle()

	if a.err != nil {
		return toFullscreenView(styles.ErrorStyle.Render(a.err.Error()), windowTitle)
	}

	if !a.ready {
		return toFullscreenView(
			styles.CenterStyle.
				Width(a.wWidth).
				Height(a.wHeight).
				Render(styles.MutedStyle.Render("Loading…")),
			windowTitle,
		)
	}

	// Content area (messages + sidebar)
	contentView := a.chatPage.View()

	// Resize handle
	resizeHandle := a.renderResizeHandle(a.width)

	// Editor
	editorView := a.editor.View()

	// Status bar
	statusBar := a.statusBar.View()

	// Combine: content | resize handle | editor | status bar
	var components []string
	components = append(components, contentView, resizeHandle, editorView)
	if statusBar != "" {
		components = append(components, statusBar)
	}

	baseView := lipgloss.JoinVertical(lipgloss.Top, components...)

	hasOverlays := a.dialog.Open() || a.notification.Open() || a.completions.Open()

	if hasOverlays {
		baseLayer := lipgloss.NewLayer(baseView)
		var allLayers []*lipgloss.Layer
		allLayers = append(allLayers, baseLayer)

		if a.dialog.Open() {
			dialogLayers := a.dialog.GetLayers()
			allLayers = append(allLayers, dialogLayers...)
		}

		if a.notification.Open() {
			allLayers = append(allLayers, a.notification.GetLayer())
		}

		if a.completions.Open() {
			layers := a.completions.GetLayers()
			allLayers = append(allLayers, layers...)
		}

		canvas := lipgloss.NewCanvas(allLayers...)
		return toFullscreenView(canvas.Render(), windowTitle)
	}

	return toFullscreenView(baseView, windowTitle)
}

func (a *appModel) windowTitle() string {
	if sessionTitle := a.sessionState.SessionTitle(); sessionTitle != "" {
		return sessionTitle + " - cagent"
	}
	return "cagent"
}

func (a *appModel) startShell() (tea.Model, tea.Cmd) {
	var cmd *exec.Cmd

	if goruntime.GOOS == "windows" {
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			cmd = exec.Command(path, "-NoLogo", "-NoExit", "-Command",
				`Write-Host ""; Write-Host "Type 'exit' to return to cagent 🐳"`)
		} else if path, err := exec.LookPath("powershell.exe"); err == nil {
			cmd = exec.Command(path, "-NoLogo", "-NoExit", "-Command",
				`Write-Host ""; Write-Host "Type 'exit' to return to cagent 🐳"`)
		} else {
			shell := cmp.Or(os.Getenv("ComSpec"), "cmd.exe")
			cmd = exec.Command(shell, "/K", `echo. & echo Type 'exit' to return to cagent`)
		}
	} else {
		shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
		cmd = exec.Command(shell, "-i", "-c",
			`echo -e "\nType 'exit' to return to cagent 🐳"; exec `+shell)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return a, tea.ExecProcess(cmd, nil)
}

func (a *appModel) invalidateCachesForThemeChange() {
	markdown.ResetStyles()
	a.statusBar.InvalidateCache()
}

func (a *appModel) applyThemeChanged() (tea.Model, tea.Cmd) {
	a.invalidateCachesForThemeChange()

	currentTheme := styles.CurrentTheme()
	if currentTheme != nil {
		_ = a.themeWatcher.Watch(currentTheme.Ref)
	}

	var cmds []tea.Cmd

	dialogUpdated, dialogCmd := a.dialog.Update(messages.ThemeChangedMsg{})
	a.dialog = dialogUpdated.(dialog.Manager)
	cmds = append(cmds, dialogCmd)

	chatUpdated, chatCmd := a.chatPage.Update(messages.ThemeChangedMsg{})
	a.chatPage = chatUpdated.(chat.Page)
	cmds = append(cmds, chatCmd)

	editorModel, editorCmd := a.editor.Update(messages.ThemeChangedMsg{})
	a.editor = editorModel.(editor.Editor)
	cmds = append(cmds, editorCmd)

	// Recreate working spinner with new theme colors
	a.workingSpinner = spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle)

	return a, tea.Batch(cmds...)
}

// openExternalEditor opens the current editor content in an external editor.
func (a *appModel) openExternalEditor() (tea.Model, tea.Cmd) {
	content := a.editor.Value()

	tmpFile, err := os.CreateTemp("", "cagent-*.md")
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to create temp file: %v", err))
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to write temp file: %v", err))
	}
	tmpFile.Close()

	editorCmd := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editorCmd == "" {
		if goruntime.GOOS == "windows" {
			editorCmd = "notepad"
		} else {
			editorCmd = "vi"
		}
	}

	parts := strings.Fields(editorCmd)
	args := append(parts[1:], tmpPath)
	cmd := exec.Command(parts[0], args...)

	ed := a.editor
	return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			os.Remove(tmpPath)
			return notification.ShowMsg{Text: fmt.Sprintf("Editor error: %v", err), Type: notification.TypeError}
		}

		updatedContent, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)

		if readErr != nil {
			return notification.ShowMsg{Text: fmt.Sprintf("Failed to read edited file: %v", readErr), Type: notification.TypeError}
		}

		c := strings.TrimSuffix(string(updatedContent), "\n")
		if strings.TrimSpace(c) == "" {
			ed.SetValue("")
		} else {
			ed.SetValue(c)
		}

		return nil
	})
}

func toFullscreenView(content, windowTitle string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background
	view.WindowTitle = windowTitle

	return view
}

// editorDisplayName returns a friendly display name for the configured external editor.
func editorDisplayName() string {
	editorCmd := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
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
	name := parts[0]
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	// Map common editor prefixes to friendly names
	for _, e := range []struct{ prefix, display string }{
		{"code", "VSCode"},
		{"cursor", "Cursor"},
		{"nvim", "Neovim"},
		{"vim", "Vim"},
		{"vi", "Vi"},
		{"nano", "Nano"},
		{"emacs", "Emacs"},
		{"subl", "Sublime Text"},
		{"zed", "Zed"},
	} {
		if strings.HasPrefix(name, e.prefix) {
			return e.display
		}
	}
	if name != "" {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return "$EDITOR"
}
