package app

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/app/export"
	"github.com/docker/cagent/pkg/app/transcript"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config/types"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/sessiontitle"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
	"github.com/docker/cagent/pkg/tui/messages"
)

type App struct {
	runtime                runtime.Runtime
	session                *session.Session
	firstMessage           *string
	firstMessageAttach     string
	events                 chan tea.Msg
	throttleDuration       time.Duration
	cancel                 context.CancelFunc
	currentAgentModel      string                  // Tracks the current agent's model ID from AgentInfoEvent
	exitAfterFirstResponse bool                    // Exit TUI after first assistant response completes
	titleGenerating        atomic.Bool             // True when title generation is in progress
	titleGen               *sessiontitle.Generator // Title generator for local runtime (nil for remote)
}

// Opt is an option for creating a new App.
type Opt func(*App)

// WithFirstMessage sets the first message to send.
func WithFirstMessage(msg string) Opt {
	return func(a *App) {
		a.firstMessage = &msg
	}
}

// WithFirstMessageAttachment sets the attachment path for the first message.
func WithFirstMessageAttachment(path string) Opt {
	return func(a *App) {
		a.firstMessageAttach = path
	}
}

// WithExitAfterFirstResponse configures the app to exit after the first assistant response.
func WithExitAfterFirstResponse() Opt {
	return func(a *App) {
		a.exitAfterFirstResponse = true
	}
}

// WithTitleGenerator sets the title generator for local title generation.
// If not set, title generation will be handled by the runtime (for remote) or skipped.
func WithTitleGenerator(gen *sessiontitle.Generator) Opt {
	return func(a *App) {
		a.titleGen = gen
	}
}

func New(ctx context.Context, rt runtime.Runtime, sess *session.Session, opts ...Opt) *App {
	app := &App{
		runtime:          rt,
		session:          sess,
		events:           make(chan tea.Msg, 128),
		throttleDuration: 50 * time.Millisecond, // Throttle rapid events
	}

	for _, opt := range opts {
		opt(app)
	}

	// Emit startup info (agent, team, tools) through the events channel.
	// This runs in the background so the TUI can start immediately while
	// slow operations (like MCP tool loading) complete asynchronously.
	go func() {
		startupEvents := make(chan runtime.Event, 10)
		go func() {
			defer close(startupEvents)
			rt.EmitStartupInfo(ctx, startupEvents)
		}()
		for event := range startupEvents {
			select {
			case app.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	// If the runtime supports background RAG initialization, start it
	// and forward events to the TUI. Remote runtimes typically handle RAG server-side
	// and won't implement this optional interface.
	if ragRuntime, ok := rt.(runtime.RAGInitializer); ok {
		go ragRuntime.StartBackgroundRAGInit(ctx, func(event runtime.Event) {
			select {
			case app.events <- event:
			case <-ctx.Done():
			}
		})
	}

	return app
}

func (a *App) SendFirstMessage() tea.Cmd {
	if a.firstMessage == nil {
		return nil
	}

	return func() tea.Msg {
		// Use the shared PrepareUserMessage function for consistent attachment handling
		userMsg := cli.PrepareUserMessage(context.Background(), a.runtime, *a.firstMessage, a.firstMessageAttach)

		// If the message has multi-content (attachments), we need to handle it specially
		if len(userMsg.Message.MultiContent) > 0 {
			return messages.SendAttachmentMsg{
				Content: userMsg,
			}
		}

		return messages.SendMsg{
			Content: userMsg.Message.Content,
		}
	}
}

// CurrentAgentCommands returns the commands for the active agent
func (a *App) CurrentAgentCommands(ctx context.Context) types.Commands {
	return a.runtime.CurrentAgentInfo(ctx).Commands
}

// CurrentAgentModel returns the model ID for the current agent.
// Returns the tracked model from AgentInfoEvent, or falls back to session overrides.
// Returns empty string if no model information is available (fail-open scenario).
func (a *App) CurrentAgentModel() string {
	if a.currentAgentModel != "" {
		return a.currentAgentModel
	}
	// Fallback to session overrides
	if a.session != nil && a.session.AgentModelOverrides != nil {
		agentName := a.runtime.CurrentAgentName()
		if modelRef, ok := a.session.AgentModelOverrides[agentName]; ok {
			return modelRef
		}
	}
	return ""
}

// TrackCurrentAgentModel updates the tracked model ID for the current agent.
// This is called when AgentInfoEvent is received from the runtime.
func (a *App) TrackCurrentAgentModel(model string) {
	a.currentAgentModel = model
}

// CurrentMCPPrompts returns the available MCP prompts for the active agent
func (a *App) CurrentMCPPrompts(ctx context.Context) map[string]mcptools.PromptInfo {
	if localRuntime, ok := a.runtime.(*runtime.LocalRuntime); ok {
		return localRuntime.CurrentMCPPrompts(ctx)
	}
	return make(map[string]mcptools.PromptInfo)
}

// ExecuteMCPPrompt executes an MCP prompt with provided arguments and returns the content
func (a *App) ExecuteMCPPrompt(ctx context.Context, promptName string, arguments map[string]string) (string, error) {
	localRuntime, ok := a.runtime.(*runtime.LocalRuntime)
	if !ok {
		return "", fmt.Errorf("MCP prompts are only supported with local runtime")
	}

	currentAgent := localRuntime.CurrentAgent()
	if currentAgent == nil {
		return "", fmt.Errorf("no current agent available")
	}

	for _, toolset := range currentAgent.ToolSets() {
		if mcpToolset, ok := tools.As[*mcptools.Toolset](toolset); ok {
			result, err := mcpToolset.GetPrompt(ctx, promptName, arguments)
			if err == nil {
				// Convert the MCP result to a string format suitable for the editor
				// The result contains Messages which are the prompt content
				if len(result.Messages) == 0 {
					return "No content returned from MCP prompt", nil
				}

				var content string
				for i, message := range result.Messages {
					if i > 0 {
						content += "\n\n"
					}
					if textContent, ok := message.Content.(*mcp.TextContent); ok {
						content += textContent.Text
					} else {
						content += fmt.Sprintf("[Non-text content: %T]", message.Content)
					}
				}
				return content, nil
			}
			// If error is "prompt not found", continue to next toolset
			// Otherwise, return the error
			if err.Error() != "prompt not found" {
				return "", fmt.Errorf("error executing prompt '%s': %w", promptName, err)
			}
		}
	}

	return "", fmt.Errorf("MCP prompt '%s' not found in any active toolset", promptName)
}

// ResolveCommand converts /command to its prompt text
func (a *App) ResolveCommand(ctx context.Context, userInput string) string {
	return runtime.ResolveCommand(ctx, a.runtime, userInput)
}

// EmitStartupInfo emits initial agent, team, and toolset information to the provided channel
func (a *App) EmitStartupInfo(ctx context.Context, events chan runtime.Event) {
	a.runtime.EmitStartupInfo(ctx, events)
}

// Run one agent loop
func (a *App) Run(ctx context.Context, cancel context.CancelFunc, message string, attachments map[string]string) {
	a.cancel = cancel

	// If this is the first message and no title exists, start local title generation
	if a.session.Title == "" && a.titleGen != nil {
		a.titleGenerating.Store(true)
		go a.generateTitle(ctx, []string{message})
	}

	go func() {
		if len(attachments) > 0 {
			multiContent := []chat.MessagePart{
				{
					Type: chat.MessagePartTypeText,
					Text: message,
				},
			}

			for key, dataURL := range attachments {
				multiContent = append(multiContent, chat.MessagePart{
					Type: chat.MessagePartTypeText,
					Text: fmt.Sprintf("Contents of %s: %s", key, dataURL),
				})
			}
			a.session.AddMessage(session.UserMessage(message, multiContent...))
		} else {
			a.session.AddMessage(session.UserMessage(message))
		}
		for event := range a.runtime.RunStream(ctx, a.session) {
			// If context is cancelled, continue draining but don't forward events.
			// This prevents the runtime from blocking on event sends.
			if ctx.Err() != nil {
				continue
			}

			// Clear titleGenerating flag when title is generated (from server for remote runtime)
			if _, ok := event.(*runtime.SessionTitleEvent); ok {
				a.titleGenerating.Store(false)
			}

			a.events <- event
		}
	}()
}

// RunWithMessage runs the agent loop with a pre-constructed message.
// This is used for special cases like image attachments.
func (a *App) RunWithMessage(ctx context.Context, cancel context.CancelFunc, msg *session.Message) {
	a.cancel = cancel

	// If this is the first message and no title exists, start local title generation
	if a.session.Title == "" && a.titleGen != nil {
		a.titleGenerating.Store(true)
		// Extract text content from the message for title generation
		userMessage := msg.Message.Content
		if userMessage == "" && len(msg.Message.MultiContent) > 0 {
			for _, part := range msg.Message.MultiContent {
				if part.Type == chat.MessagePartTypeText {
					userMessage = part.Text
					break
				}
			}
		}
		go a.generateTitle(ctx, []string{userMessage})
	}

	go func() {
		a.session.AddMessage(msg)
		for event := range a.runtime.RunStream(ctx, a.session) {
			// If context is cancelled, continue draining but don't forward events.
			// This prevents the runtime from blocking on event sends.
			if ctx.Err() != nil {
				continue
			}

			// Clear titleGenerating flag when title is generated (from server for remote runtime)
			if _, ok := event.(*runtime.SessionTitleEvent); ok {
				a.titleGenerating.Store(false)
			}

			a.events <- event
		}
	}()
}

func (a *App) RunBangCommand(ctx context.Context, command string) {
	out, _ := exec.CommandContext(ctx, "/bin/sh", "-c", command).CombinedOutput()
	a.events <- runtime.ShellOutput("$ " + command + "\n" + string(out))
}

func (a *App) Subscribe(ctx context.Context, program *tea.Program) {
	throttledChan := a.throttleEvents(ctx, a.events)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-throttledChan:
			if !ok {
				return
			}

			program.Send(msg)
		}
	}
}

// Resume resumes the runtime with the given confirmation request
func (a *App) Resume(req runtime.ResumeRequest) {
	a.runtime.Resume(context.Background(), req)
}

// ResumeElicitation resumes an elicitation request with the given action and content
func (a *App) ResumeElicitation(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
	return a.runtime.ResumeElicitation(ctx, action, content)
}

func (a *App) NewSession() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	// Preserve user-controlled session flags (like /think toggle)
	// so they don't reset to default on /new
	var opts []session.Opt
	if a.session != nil {
		opts = append(opts,
			session.WithThinking(a.session.Thinking),
			session.WithToolsApproved(a.session.ToolsApproved),
			session.WithHideToolResults(a.session.HideToolResults),
		)
	}
	a.session = session.New(opts...)
	// Clear first message so it won't be re-sent on re-init
	a.firstMessage = nil
	a.firstMessageAttach = ""
}

func (a *App) Session() *session.Session {
	return a.session
}

// PermissionsInfo returns combined permissions info from team and session.
// Returns nil if no permissions are configured at either level.
func (a *App) PermissionsInfo() *runtime.PermissionsInfo {
	// Get team-level permissions from runtime
	teamPerms := a.runtime.PermissionsInfo()

	// Get session-level permissions
	var sessionPerms *runtime.PermissionsInfo
	if a.session != nil && a.session.Permissions != nil {
		if len(a.session.Permissions.Allow) > 0 || len(a.session.Permissions.Deny) > 0 {
			sessionPerms = &runtime.PermissionsInfo{
				Allow: a.session.Permissions.Allow,
				Deny:  a.session.Permissions.Deny,
			}
		}
	}

	// Return nil if no permissions configured at any level
	if teamPerms == nil && sessionPerms == nil {
		return nil
	}

	// Merge permissions, with session taking priority (listed first)
	result := &runtime.PermissionsInfo{}
	if sessionPerms != nil {
		result.Allow = append(result.Allow, sessionPerms.Allow...)
		result.Deny = append(result.Deny, sessionPerms.Deny...)
	}
	if teamPerms != nil {
		result.Allow = append(result.Allow, teamPerms.Allow...)
		result.Deny = append(result.Deny, teamPerms.Deny...)
	}

	return result
}

// HasPermissions returns true if any permissions are configured (team or session level).
func (a *App) HasPermissions() bool {
	return a.PermissionsInfo() != nil
}

// SwitchAgent switches the currently active agent for subsequent user messages
func (a *App) SwitchAgent(agentName string) error {
	return a.runtime.SetCurrentAgent(agentName)
}

// SetCurrentAgentModel sets the model for the current agent and persists
// the override in the session. Returns an error if model switching is not
// supported by the runtime (e.g., remote runtimes).
// Pass an empty modelRef to clear the override and use the agent's default model.
func (a *App) SetCurrentAgentModel(ctx context.Context, modelRef string) error {
	modelSwitcher, ok := a.runtime.(runtime.ModelSwitcher)
	if !ok {
		return fmt.Errorf("model switching not supported by this runtime")
	}

	agentName := a.runtime.CurrentAgentName()

	// Set the model override on the runtime (empty modelRef clears the override)
	if err := modelSwitcher.SetAgentModel(ctx, agentName, modelRef); err != nil {
		return err
	}

	// Update the session's model overrides
	if modelRef == "" {
		// Clear the override - remove from map
		delete(a.session.AgentModelOverrides, agentName)
		slog.Debug("Cleared model override from session", "session_id", a.session.ID, "agent", agentName)
	} else {
		// Set the override
		if a.session.AgentModelOverrides == nil {
			a.session.AgentModelOverrides = make(map[string]string)
		}
		a.session.AgentModelOverrides[agentName] = modelRef
		slog.Debug("Set model override in session", "session_id", a.session.ID, "agent", agentName, "model", modelRef)

		// Track custom models (inline provider/model format) in the session
		if strings.Contains(modelRef, "/") {
			a.trackCustomModel(modelRef)
		}
	}

	// Persist the session
	if store := a.runtime.SessionStore(); store != nil {
		if err := store.UpdateSession(ctx, a.session); err != nil {
			return fmt.Errorf("failed to persist model override: %w", err)
		}
		slog.Debug("Persisted session with model override", "session_id", a.session.ID, "overrides", a.session.AgentModelOverrides)
	}

	// Re-emit startup info so the sidebar updates with the new model
	a.runtime.ResetStartupInfo()
	go func() {
		startupEvents := make(chan runtime.Event, 10)
		go func() {
			defer close(startupEvents)
			a.runtime.EmitStartupInfo(ctx, startupEvents)
		}()
		for event := range startupEvents {
			select {
			case a.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// AvailableModels returns the list of models available for selection.
// Returns nil if model switching is not supported.
func (a *App) AvailableModels(ctx context.Context) []runtime.ModelChoice {
	modelSwitcher, ok := a.runtime.(runtime.ModelSwitcher)
	if !ok {
		return nil
	}
	models := modelSwitcher.AvailableModels(ctx)

	// Determine the currently active model for this agent
	agentName := a.runtime.CurrentAgentName()
	currentModelRef := ""
	if a.session != nil && a.session.AgentModelOverrides != nil {
		currentModelRef = a.session.AgentModelOverrides[agentName]
	}

	// Build a set of model refs already in the list
	existingRefs := make(map[string]bool)
	for _, m := range models {
		existingRefs[m.Ref] = true
	}

	// Check if current model is in the list and mark it
	currentFound := currentModelRef == ""
	for i := range models {
		if currentModelRef != "" {
			// An override is set - mark the override as current
			if models[i].Ref == currentModelRef {
				models[i].IsCurrent = true
				currentFound = true
			}
		} else {
			// No override - the default model is current
			models[i].IsCurrent = models[i].IsDefault
		}
	}

	// Add custom models from the session that aren't already in the list
	if a.session != nil {
		for _, customRef := range a.session.CustomModelsUsed {
			if existingRefs[customRef] {
				continue // Already in the list
			}
			existingRefs[customRef] = true

			providerName, modelName, _ := strings.Cut(customRef, "/")
			isCurrent := customRef == currentModelRef
			if isCurrent {
				currentFound = true
			}
			models = append(models, runtime.ModelChoice{
				Name:      customRef,
				Ref:       customRef,
				Provider:  providerName,
				Model:     modelName,
				IsDefault: false,
				IsCurrent: isCurrent,
				IsCustom:  true,
			})
		}
	}

	// If current model is a custom model not in the list, add it
	if !currentFound && strings.Contains(currentModelRef, "/") {
		providerName, modelName, _ := strings.Cut(currentModelRef, "/")
		models = append(models, runtime.ModelChoice{
			Name:      currentModelRef,
			Ref:       currentModelRef,
			Provider:  providerName,
			Model:     modelName,
			IsDefault: false,
			IsCurrent: true,
			IsCustom:  true,
		})
	}

	return models
}

// trackCustomModel adds a custom model to the session's history if not already present.
func (a *App) trackCustomModel(modelRef string) {
	if a.session == nil {
		return
	}

	// Check if already tracked
	if slices.Contains(a.session.CustomModelsUsed, modelRef) {
		return
	}

	a.session.CustomModelsUsed = append(a.session.CustomModelsUsed, modelRef)
	slog.Debug("Tracked custom model in session", "session_id", a.session.ID, "model", modelRef)
}

// SupportsModelSwitching returns true if the runtime supports model switching.
func (a *App) SupportsModelSwitching() bool {
	_, ok := a.runtime.(runtime.ModelSwitcher)
	return ok
}

// ShouldExitAfterFirstResponse returns true if the app is configured to exit
// after the first assistant response completes.
func (a *App) ShouldExitAfterFirstResponse() bool {
	return a.exitAfterFirstResponse
}

func (a *App) CompactSession(additionalPrompt string) {
	if a.session != nil {
		events := make(chan runtime.Event, 100)
		a.runtime.Summarize(context.Background(), a.session, additionalPrompt, events)
		close(events)
		for event := range events {
			a.events <- event
		}
	}
}

func (a *App) PlainTextTranscript() string {
	return transcript.PlainText(a.session)
}

// SessionStore returns the session store for browsing/loading sessions.
// Returns nil if no session store is configured.
func (a *App) SessionStore() session.Store {
	return a.runtime.SessionStore()
}

// ReplaceSession replaces the current session with the given session.
// This is used when loading a past session. It also re-emits startup info
// so the sidebar displays the agent and tool information.
// If the session has stored model overrides, they are applied to the runtime.
func (a *App) ReplaceSession(ctx context.Context, sess *session.Session) {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.session = sess
	// Clear first message so it won't be re-sent on re-init
	a.firstMessage = nil
	a.firstMessageAttach = ""

	// Apply any stored model overrides from the session
	a.applySessionModelOverrides(ctx, sess)

	// Reset and re-emit startup info so the sidebar shows agent/tools info
	a.runtime.ResetStartupInfo()
	go func() {
		startupEvents := make(chan runtime.Event, 10)
		go func() {
			defer close(startupEvents)
			a.runtime.EmitStartupInfo(ctx, startupEvents)
		}()
		for event := range startupEvents {
			select {
			case a.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
}

// applySessionModelOverrides applies any stored model overrides from a loaded session.
func (a *App) applySessionModelOverrides(ctx context.Context, sess *session.Session) {
	if len(sess.AgentModelOverrides) == 0 {
		slog.Debug("No model overrides to apply from session", "session_id", sess.ID)
		return
	}

	// Check if runtime supports model switching
	modelSwitcher, ok := a.runtime.(runtime.ModelSwitcher)
	if !ok {
		slog.Debug("Runtime does not support model switching, skipping overrides")
		return
	}

	slog.Debug("Applying model overrides from session", "session_id", sess.ID, "overrides", sess.AgentModelOverrides)
	for agentName, modelRef := range sess.AgentModelOverrides {
		if err := modelSwitcher.SetAgentModel(ctx, agentName, modelRef); err != nil {
			// Log but don't fail - the session can still be used with default models
			slog.Warn("Failed to apply model override from session", "agent", agentName, "model", modelRef, "error", err)
			a.events <- runtime.Warning(fmt.Sprintf("Failed to apply model override for agent %q: %v", agentName, err), agentName)
		} else {
			slog.Info("Applied model override from session", "agent", agentName, "model", modelRef)
		}
	}
}

// throttleEvents buffers and merges rapid events to prevent UI flooding
func (a *App) throttleEvents(ctx context.Context, in <-chan tea.Msg) <-chan tea.Msg {
	out := make(chan tea.Msg, 128)

	go func() {
		defer close(out)

		var buffer []tea.Msg
		var timerCh <-chan time.Time

		flush := func() {
			for _, msg := range a.mergeEvents(buffer) {
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			}
			buffer = buffer[:0]
			timerCh = nil
		}

		for {
			select {
			case <-ctx.Done():
				return

			case msg, ok := <-in:
				if !ok {
					return
				}

				buffer = append(buffer, msg)
				if a.shouldThrottle(msg) {
					if timerCh == nil {
						timerCh = time.After(a.throttleDuration)
					}
				} else {
					flush()
				}

			case <-timerCh:
				flush()
			}
		}
	}()

	return out
}

// shouldThrottle determines if an event should be buffered/throttled
func (a *App) shouldThrottle(msg tea.Msg) bool {
	switch msg.(type) {
	case *runtime.AgentChoiceEvent:
		return true
	case *runtime.AgentChoiceReasoningEvent:
		return true
	case *runtime.PartialToolCallEvent:
		return true
	default:
		return false
	}
}

// mergeEvents merges consecutive similar events to reduce UI updates
func (a *App) mergeEvents(events []tea.Msg) []tea.Msg {
	if len(events) == 0 {
		return events
	}

	var result []tea.Msg

	// Group events by type and merge
	for i := 0; i < len(events); i++ {
		current := events[i]

		switch ev := current.(type) {
		case *runtime.AgentChoiceEvent:
			// Merge consecutive AgentChoiceEvents with same agent
			merged := ev
			for i+1 < len(events) {
				if next, ok := events[i+1].(*runtime.AgentChoiceEvent); ok && next.AgentName == ev.AgentName {
					// Concatenate content
					merged = &runtime.AgentChoiceEvent{
						Type:         ev.Type,
						Content:      merged.Content + next.Content,
						AgentContext: ev.AgentContext,
					}
					i++
				} else {
					break
				}
			}
			result = append(result, merged)

		case *runtime.AgentChoiceReasoningEvent:
			// Merge consecutive AgentChoiceReasoningEvents with same agent
			merged := ev
			for i+1 < len(events) {
				if next, ok := events[i+1].(*runtime.AgentChoiceReasoningEvent); ok && next.AgentName == ev.AgentName {
					// Concatenate content
					merged = &runtime.AgentChoiceReasoningEvent{
						Type:         ev.Type,
						Content:      merged.Content + next.Content,
						AgentContext: ev.AgentContext,
					}
					i++
				} else {
					break
				}
			}
			result = append(result, merged)

		case *runtime.PartialToolCallEvent:
			// For PartialToolCallEvent, keep only the latest one per tool call ID
			// Only merge consecutive events with the same ID
			latest := ev
			for i+1 < len(events) {
				if next, ok := events[i+1].(*runtime.PartialToolCallEvent); ok && next.ToolCall.ID == ev.ToolCall.ID {
					latest = next
					i++
				} else {
					break
				}
			}
			result = append(result, latest)

		default:
			// Pass through other events as-is
			result = append(result, current)
		}
	}

	return result
}

// ExportHTML exports the current session as a standalone HTML file.
// If filename is empty, a default name based on the session title and timestamp is used.
func (a *App) ExportHTML(ctx context.Context, filename string) (string, error) {
	agentInfo := a.runtime.CurrentAgentInfo(ctx)
	return export.SessionToFile(a.session, agentInfo.Description, filename)
}

// UpdateSessionTitle updates the current session's title and persists it.
// It works with both local and remote runtimes.
// ErrTitleGenerating is returned when attempting to set a title while generation is in progress.
var ErrTitleGenerating = fmt.Errorf("title generation in progress, please wait")

func (a *App) UpdateSessionTitle(ctx context.Context, title string) error {
	if a.session == nil {
		return fmt.Errorf("no active session")
	}

	// Prevent manual title edits while generation is in progress
	if a.titleGenerating.Load() {
		return ErrTitleGenerating
	}

	// Update in-memory session
	a.session.Title = title

	// Check if runtime is a RemoteRuntime and use its UpdateSessionTitle method
	if remoteRT, ok := a.runtime.(*runtime.RemoteRuntime); ok {
		if err := remoteRT.UpdateSessionTitle(ctx, title); err != nil {
			return fmt.Errorf("failed to update session title on remote: %w", err)
		}
	} else if store := a.runtime.SessionStore(); store != nil {
		// For local runtime, persist via session store
		if err := store.UpdateSession(ctx, a.session); err != nil {
			return fmt.Errorf("failed to persist session title: %w", err)
		}
	}

	// Emit a SessionTitleEvent to update the UI consistently
	a.events <- runtime.SessionTitle(a.session.ID, title)
	return nil
}

// IsTitleGenerating returns true if title generation is currently in progress.
func (a *App) IsTitleGenerating() bool {
	return a.titleGenerating.Load()
}

// generateTitle generates a title using the local title generator.
// This method always clears the titleGenerating flag when done (success or failure).
// It should be called in a goroutine.
func (a *App) generateTitle(ctx context.Context, userMessages []string) {
	// Always clear the flag when done, whether success or failure
	defer a.titleGenerating.Store(false)

	if a.titleGen == nil {
		slog.Debug("No title generator available, skipping title generation")
		return
	}

	title, err := a.titleGen.Generate(ctx, a.session.ID, userMessages)
	if err != nil {
		slog.Error("Failed to generate session title", "session_id", a.session.ID, "error", err)
		return
	}

	if title == "" {
		return
	}

	// Update the session title
	a.session.Title = title

	// Persist the title
	if remoteRT, ok := a.runtime.(*runtime.RemoteRuntime); ok {
		if err := remoteRT.UpdateSessionTitle(ctx, title); err != nil {
			slog.Error("Failed to persist title on remote", "session_id", a.session.ID, "error", err)
		}
	} else if store := a.runtime.SessionStore(); store != nil {
		if err := store.UpdateSession(ctx, a.session); err != nil {
			slog.Error("Failed to persist title", "session_id", a.session.ID, "error", err)
		}
	}

	// Emit the title event to update the UI
	a.events <- runtime.SessionTitle(a.session.ID, title)
}

// RegenerateSessionTitle triggers AI-based title regeneration for the current session.
// Returns ErrTitleGenerating if a title generation is already in progress.
func (a *App) RegenerateSessionTitle(ctx context.Context) error {
	if a.session == nil {
		return fmt.Errorf("no active session")
	}

	// Check if title generation is already in progress
	if a.titleGenerating.Load() {
		return ErrTitleGenerating
	}

	// For local runtime with title generator, use it directly
	if a.titleGen != nil {
		a.titleGenerating.Store(true)

		// Collect user messages for title generation
		var userMessages []string
		for _, msg := range a.session.GetAllMessages() {
			if msg.Message.Role == chat.MessageRoleUser {
				userMessages = append(userMessages, msg.Message.Content)
			}
		}

		go a.generateTitle(ctx, userMessages)
		return nil
	}

	// For remote runtime, title regeneration is not yet supported
	// (the server would need to implement this)
	slog.Debug("Title regeneration not available for remote runtime", "session_id", a.session.ID)
	return fmt.Errorf("title regeneration not available")
}
