package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/browser"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

// Session management handlers

func (a *appModel) applyKeyboardEnhancements() {
	if a.keyboardEnhancements != nil {
		updated, _ := a.chatPage.Update(*a.keyboardEnhancements)
		a.chatPage = updated.(chat.Page)
	}
}

func (a *appModel) handleNewSession() (tea.Model, tea.Cmd) {
	// Theme is now global - no per-session theme reset needed
	a.application.NewSession()
	sess := a.application.Session()
	a.sessionState = service.NewSessionState(sess)
	a.chatPage = chat.New(a.application, a.sessionState)
	a.dialog = dialog.New()
	a.applyKeyboardEnhancements()

	return a, tea.Batch(
		a.Init(),
		a.handleWindowResize(a.wWidth, a.wHeight),
	)
}

func (a *appModel) handleOpenSessionBrowser() (tea.Model, tea.Cmd) {
	store := a.application.SessionStore()
	if store == nil {
		return a, notification.InfoCmd("No session store configured")
	}

	sessions, err := store.GetSessionSummaries(context.Background())
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to load sessions: %v", err))
	}
	if len(sessions) == 0 {
		return a, notification.InfoCmd("No previous sessions found")
	}

	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewSessionBrowserDialog(sessions),
	})
}

func (a *appModel) handleLoadSession(sessionID string) (tea.Model, tea.Cmd) {
	store := a.application.SessionStore()
	if store == nil {
		return a, notification.ErrorCmd("No session store configured")
	}

	sess, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
	}

	slog.Debug("Loaded session from store", "session_id", sessionID, "model_overrides", sess.AgentModelOverrides)

	// Theme is now global - no per-session theme switching needed

	// Cancel current session and replace with loaded one
	a.application.ReplaceSession(context.Background(), sess)
	a.sessionState = service.NewSessionState(sess)
	a.chatPage = chat.New(a.application, a.sessionState)
	a.dialog = dialog.New()
	a.applyKeyboardEnhancements()

	return a, tea.Batch(
		a.Init(),
		a.handleWindowResize(a.wWidth, a.wHeight),
	)
}

func (a *appModel) handleToggleSessionStar(sessionID string) (tea.Model, tea.Cmd) {
	store := a.application.SessionStore()
	if store == nil {
		return a, notification.ErrorCmd("No session store configured")
	}

	// Get current session
	currentSess := a.application.Session()

	// Determine the new starred status
	var newStarred bool
	if currentSess != nil && currentSess.ID == sessionID {
		// For current session, toggle from current state
		newStarred = !currentSess.Starred
		currentSess.Starred = newStarred
		a.chatPage.SetSessionStarred(newStarred)

		// Use UpdateSession (upsert) to ensure the session exists in DB before setting starred
		// This handles the case where the session hasn't been persisted yet
		if err := store.UpdateSession(context.Background(), currentSess); err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to save session: %v", err))
		}
	} else {
		// For non-current sessions (from session browser), fetch and toggle
		sess, err := store.GetSession(context.Background(), sessionID)
		if err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
		}
		newStarred = !sess.Starred

		// Persist the starred status to database
		if err := store.SetSessionStarred(context.Background(), sessionID, newStarred); err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to update session: %v", err))
		}
	}

	return a, nil
}

func (a *appModel) handleSetSessionTitle(title string) (tea.Model, tea.Cmd) {
	if err := a.application.UpdateSessionTitle(context.Background(), title); err != nil {
		if errors.Is(err, app.ErrTitleGenerating) {
			return a, notification.WarningCmd("Title is being generated, please wait")
		}
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to set session title: %v", err))
	}
	// Title will be updated via SessionTitleEvent emitted by UpdateSessionTitle
	return a, notification.SuccessCmd(fmt.Sprintf("Title set to: %s", title))
}

func (a *appModel) handleRegenerateTitle() (tea.Model, tea.Cmd) {
	sess := a.application.Session()
	if sess == nil {
		return a, notification.ErrorCmd("No active session")
	}

	if len(sess.GetLastUserMessages(1)) == 0 {
		return a, notification.ErrorCmd("Cannot regenerate title: no user message in session")
	}

	// Trigger regeneration - returns error if already in progress
	if err := a.application.RegenerateSessionTitle(context.Background()); err != nil {
		if errors.Is(err, app.ErrTitleGenerating) {
			return a, notification.WarningCmd("Title is being generated, please wait")
		}
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to regenerate title: %v", err))
	}

	// Show spinner while regenerating - the spinner will be cleared when SessionTitleEvent arrives
	spinnerCmd := a.chatPage.SetTitleRegenerating(true)

	return a, tea.Batch(spinnerCmd, notification.SuccessCmd("Regenerating title..."))
}

func (a *appModel) handleEvalSession(filename string) (tea.Model, tea.Cmd) {
	evalFile, _ := evaluation.Save(a.application.Session(), filename)
	return a, notification.SuccessCmd(fmt.Sprintf("Eval saved to file %s", evalFile))
}

func (a *appModel) handleExportSession(filename string) (tea.Model, tea.Cmd) {
	exportFile, err := a.application.ExportHTML(context.Background(), filename)
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to export session: %v", err))
	}
	return a, notification.SuccessCmd(fmt.Sprintf("Session exported to %s", exportFile))
}

func (a *appModel) handleCompactSession(additionalPrompt string) (tea.Model, tea.Cmd) {
	return a, a.chatPage.CompactSession(additionalPrompt)
}

func (a *appModel) handleCopySessionToClipboard() (tea.Model, tea.Cmd) {
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
}

func (a *appModel) handleCopyLastResponseToClipboard() (tea.Model, tea.Cmd) {
	sess := a.application.Session()
	if sess == nil {
		return a, notification.InfoCmd("No active session.")
	}

	lastResponse := sess.GetLastAssistantMessageContent()
	if lastResponse == "" {
		return a, notification.InfoCmd("No assistant response to copy.")
	}

	return a, tea.Sequence(
		tea.SetClipboard(lastResponse),
		func() tea.Msg {
			_ = clipboard.WriteAll(lastResponse)
			return nil
		},
		notification.SuccessCmd("Last response copied to clipboard."),
	)
}

// Agent management handlers

func (a *appModel) handleSwitchAgent(agentName string) (tea.Model, tea.Cmd) {
	if err := a.application.SwitchAgent(agentName); err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to switch to agent '%s': %v", agentName, err))
	}

	a.sessionState.SetCurrentAgentName(agentName)
	return a, notification.SuccessCmd(fmt.Sprintf("Switched to agent '%s'", agentName))
}

func (a *appModel) handleCycleAgent() (tea.Model, tea.Cmd) {
	availableAgents := a.sessionState.AvailableAgents()
	if len(availableAgents) <= 1 {
		return a, notification.InfoCmd("No other agents available")
	}

	// Find the current agent index
	currentIndex := -1
	for i, agent := range availableAgents {
		if agent.Name == a.sessionState.CurrentAgentName() {
			currentIndex = i
			break
		}
	}

	// Cycle to the next agent (wrap around to 0 if at the end)
	nextIndex := (currentIndex + 1) % len(availableAgents)
	return a.handleSwitchToAgentByIndex(nextIndex)
}

func (a *appModel) handleSwitchToAgentByIndex(index int) (tea.Model, tea.Cmd) {
	availableAgents := a.sessionState.AvailableAgents()
	if index >= 0 && index < len(availableAgents) {
		agentName := availableAgents[index].Name
		if agentName != a.sessionState.CurrentAgentName() {
			return a, core.CmdHandler(messages.SwitchAgentMsg{AgentName: agentName})
		}
	}
	return a, nil
}

// Toggles

func (a *appModel) handleToggleYolo() (tea.Model, tea.Cmd) {
	sess := a.application.Session()
	sess.ToolsApproved = !sess.ToolsApproved
	a.sessionState.SetYoloMode(sess.ToolsApproved)
	return a, nil
}

func (a *appModel) handleToggleThinking() (tea.Model, tea.Cmd) {
	// Check if the current model supports reasoning
	currentModel := a.application.CurrentAgentModel()
	if !modelsdev.ModelSupportsReasoning(context.Background(), currentModel) {
		return a, notification.InfoCmd("Thinking/reasoning is not supported for the current model")
	}

	sess := a.application.Session()
	sess.Thinking = !sess.Thinking
	a.sessionState.SetThinking(sess.Thinking)

	// Persist the change to the database immediately
	if store := a.application.SessionStore(); store != nil {
		if err := store.UpdateSession(context.Background(), sess); err != nil {
			return a, notification.ErrorCmd(fmt.Sprintf("Failed to save session: %v", err))
		}
	}

	var msg string
	if sess.Thinking {
		msg = "Thinking/reasoning enabled for this session"
	} else {
		msg = "Thinking/reasoning disabled for this session"
	}
	return a, notification.InfoCmd(msg)
}

func (a *appModel) handleToggleHideToolResults() (tea.Model, tea.Cmd) {
	updated, cmd := a.chatPage.Update(messages.ToggleHideToolResultsMsg{})
	a.chatPage = updated.(chat.Page)
	return a, cmd
}

// Cost

func (a *appModel) handleShowCostDialog() (tea.Model, tea.Cmd) {
	sess := a.application.Session()
	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewCostDialog(sess),
	})
}

// Permissions

func (a *appModel) handleShowPermissionsDialog() (tea.Model, tea.Cmd) {
	perms := a.application.PermissionsInfo()
	sess := a.application.Session()
	yoloEnabled := sess != nil && sess.ToolsApproved
	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewPermissionsDialog(perms, yoloEnabled),
	})
}

// MCP prompt handlers

func (a *appModel) handleShowMCPPromptInput(promptName string, promptInfo any) (tea.Model, tea.Cmd) {
	info, ok := promptInfo.(mcptools.PromptInfo)
	if !ok {
		return a, notification.ErrorCmd("Invalid prompt info")
	}

	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewMCPPromptInputDialog(promptName, info),
	})
}

func (a *appModel) handleMCPPrompt(promptName string, arguments map[string]string) (tea.Model, tea.Cmd) {
	promptContent, err := a.application.ExecuteMCPPrompt(context.Background(), promptName, arguments)
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Error executing MCP prompt '%s': %v", promptName, err))
	}

	return a, core.CmdHandler(messages.SendMsg{Content: promptContent})
}

// Miscellaneous handlers

func (a *appModel) handleOpenURL(url string) (tea.Model, tea.Cmd) {
	_ = browser.Open(context.Background(), url)
	return a, nil
}

func (a *appModel) handleAgentCommand(command string) (tea.Model, tea.Cmd) {
	resolvedCommand := a.application.ResolveCommand(context.Background(), command)
	return a, core.CmdHandler(messages.SendMsg{Content: resolvedCommand})
}

// File attachment handler

func (a *appModel) handleAttachFile(filePath string) (tea.Model, tea.Cmd) {
	// If a file path is provided and it's an existing file, attach it directly
	if filePath != "" {
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			// Insert the file reference into the editor using @filepath syntax
			updated, cmd := a.chatPage.Update(messages.InsertFileRefMsg{FilePath: filePath})
			a.chatPage = updated.(chat.Page)
			return a, tea.Batch(cmd, notification.SuccessCmd("File attached: "+filePath))
		}
	}

	// Otherwise, open the file picker dialog
	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewFilePickerDialog(filePath),
	})
}

// Model switching handlers

func (a *appModel) handleOpenModelPicker() (tea.Model, tea.Cmd) {
	// Check if model switching is supported
	if !a.application.SupportsModelSwitching() {
		return a, notification.InfoCmd("Model switching is not supported with remote runtimes")
	}

	models := a.application.AvailableModels(context.Background())
	if len(models) == 0 {
		return a, notification.InfoCmd("No models available for selection")
	}

	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewModelPickerDialog(models),
	})
}

func (a *appModel) handleChangeModel(modelRef string) (tea.Model, tea.Cmd) {
	if err := a.application.SetCurrentAgentModel(context.Background(), modelRef); err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to change model: %v", err))
	}

	if modelRef == "" {
		return a, notification.SuccessCmd("Model reset to default")
	}
	return a, notification.SuccessCmd(fmt.Sprintf("Model changed to %s", modelRef))
}

// Theme handlers

func (a *appModel) handleOpenThemePicker() (tea.Model, tea.Cmd) {
	// Get available themes
	themeRefs, err := styles.ListThemeRefs()
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to list themes: %v", err))
	}

	// Get the currently active global theme
	currentTheme := styles.CurrentTheme()
	currentRef := currentTheme.Ref

	// Build theme choices
	var choices []dialog.ThemeChoice

	for _, ref := range themeRefs {
		theme, loadErr := styles.LoadTheme(ref)
		if loadErr != nil {
			continue
		}

		// Use YAML name, or filename as fallback
		name := theme.Name
		if name == "" {
			name = strings.TrimPrefix(ref, styles.UserThemePrefix)
		}

		choices = append(choices, dialog.ThemeChoice{
			Ref:       ref,
			Name:      name,
			IsCurrent: ref == currentRef,
			IsDefault: ref == styles.DefaultThemeRef,
			IsBuiltin: styles.IsBuiltinTheme(ref),
		})
	}

	return a, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewThemePickerDialog(choices, currentRef),
	})
}

func (a *appModel) handleChangeTheme(themeRef string) (tea.Model, tea.Cmd) {
	// Skip if selecting the already-persisted theme - preview already applied it visually,
	// so no need for notification, cache invalidation, or re-persisting.
	if styles.GetPersistedThemeRef() == themeRef {
		return a, nil
	}

	// Load and apply the theme
	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to load theme: %v", err))
	}

	styles.ApplyTheme(theme)

	// Invalidate caches synchronously
	a.invalidateCachesForThemeChange()

	// Persist to user config (global setting)
	if err := styles.SaveThemeToUserConfig(themeRef); err != nil {
		slog.Warn("Failed to save theme to user config", "theme", themeRef, "error", err)
	}

	return a, tea.Sequence(
		notification.SuccessCmd(fmt.Sprintf("Theme changed to %s", theme.Name)),
		core.CmdHandler(messages.ThemeChangedMsg{}),
	)
}

// handleThemePreview applies a theme temporarily for live preview (without persisting).
func (a *appModel) handleThemePreview(themeRef string) (tea.Model, tea.Cmd) {
	// Skip if already on this theme - no need to invalidate caches
	if current := styles.CurrentTheme(); current != nil && current.Ref == themeRef {
		return a, nil
	}

	// Load and apply the theme (without persisting)
	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		// Silently fail for preview - don't show error notification
		return a, nil
	}

	styles.ApplyTheme(theme)

	// Apply theme changed logic synchronously to ensure View() renders with updated styles
	return a.applyThemeChanged()
}

// handleThemeCancelPreview restores the original theme when the user cancels the theme picker.
func (a *appModel) handleThemeCancelPreview(originalRef string) (tea.Model, tea.Cmd) {
	// Skip if already on the original theme - no need to invalidate caches
	if current := styles.CurrentTheme(); current != nil && current.Ref == originalRef {
		return a, nil
	}

	// Load and apply the original theme
	theme, err := styles.LoadTheme(originalRef)
	if err != nil {
		// Fall back to default theme if original can't be loaded
		theme = styles.DefaultTheme()
	}

	styles.ApplyTheme(theme)

	// Apply theme changed logic (invalidates caches, updates watcher, forwards messages)
	return a.applyThemeChanged()
}

// Speech-to-text handlers

// speakTranscriptAndContinue is an internal message that carries a transcript delta
// and the channel to continue listening.
type speakTranscriptAndContinue struct {
	delta string
	ch    <-chan string
}

func (a *appModel) handleStartSpeak() (tea.Model, tea.Cmd) {
	if a.transcriber.IsRunning() {
		return a, nil
	}

	// Start transcription
	transcriptCh := make(chan string, 100)
	err := a.transcriber.Start(context.Background(), func(delta string) {
		select {
		case transcriptCh <- delta:
		default:
			// Channel full, drop the delta
		}
	})
	if err != nil {
		return a, notification.ErrorCmd(fmt.Sprintf("Failed to start listening: %v", err))
	}

	// Set recording mode on the editor to show animated dots
	recordingCmd := a.chatPage.SetRecording(true)

	// Return a command that listens for transcripts and sends them as messages
	return a, tea.Batch(
		notification.InfoCmd("🎤 Listening... (ENTER to send or ESC to cancel)"),
		recordingCmd,
		a.listenForTranscripts(transcriptCh),
	)
}

// listenForTranscripts returns a command that listens for transcript deltas
// and sends them as messages to the TUI.
func (a *appModel) listenForTranscripts(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		delta, ok := <-ch
		if !ok {
			return nil // Channel closed
		}
		return speakTranscriptAndContinue{delta: delta, ch: ch}
	}
}

func (a *appModel) handleStopSpeak() (tea.Model, tea.Cmd) {
	if !a.transcriber.IsRunning() {
		return a, nil
	}

	a.transcriber.Stop()
	recordingCmd := a.chatPage.SetRecording(false)
	return a, tea.Batch(recordingCmd, notification.SuccessCmd("Stopped listening"))
}

func (a *appModel) handleSpeakTranscript(delta string) (tea.Model, tea.Cmd) {
	a.chatPage.InsertText(delta + " ")
	return a, nil
}

func (a *appModel) handleElicitationResponse(action tools.ElicitationAction, content map[string]any) (tea.Model, tea.Cmd) {
	if err := a.application.ResumeElicitation(context.Background(), action, content); err != nil {
		slog.Error("Failed to resume elicitation", "action", action, "error", err)
		return a, notification.ErrorCmd("Failed to complete server request: " + err.Error())
	}
	return a, nil
}
