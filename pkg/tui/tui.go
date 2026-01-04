package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/browser"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

// appModel represents the main application model
type appModel struct {
	application     *app.App
	wWidth, wHeight int // Window dimensions
	width, height   int
	keyMap          KeyMap

	chatPage  chat.Page
	statusBar statusbar.StatusBar

	notification notification.Manager
	dialog       dialog.Manager
	completions  completion.Manager

	// Session state
	sessionState *service.SessionState

	// Agent state
	availableAgents []runtime.AgentDetails
	currentAgent    string

	// State
	ready bool
	err   error
}

// KeyMap defines global key bindings
type KeyMap struct {
	Quit                  key.Binding
	CommandPalette        key.Binding
	ToggleYolo            key.Binding
	ToggleHideToolResults key.Binding
	SwitchAgent           key.Binding
}

// DefaultKeyMap returns the default global key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("Ctrl+c", "quit"),
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
		SwitchAgent: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("Ctrl+s", "cycle agent"),
		),
	}
}

// New creates and initializes a new TUI application model
func New(ctx context.Context, a *app.App) tea.Model {
	sessionState := service.NewSessionState(a.Session())

	t := &appModel{
		keyMap:       DefaultKeyMap(),
		dialog:       dialog.New(),
		notification: notification.New(),
		completions:  completion.New(),
		application:  a,
		sessionState: sessionState,
	}

	t.statusBar = statusbar.New(t)
	t.chatPage = chat.New(a, sessionState)

	// Make sure to stop the progress bar when the app quits abruptly.
	go func() {
		<-ctx.Done()
		t.chatPage.Cleanup()
	}()

	return t
}

// Init initializes the application
func (a *appModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.dialog.Init(),
		a.chatPage.Init(),
	}

	if firstMessage := a.application.FirstMessage(); firstMessage != nil {
		cmds = append(cmds, func() tea.Msg {
			// Use the shared PrepareUserMessage function for consistent attachment handling
			userMsg := cli.PrepareUserMessage(context.Background(), a.application.Runtime(), *firstMessage, a.application.FirstMessageAttachment())

			// If the message has multi-content (attachments), we need to handle it specially
			if len(userMsg.Message.MultiContent) > 0 {
				return firstMessageWithAttachment{
					message: userMsg,
				}
			}

			return editor.SendMsg{
				Content: userMsg.Message.Content,
			}
		})
	}

	return tea.Batch(cmds...)
}

// firstMessageWithAttachment is a message for the first message with an attachment
type firstMessageWithAttachment struct {
	message *session.Message
}

// Help returns help information
func (a *appModel) Help() help.KeyMap {
	return core.NewSimpleHelp(a.Bindings())
}

func (a *appModel) Bindings() []key.Binding {
	return append([]key.Binding{
		a.keyMap.Quit,
		a.keyMap.CommandPalette,
	}, a.chatPage.Bindings()...)
}

// Update handles incoming messages and updates the application state
func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// Handle dialog-specific messages first
	case dialog.OpenDialogMsg, dialog.CloseDialogMsg:
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd

	case *runtime.TeamInfoEvent:
		// Store team info for agent switching shortcuts
		a.availableAgents = msg.AvailableAgents
		a.currentAgent = msg.CurrentAgent
		a.sessionState.SetCurrentAgent(msg.CurrentAgent)
		// Forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case *runtime.AgentInfoEvent:
		// Track current agent
		a.currentAgent = msg.AgentName
		a.sessionState.SetCurrentAgent(msg.AgentName)
		// Forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.SwitchAgentMsg:
		// Switch the agent in the runtime
		if err := a.application.SwitchAgent(msg.AgentName); err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to switch to agent '%s': %v", msg.AgentName, err))
		}
		// Update local tracking
		a.currentAgent = msg.AgentName
		a.sessionState.SetCurrentAgent(msg.AgentName)
		return a, notification.SuccessCmd(fmt.Sprintf("Switched to agent '%s'", msg.AgentName))

	case tea.WindowSizeMsg:
		a.wWidth, a.wHeight = msg.Width, msg.Height
		cmd := a.handleWindowResize(msg.Width, msg.Height)
		a.completions.Update(msg)
		return a, cmd

	case tea.KeyboardEnhancementsMsg:
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case notification.ShowMsg, notification.HideMsg:
		updated, cmd := a.notification.Update(msg)
		a.notification = updated
		return a, cmd

	case tea.KeyPressMsg:
		return a.handleKeyPressMsg(msg)

	case tea.MouseWheelMsg:
		// If dialogs are active, they get priority for mouse events
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}
		// Otherwise forward to chat page
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.ExitSessionMsg:
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewExitConfirmationDialog(),
		})

	case messages.NewSessionMsg:
		a.application.NewSession()
		sess := a.application.Session()
		a.sessionState = service.NewSessionState(sess)
		a.chatPage = chat.New(a.application, a.sessionState)
		a.dialog = dialog.New()
		a.statusBar = statusbar.New(a.chatPage)

		return a, tea.Batch(a.Init(), a.handleWindowResize(a.wWidth, a.wHeight))

	case messages.OpenSessionBrowserMsg:
		store := a.application.SessionStore()
		if store == nil {
			return a, notification.InfoCmd("No session store configured")
		}
		sessions, err := store.GetSessions(context.Background())
		if err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to load sessions: %v", err))
		}
		if len(sessions) == 0 {
			return a, notification.InfoCmd("No previous sessions found")
		}
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewSessionBrowserDialog(sessions),
		})

	case messages.LoadSessionMsg:
		store := a.application.SessionStore()
		if store == nil {
			return a, notification.ErrorCmd("No session store configured")
		}
		sess, err := store.GetSession(context.Background(), msg.SessionID)
		if err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
		}
		// Cancel current session and replace with loaded one
		a.application.ReplaceSession(context.Background(), sess)
		a.sessionState = service.NewSessionState(sess)
		a.chatPage = chat.New(a.application, a.sessionState)
		a.dialog = dialog.New()
		a.statusBar = statusbar.New(a.chatPage)

		return a, tea.Batch(a.Init(), a.handleWindowResize(a.wWidth, a.wHeight))

	case messages.StartShellMsg:
		return a.startShell()

	case messages.EvalSessionMsg:
		evalFile, _ := evaluation.Save(a.application.Session(), msg.Filename)
		return a, notification.SuccessCmd(fmt.Sprintf("Eval saved to file %s", evalFile))

	case messages.CompactSessionMsg:
		return a, a.chatPage.CompactSession(msg.AdditionalPrompt)

	case messages.CopySessionToClipboardMsg:
		transcript := a.application.PlainTextTranscript()
		if transcript == "" {
			return a, notification.SuccessCmd("Conversation is empty; nothing copied.")
		}

		return a, tea.Sequence(
			tea.SetClipboard(transcript),
			func() tea.Msg {
				_ = clipboard.WriteAll(transcript)
				return nil
			},
			notification.SuccessCmd("Conversation copied to clipboard."),
		)

	case messages.ToggleYoloMsg:
		sess := a.application.Session()
		sess.ToolsApproved = !sess.ToolsApproved
		a.sessionState.SetYoloMode(sess.ToolsApproved)
		return a, nil

	case messages.ToggleHideToolResultsMsg:
		// Forward to chat page to invalidate message cache and trigger redraw
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd

	case messages.AgentCommandMsg:
		resolvedCommand := a.application.ResolveCommand(context.Background(), msg.Command)
		return a, core.CmdHandler(editor.SendMsg{Content: resolvedCommand})

	case messages.ShowMCPPromptInputMsg:
		// Convert the interface{} back to mcptools.PromptInfo
		promptInfo, ok := msg.PromptInfo.(mcptools.PromptInfo)
		if !ok {
			return a, notification.ErrorCmd("Invalid prompt info")
		}
		// Show the MCP prompt input dialog
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewMCPPromptInputDialog(msg.PromptName, promptInfo),
		})

	case messages.MCPPromptMsg:
		// Execute MCP prompt and send the result as editor content
		promptContent, err := a.application.ExecuteMCPPrompt(context.Background(), msg.PromptName, msg.Arguments)
		if err != nil {
			errorMsg := fmt.Sprintf("Error executing MCP prompt '%s': %v", msg.PromptName, err)
			return a, notification.ErrorCmd(errorMsg)
		}
		return a, core.CmdHandler(editor.SendMsg{Content: promptContent})

	case messages.OpenURLMsg:
		_ = browser.Open(context.Background(), msg.URL)
		return a, nil

	case dialog.RuntimeResumeMsg:
		a.application.Resume(msg.Response)
		return a, nil

	case dialog.ExitConfirmedMsg:
		a.chatPage.Cleanup()
		return a, tea.Quit

	case chat.EditorHeightChangedMsg:
		a.completions.SetEditorBottom(msg.Height)
		return a, nil

	case firstMessageWithAttachment:
		// Handle first message with image attachment using the pre-prepared message
		a.application.RunWithMessage(context.Background(), nil, msg.message)
		return a, nil

	case error:
		a.err = msg
		return a, nil

	default:
		if _, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			// Always forward runtime events to chat page
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			return a, cmd
		}

		// Spinner tick messages should always be forwarded to chat page
		// to keep animations running even when dialogs are open
		if spinner.IsTickMsg(msg) {
			var cmds []tea.Cmd
			updated, cmd := a.chatPage.Update(msg)
			a.chatPage = updated.(chat.Page)
			cmds = append(cmds, cmd)

			// Also forward to dialog if open (dialogs may have spinners too)
			if a.dialog.Open() {
				u, dialogCmd := a.dialog.Update(msg)
				a.dialog = u.(dialog.Manager)
				cmds = append(cmds, dialogCmd)
			}
			return a, tea.Batch(cmds...)
		}

		// For other messages, check if dialogs should handle them first
		// If dialogs are active, they get priority for input
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}

		var cmds []tea.Cmd
		var cmd tea.Cmd

		updated, cmd := a.completions.Update(msg)
		cmds = append(cmds, cmd)
		a.completions = updated.(completion.Manager)

		updated, cmd = a.chatPage.Update(msg)
		cmds = append(cmds, cmd)
		a.chatPage = updated.(chat.Page)

		return a, tea.Batch(cmds...)
	}
}

// handleWindowResize processes window resize events
func (a *appModel) handleWindowResize(width, height int) tea.Cmd {
	var cmds []tea.Cmd

	// Update dimensions
	a.width, a.height = width, height-1 // Account for status bar

	if !a.ready {
		a.ready = true
	}

	// Update dialog system
	u, cmd := a.dialog.Update(tea.WindowSizeMsg{Width: width, Height: height})
	a.dialog = u.(dialog.Manager)
	cmds = append(cmds, cmd)

	cmd = a.chatPage.SetSize(a.width, a.height)
	cmds = append(cmds, cmd)

	// Update completion manager with actual editor height for popup positioning
	a.completions.SetEditorBottom(a.chatPage.GetInputHeight())

	// Update status bar width
	a.statusBar.SetWidth(a.width)

	// Update notification size
	a.notification.SetSize(a.width, a.height)

	return tea.Batch(cmds...)
}

func (a *appModel) handleKeyPressMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd
	}

	if a.completions.Open() {
		// Check if this is a navigation key that the completion manager should handle
		if core.IsNavigationKey(msg) {
			// Let completion manager handle navigation keys
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			return a, completionCmd
		}

		// For all other keys (typing), send to both completion (for filtering) and editor
		var cmds []tea.Cmd
		u, completionCmd := a.completions.Update(msg)
		a.completions = u.(completion.Manager)
		cmds = append(cmds, completionCmd)

		// Also send to chat page/editor so user can continue typing
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		cmds = append(cmds, cmd)

		return a, tea.Batch(cmds...)
	}

	switch {
	case key.Matches(msg, a.keyMap.Quit):
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewExitConfirmationDialog(),
		})

	case key.Matches(msg, a.keyMap.CommandPalette):
		categories := commands.BuildCommandCategories(context.Background(), a.application)
		return a, core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewCommandPaletteDialog(categories),
		})

	case key.Matches(msg, a.keyMap.ToggleYolo):
		return a, core.CmdHandler(messages.ToggleYoloMsg{})

	case key.Matches(msg, a.keyMap.ToggleHideToolResults):
		return a, core.CmdHandler(messages.ToggleHideToolResultsMsg{})

	case key.Matches(msg, a.keyMap.SwitchAgent):
		// Cycle to the next agent in the list
		return a.cycleToNextAgent()

	default:
		// Handle ctrl+1 through ctrl+9 for quick agent switching
		if index := parseCtrlNumberKey(msg); index >= 0 {
			return a.switchToAgentByIndex(index)
		}
		updated, cmd := a.chatPage.Update(msg)
		a.chatPage = updated.(chat.Page)
		return a, cmd
	}
}

// parseCtrlNumberKey checks if msg is ctrl+1 through ctrl+9 and returns the index (0-8), or -1 if not matched
func parseCtrlNumberKey(msg tea.KeyPressMsg) int {
	s := msg.String()
	if len(s) == 6 && s[:5] == "ctrl+" && s[5] >= '1' && s[5] <= '9' {
		return int(s[5] - '1')
	}
	return -1
}

// switchToAgentByIndex switches to the agent at the given index
func (a *appModel) switchToAgentByIndex(index int) (tea.Model, tea.Cmd) {
	if index >= 0 && index < len(a.availableAgents) {
		agentName := a.availableAgents[index].Name
		if agentName != a.currentAgent {
			return a, core.CmdHandler(messages.SwitchAgentMsg{AgentName: agentName})
		}
	}
	return a, nil
}

// cycleToNextAgent cycles to the next agent in the available agents list
func (a *appModel) cycleToNextAgent() (tea.Model, tea.Cmd) {
	if len(a.availableAgents) <= 1 {
		return a, notification.InfoCmd("No other agents available")
	}

	// Find the current agent index
	currentIndex := -1
	for i, agent := range a.availableAgents {
		if agent.Name == a.currentAgent {
			currentIndex = i
			break
		}
	}

	// Cycle to the next agent (wrap around to 0 if at the end)
	nextIndex := (currentIndex + 1) % len(a.availableAgents)
	return a.switchToAgentByIndex(nextIndex)
}

// View renders the complete application interface
func (a *appModel) View() tea.View {
	// Show error if present
	if a.err != nil {
		return toFullscreenView(styles.ErrorStyle.Render(a.err.Error()))
	}

	// Show loading if not ready
	if !a.ready {
		return toFullscreenView(
			styles.CenterStyle.
				Width(a.wWidth).
				Height(a.wHeight).
				Render(styles.MutedStyle.Render("Loading…")),
		)
	}

	// Render chat page
	pageView := a.chatPage.View()

	// Create status bar
	statusBar := a.statusBar.View()

	// Combine page view with status bar
	var components []string
	components = append(components, pageView)
	if statusBar != "" {
		components = append(components, statusBar)
	}

	baseView := lipgloss.JoinVertical(lipgloss.Top, components...)

	hasOverlays := a.dialog.Open() || a.notification.Open() || a.completions.Open()

	if hasOverlays {
		baseLayer := lipgloss.NewLayer(baseView)
		var allLayers []*lipgloss.Layer
		allLayers = append(allLayers, baseLayer)

		// Add dialog layers
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
		return toFullscreenView(canvas.Render())
	}

	return toFullscreenView(baseView)
}

func (a *appModel) startShell() (tea.Model, tea.Cmd) {
	var cmd *exec.Cmd

	if goruntime.GOOS == "windows" {
		// Prefer PowerShell (pwsh or Windows PowerShell) when available, otherwise fall back to cmd.exe
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			cmd = exec.Command(path, "-NoLogo", "-NoExit", "-Command",
				`Write-Host ""; Write-Host "Type 'exit' to return to cagent 🐳"`)
		} else if path, err := exec.LookPath("powershell.exe"); err == nil {
			cmd = exec.Command(path, "-NoLogo", "-NoExit", "-Command",
				`Write-Host ""; Write-Host "Type 'exit' to return to cagent 🐳"`)
		} else {
			// Use ComSpec if available, otherwise default to cmd.exe
			shell := cmp.Or(os.Getenv("ComSpec"), "cmd.exe")
			cmd = exec.Command(shell, "/K", `echo. & echo Type 'exit' to return to cagent`)
		}
	} else {
		// Unix-like: use SHELL or default to /bin/sh
		shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
		cmd = exec.Command(shell, "-i", "-c",
			`echo -e "\nType 'exit' to return to cagent 🐳"; exec `+shell)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return a, tea.ExecProcess(cmd, nil)
}

func toFullscreenView(content string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background

	return view
}
