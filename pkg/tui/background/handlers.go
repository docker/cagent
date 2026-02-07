package background

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/browser"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

// --- Session management ---

func (m *Model) handleBranchFromEdit(msg messages.BranchFromEditMsg) (tea.Model, tea.Cmd) {
	store := m.application.SessionStore()
	if store == nil {
		return m, notification.ErrorCmd("No session store configured")
	}
	if msg.ParentSessionID == "" {
		return m, notification.ErrorCmd("No parent session for branch")
	}

	ctx := context.Background()

	newSess, err := store.BranchSession(ctx, msg.ParentSessionID, msg.BranchAtPosition)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to branch session: %v", err))
	}

	if current := m.application.Session(); current != nil {
		newSess.HideToolResults = current.HideToolResults
		newSess.ToolsApproved = current.ToolsApproved
	}

	// Preserve sidebar settings across branch
	sidebarSettings := m.chatPage.GetSidebarSettings()

	activeID := m.supervisor.ActiveID()

	// Replace the session in the app and rebuild the chat page
	m.application.ReplaceSession(ctx, newSess)
	m.sessionState = service.NewSessionState(newSess)
	m.chatPage = chat.New(m.application, m.sessionState)
	m.chatPages[activeID] = m.chatPage
	m.sessionStates[activeID] = m.sessionState
	m.dialogMgr = dialog.New()

	// Restore sidebar settings
	m.chatPage.SetSidebarSettings(sidebarSettings)

	if m.keyboardEnhancements != nil {
		updated, _ := m.chatPage.Update(*m.keyboardEnhancements)
		m.chatPage = updated.(chat.Page)
	}

	return m, tea.Sequence(
		m.chatPage.Init(),
		m.resizeAll(),
		m.editor.Focus(),
		core.CmdHandler(messages.SendMsg{
			Content:     msg.Content,
			Attachments: msg.Attachments,
		}),
	)
}

func (m *Model) handleToggleSessionStar(sessionID string) (tea.Model, tea.Cmd) {
	store := m.application.SessionStore()
	if store == nil {
		return m, notification.ErrorCmd("No session store configured")
	}

	currentSess := m.application.Session()
	if currentSess != nil && currentSess.ID == sessionID {
		currentSess.Starred = !currentSess.Starred
		m.chatPage.SetSessionStarred(currentSess.Starred)
		if err := store.UpdateSession(context.Background(), currentSess); err != nil {
			return m, notification.ErrorCmd(fmt.Sprintf("Failed to save session: %v", err))
		}
	} else {
		sess, err := store.GetSession(context.Background(), sessionID)
		if err != nil {
			return m, notification.ErrorCmd(fmt.Sprintf("Failed to load session: %v", err))
		}
		if err := store.SetSessionStarred(context.Background(), sessionID, !sess.Starred); err != nil {
			return m, notification.ErrorCmd(fmt.Sprintf("Failed to update session: %v", err))
		}
	}
	return m, nil
}

func (m *Model) handleSetSessionTitle(title string) (tea.Model, tea.Cmd) {
	if err := m.application.UpdateSessionTitle(context.Background(), title); err != nil {
		if isErrTitleGenerating(err) {
			return m, notification.WarningCmd("Title is being generated, please wait")
		}
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to set session title: %v", err))
	}
	return m, notification.SuccessCmd(fmt.Sprintf("Title set to: %s", title))
}

func (m *Model) handleRegenerateTitle() (tea.Model, tea.Cmd) {
	sess := m.application.Session()
	if sess == nil {
		return m, notification.ErrorCmd("No active session")
	}
	if len(sess.GetLastUserMessages(1)) == 0 {
		return m, notification.ErrorCmd("Cannot regenerate title: no user message in session")
	}
	if err := m.application.RegenerateSessionTitle(context.Background()); err != nil {
		if isErrTitleGenerating(err) {
			return m, notification.WarningCmd("Title is being generated, please wait")
		}
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to regenerate title: %v", err))
	}
	spinnerCmd := m.chatPage.SetTitleRegenerating(true)
	return m, tea.Batch(spinnerCmd, notification.SuccessCmd("Regenerating title..."))
}

func isErrTitleGenerating(err error) bool {
	return err != nil && err.Error() == app.ErrTitleGenerating.Error()
}

// --- Eval / Export / Compact / Copy ---

func (m *Model) handleEvalSession(filename string) (tea.Model, tea.Cmd) {
	evalFile, _ := evaluation.Save(m.application.Session(), filename)
	return m, notification.SuccessCmd(fmt.Sprintf("Eval saved to file %s", evalFile))
}

func (m *Model) handleExportSession(filename string) (tea.Model, tea.Cmd) {
	exportFile, err := m.application.ExportHTML(context.Background(), filename)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to export session: %v", err))
	}
	return m, notification.SuccessCmd(fmt.Sprintf("Session exported to %s", exportFile))
}

func (m *Model) handleCompactSession(additionalPrompt string) (tea.Model, tea.Cmd) {
	return m, m.chatPage.CompactSession(additionalPrompt)
}

func (m *Model) handleCopySessionToClipboard() (tea.Model, tea.Cmd) {
	transcript := m.application.PlainTextTranscript()
	if transcript == "" {
		return m, notification.SuccessCmd("Conversation is empty; nothing copied.")
	}
	return m, tea.Sequence(
		tea.SetClipboard(transcript),
		func() tea.Msg {
			_ = clipboard.WriteAll(transcript)
			return nil
		},
		notification.SuccessCmd("Conversation copied to clipboard."),
	)
}

func (m *Model) handleCopyLastResponseToClipboard() (tea.Model, tea.Cmd) {
	sess := m.application.Session()
	if sess == nil {
		return m, notification.InfoCmd("No active session.")
	}
	lastResponse := sess.GetLastAssistantMessageContent()
	if lastResponse == "" {
		return m, notification.InfoCmd("No assistant response to copy.")
	}
	return m, tea.Sequence(
		tea.SetClipboard(lastResponse),
		func() tea.Msg {
			_ = clipboard.WriteAll(lastResponse)
			return nil
		},
		notification.SuccessCmd("Last response copied to clipboard."),
	)
}

// --- Agent management ---

func (m *Model) handleSwitchAgent(agentName string) (tea.Model, tea.Cmd) {
	if err := m.application.SwitchAgent(agentName); err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to switch to agent '%s': %v", agentName, err))
	}
	m.sessionState.SetCurrentAgentName(agentName)
	return m, notification.SuccessCmd(fmt.Sprintf("Switched to agent '%s'", agentName))
}

func (m *Model) handleCycleAgent() (tea.Model, tea.Cmd) {
	availableAgents := m.sessionState.AvailableAgents()
	if len(availableAgents) <= 1 {
		return m, notification.InfoCmd("No other agents available")
	}
	currentIndex := -1
	for i, agent := range availableAgents {
		if agent.Name == m.sessionState.CurrentAgentName() {
			currentIndex = i
			break
		}
	}
	nextIndex := (currentIndex + 1) % len(availableAgents)
	if nextIndex >= 0 && nextIndex < len(availableAgents) {
		agentName := availableAgents[nextIndex].Name
		if agentName != m.sessionState.CurrentAgentName() {
			return m, core.CmdHandler(messages.SwitchAgentMsg{AgentName: agentName})
		}
	}
	return m, nil
}

// --- Toggles ---

func (m *Model) handleToggleYolo() (tea.Model, tea.Cmd) {
	sess := m.application.Session()
	sess.ToolsApproved = !sess.ToolsApproved
	m.sessionState.SetYoloMode(sess.ToolsApproved)
	return m, nil
}

func (m *Model) handleToggleThinking() (tea.Model, tea.Cmd) {
	currentModel := m.application.CurrentAgentModel()
	if !modelsdev.ModelSupportsReasoning(context.Background(), currentModel) {
		return m, notification.InfoCmd("Thinking/reasoning is not supported for the current model")
	}
	sess := m.application.Session()
	sess.Thinking = !sess.Thinking
	m.sessionState.SetThinking(sess.Thinking)
	if store := m.application.SessionStore(); store != nil {
		if err := store.UpdateSession(context.Background(), sess); err != nil {
			return m, notification.ErrorCmd(fmt.Sprintf("Failed to save session: %v", err))
		}
	}
	var msg string
	if sess.Thinking {
		msg = "Thinking/reasoning enabled for this session"
	} else {
		msg = "Thinking/reasoning disabled for this session"
	}
	return m, notification.InfoCmd(msg)
}

func (m *Model) handleToggleHideToolResults() (tea.Model, tea.Cmd) {
	updated, cmd := m.chatPage.Update(messages.ToggleHideToolResultsMsg{})
	m.chatPage = updated.(chat.Page)
	return m, cmd
}

// --- Dialogs ---

func (m *Model) handleShowCostDialog() (tea.Model, tea.Cmd) {
	sess := m.application.Session()
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewCostDialog(sess),
	})
}

func (m *Model) handleShowPermissionsDialog() (tea.Model, tea.Cmd) {
	perms := m.application.PermissionsInfo()
	sess := m.application.Session()
	yoloEnabled := sess != nil && sess.ToolsApproved
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewPermissionsDialog(perms, yoloEnabled),
	})
}

// --- MCP prompts ---

func (m *Model) handleShowMCPPromptInput(promptName string, promptInfo any) (tea.Model, tea.Cmd) {
	info, ok := promptInfo.(mcptools.PromptInfo)
	if !ok {
		return m, notification.ErrorCmd("Invalid prompt info")
	}
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewMCPPromptInputDialog(promptName, info),
	})
}

func (m *Model) handleMCPPrompt(promptName string, arguments map[string]string) (tea.Model, tea.Cmd) {
	promptContent, err := m.application.ExecuteMCPPrompt(context.Background(), promptName, arguments)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Error executing MCP prompt '%s': %v", promptName, err))
	}
	return m, core.CmdHandler(messages.SendMsg{Content: promptContent})
}

// --- Model picker ---

func (m *Model) handleOpenModelPicker() (tea.Model, tea.Cmd) {
	if !m.application.SupportsModelSwitching() {
		return m, notification.InfoCmd("Model switching is not supported with remote runtimes")
	}
	models := m.application.AvailableModels(context.Background())
	if len(models) == 0 {
		return m, notification.InfoCmd("No models available for selection")
	}
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewModelPickerDialog(models),
	})
}

func (m *Model) handleChangeModel(modelRef string) (tea.Model, tea.Cmd) {
	if err := m.application.SetCurrentAgentModel(context.Background(), modelRef); err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to change model: %v", err))
	}
	if modelRef == "" {
		return m, notification.SuccessCmd("Model reset to default")
	}
	return m, notification.SuccessCmd(fmt.Sprintf("Model changed to %s", modelRef))
}

// --- Theme picker ---

func (m *Model) handleOpenThemePicker() (tea.Model, tea.Cmd) {
	themeRefs, err := styles.ListThemeRefs()
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to list themes: %v", err))
	}
	currentTheme := styles.CurrentTheme()
	currentRef := currentTheme.Ref

	var choices []dialog.ThemeChoice
	for _, ref := range themeRefs {
		theme, loadErr := styles.LoadTheme(ref)
		if loadErr != nil {
			continue
		}
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
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewThemePickerDialog(choices, currentRef),
	})
}

func (m *Model) handleChangeTheme(themeRef string) (tea.Model, tea.Cmd) {
	if styles.GetPersistedThemeRef() == themeRef {
		return m, nil
	}
	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to load theme: %v", err))
	}
	styles.ApplyTheme(theme)
	m.invalidateCachesForThemeChange()

	if err := styles.SaveThemeToUserConfig(themeRef); err != nil {
		slog.Warn("Failed to save theme to user config", "theme", themeRef, "error", err)
	}
	return m, tea.Sequence(
		notification.SuccessCmd(fmt.Sprintf("Theme changed to %s", theme.Name)),
		core.CmdHandler(messages.ThemeChangedMsg{}),
	)
}

func (m *Model) handleThemePreview(themeRef string) (tea.Model, tea.Cmd) {
	if current := styles.CurrentTheme(); current != nil && current.Ref == themeRef {
		return m, nil
	}
	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		return m, nil
	}
	styles.ApplyTheme(theme)
	return m.applyThemeChanged()
}

func (m *Model) handleThemeCancelPreview(originalRef string) (tea.Model, tea.Cmd) {
	if current := styles.CurrentTheme(); current != nil && current.Ref == originalRef {
		return m, nil
	}
	theme, err := styles.LoadTheme(originalRef)
	if err != nil {
		theme = styles.DefaultTheme()
	}
	styles.ApplyTheme(theme)
	return m.applyThemeChanged()
}

func (m *Model) invalidateCachesForThemeChange() {
	markdown.ResetStyles()
	m.statusBar.InvalidateCache()
}

func (m *Model) applyThemeChanged() (tea.Model, tea.Cmd) {
	m.invalidateCachesForThemeChange()

	var cmds []tea.Cmd

	dialogUpdated, dialogCmd := m.dialogMgr.Update(messages.ThemeChangedMsg{})
	m.dialogMgr = dialogUpdated.(dialog.Manager)
	cmds = append(cmds, dialogCmd)

	chatUpdated, chatCmd := m.chatPage.Update(messages.ThemeChangedMsg{})
	m.chatPage = chatUpdated.(chat.Page)
	cmds = append(cmds, chatCmd)

	return m, tea.Batch(cmds...)
}

// handleThemeFileChanged hot-reloads a theme that was modified on disk.
func (m *Model) handleThemeFileChanged(themeRef string) (tea.Model, tea.Cmd) {
	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		return m, notification.ErrorCmd(fmt.Sprintf("Failed to hot-reload theme: %v", err))
	}
	styles.ApplyTheme(theme)
	return m, tea.Batch(
		notification.SuccessCmd("Theme hot-reloaded"),
		core.CmdHandler(messages.ThemeChangedMsg{}),
	)
}

// --- Miscellaneous ---

func (m *Model) handleOpenURL(url string) (tea.Model, tea.Cmd) {
	_ = browser.Open(context.Background(), url)
	return m, nil
}

func (m *Model) handleAgentCommand(command string) (tea.Model, tea.Cmd) {
	resolvedCommand := m.application.ResolveCommand(context.Background(), command)
	return m, core.CmdHandler(messages.SendMsg{Content: resolvedCommand})
}

func (m *Model) handleAttachFile(filePath string) (tea.Model, tea.Cmd) {
	if filePath != "" {
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			// Attach file to the editor directly
			m.editor.AttachFile(filePath)
			return m, notification.SuccessCmd("File attached: " + filePath)
		}
	}
	return m, core.CmdHandler(dialog.OpenDialogMsg{
		Model: dialog.NewFilePickerDialog(filePath),
	})
}

func (m *Model) handleElicitationResponse(action tools.ElicitationAction, content map[string]any) (tea.Model, tea.Cmd) {
	if err := m.application.ResumeElicitation(context.Background(), action, content); err != nil {
		slog.Error("Failed to resume elicitation", "action", action, "error", err)
		return m, notification.ErrorCmd("Failed to complete server request: " + err.Error())
	}
	return m, nil
}

func (m *Model) startShell() (tea.Model, tea.Cmd) {
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
	return m, tea.ExecProcess(cmd, nil)
}
