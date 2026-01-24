package acp

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/coder/acp-go-sdk"
	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/version"
)

// Agent implements the ACP Agent interface for cagent
type Agent struct {
	agentSource config.Source
	runConfig   *config.RuntimeConfig
	sessions    map[string]*Session

	conn *acp.AgentSideConnection
	team *team.Team
	mu   sync.Mutex
}

var _ acp.Agent = (*Agent)(nil)

// Session represents an ACP session
type Session struct {
	id         string
	sess       *session.Session
	rt         runtime.Runtime
	cancel     context.CancelFunc
	workingDir string
}

// NewAgent creates a new ACP agent
func NewAgent(agentSource config.Source, runConfig *config.RuntimeConfig) *Agent {
	return &Agent{
		agentSource: agentSource,
		runConfig:   runConfig,
		sessions:    make(map[string]*Session),
	}
}

// Stop stops the agent and its toolsets
func (a *Agent) Stop(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.team != nil {
		if err := a.team.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}
}

// SetAgentConnection sets the ACP connection
func (a *Agent) SetAgentConnection(conn *acp.AgentSideConnection) {
	a.conn = conn
}

// Initialize implements [acp.Agent]
func (a *Agent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	slog.Debug("ACP Initialize called", "client_version", params.ProtocolVersion)

	a.mu.Lock()
	defer a.mu.Unlock()
	t, err := teamloader.Load(ctx, a.agentSource, a.runConfig, teamloader.WithToolsetRegistry(createToolsetRegistry(a)))
	if err != nil {
		return acp.InitializeResponse{}, fmt.Errorf("failed to load teams: %w", err)
	}
	a.team = t
	slog.Debug("Teams loaded successfully", "source", a.agentSource.Name(), "agent_count", t.Size())

	agentTitle := "cagent"
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentInfo: &acp.Implementation{
			Name:    "cagent",
			Version: version.Version,
			Title:   &agentTitle,
		},
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false,
			PromptCapabilities: acp.PromptCapabilities{
				EmbeddedContext: true,
				Image:           false, // Not yet supported
				Audio:           false, // Not yet supported
			},
			McpCapabilities: acp.McpCapabilities{
				Http: false, // MCP servers from client not yet supported
				Sse:  false, // MCP servers from client not yet supported
			},
		},
	}, nil
}

// NewSession implements [acp.Agent]
func (a *Agent) NewSession(_ context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := uuid.New().String()
	slog.Debug("ACP NewSession called", "session_id", sid, "cwd", params.Cwd)

	// Log warning if MCP servers are provided (not yet supported)
	if len(params.McpServers) > 0 {
		slog.Warn("MCP servers provided by client are not yet supported", "count", len(params.McpServers))
	}

	rt, err := runtime.New(a.team, runtime.WithCurrentAgent("root"))
	if err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("failed to create runtime: %w", err)
	}

	a.mu.Lock()
	a.sessions[sid] = &Session{
		id:         sid,
		sess:       session.New(session.WithTitle("ACP Session " + sid)),
		rt:         rt,
		workingDir: params.Cwd,
	}
	a.mu.Unlock()

	return acp.NewSessionResponse{SessionId: acp.SessionId(sid)}, nil
}

// Authenticate implements [acp.Agent]
func (a *Agent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	slog.Debug("ACP Authenticate called")
	return acp.AuthenticateResponse{}, nil
}

// LoadSession implements [acp.Agent] (optional, not supported)
func (a *Agent) LoadSession(context.Context, acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	slog.Debug("ACP LoadSession called (not supported)")
	return acp.LoadSessionResponse{}, fmt.Errorf("load session not supported")
}

// Cancel implements [acp.Agent]
func (a *Agent) Cancel(_ context.Context, params acp.CancelNotification) error {
	sid := string(params.SessionId)
	slog.Debug("ACP Cancel called", "session_id", sid)

	a.mu.Lock()
	acpSess, ok := a.sessions[sid]
	a.mu.Unlock()

	if ok && acpSess != nil && acpSess.cancel != nil {
		acpSess.cancel()
	}

	return nil
}

// Prompt implements [acp.Agent]
func (a *Agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)
	slog.Debug("ACP Prompt called", "session_id", sid)

	a.mu.Lock()
	acpSess, ok := a.sessions[sid]
	a.mu.Unlock()

	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sid)
	}

	// Cancel any previous turn
	a.mu.Lock()
	if acpSess.cancel != nil {
		prev := acpSess.cancel
		a.mu.Unlock()
		prev()
	} else {
		a.mu.Unlock()
	}

	// Create a new context for this turn
	turnCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	acpSess.cancel = cancel
	a.mu.Unlock()

	// Build user message from prompt content blocks
	userContent := a.buildUserContent(ctx, sid, params.Prompt)

	if userContent != "" {
		acpSess.sess.AddMessage(session.UserMessage(userContent))
	}

	// Run the agent and stream updates
	if err := a.runAgent(turnCtx, acpSess); err != nil {
		if turnCtx.Err() != nil {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		return acp.PromptResponse{}, err
	}

	a.mu.Lock()
	acpSess.cancel = nil
	a.mu.Unlock()

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// buildUserContent constructs user message text from ACP content blocks
func (a *Agent) buildUserContent(ctx context.Context, sessionID string, prompt []acp.ContentBlock) string {
	var parts []string

	for _, content := range prompt {
		switch {
		case content.Text != nil:
			parts = append(parts, content.Text.Text)

		case content.ResourceLink != nil:
			// Try to read the file content via ACP client
			rl := content.ResourceLink
			slog.Debug("Processing resource link", "uri", rl.Uri, "name", rl.Name)

			// Attempt to read file content if it's a file URI
			if fileContent := a.readResourceLink(ctx, sessionID, rl); fileContent != "" {
				parts = append(parts, fmt.Sprintf("\n\n--- File: %s ---\n%s\n--- End File ---\n", rl.Name, fileContent))
			} else {
				// Fallback: include metadata about the resource
				parts = append(parts, fmt.Sprintf("\n[Referenced file: %s (URI: %s)]\n", rl.Name, rl.Uri))
			}

		case content.Resource != nil:
			// Embedded resource - extract content directly
			res := content.Resource.Resource
			if res.TextResourceContents != nil {
				slog.Debug("Processing embedded text resource", "uri", res.TextResourceContents.Uri)
				parts = append(parts, fmt.Sprintf("\n\n--- Resource: %s ---\n%s\n--- End Resource ---\n",
					res.TextResourceContents.Uri, res.TextResourceContents.Text))
			} else if res.BlobResourceContents != nil {
				slog.Debug("Processing embedded blob resource", "uri", res.BlobResourceContents.Uri)
				parts = append(parts, fmt.Sprintf("\n[Binary resource: %s (type: %s)]\n",
					res.BlobResourceContents.Uri, stringOrDefault(res.BlobResourceContents.MimeType, "unknown")))
			}

		case content.Image != nil:
			slog.Debug("Image content received but not yet fully supported")
			parts = append(parts, "[Image content provided]")

		case content.Audio != nil:
			slog.Debug("Audio content received but not yet supported")
			parts = append(parts, "[Audio content provided]")
		}
	}

	return strings.Join(parts, "")
}

// readResourceLink attempts to read a text file referenced by an ACP resource link.
//
// For security reasons, this function applies basic path hardening:
//
//   - Only relative paths are allowed
//
//   - Path traversal (e.g. "../") is blocked
//
//     NOTE: This is defense-in-depth. The ACP server may apply its own
//     validation, but we avoid sending unsafe paths altogether.
//
// If the path is considered unsafe or the file cannot be read,
// an empty string is returned and the error is logged at debug level.
func (a *Agent) readResourceLink(
	ctx context.Context,
	sessionID string,
	rl *acp.ContentBlockResourceLink,
) string {
	// Strip the file:// prefix if present
	path := strings.TrimPrefix(rl.Uri, "file://")

	// Clean the path to normalize separators and remove redundant elements
	clean := filepath.Clean(path)

	// Basic hardening: block absolute paths and path traversal
	// This prevents access outside the intended working directory.
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		slog.Warn("Blocked unsafe file resource link", "path", path)
		return ""
	}

	// Attempt to read the file via the ACP connection
	resp, err := a.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      clean,
	})
	if err != nil {
		slog.Debug("Failed to read resource link", "path", clean, "error", err)
		return ""
	}

	return resp.Content
}

// stringOrDefault returns the string value or a default if nil
func stringOrDefault(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// SetSessionMode implements acp.Agent (optional)
func (a *Agent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	// We don't implement session modes, cagent agents have only one mode (for now? ;) ).
	return acp.SetSessionModeResponse{}, nil
}

// runAgent runs a single agent loop and streams updates to the ACP client
func (a *Agent) runAgent(ctx context.Context, acpSess *Session) error {
	slog.Debug("Running agent turn", "session_id", acpSess.id)

	ctx = withSessionID(ctx, acpSess.id)

	// Emit available commands at start of first turn
	if err := a.emitAvailableCommands(ctx, acpSess); err != nil {
		slog.Debug("Failed to emit available commands", "error", err)
		// Don't fail the turn, this is not critical
	}

	eventsChan := acpSess.rt.RunStream(ctx, acpSess.sess)

	for event := range eventsChan {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch e := event.(type) {
		case *runtime.AgentChoiceEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    acp.UpdateAgentMessageText(e.Content),
			}); err != nil {
				return err
			}

		case *runtime.AgentChoiceReasoningEvent:
			// Send reasoning/thinking content as agent thought
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    acp.UpdateAgentThoughtText(e.Content),
			}); err != nil {
				return err
			}

		case *runtime.ToolCallConfirmationEvent:
			if err := a.handleToolCallConfirmation(ctx, acpSess, e); err != nil {
				return err
			}

		case *runtime.ToolCallEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    buildToolCallStart(e.ToolCall, e.ToolDefinition),
			}); err != nil {
				return err
			}

		case *runtime.ToolCallResponseEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    buildToolCallComplete(e.ToolCall, e.Response),
			}); err != nil {
				return err
			}

			// Check if this is a todo tool response and emit plan update
			if isTodoTool(e.ToolCall.Function.Name) && e.Result != nil && e.Result.Meta != nil {
				if planUpdate := buildPlanUpdateFromTodos(e.Result.Meta); planUpdate != nil {
					if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
						SessionId: acp.SessionId(acpSess.id),
						Update:    *planUpdate,
					}); err != nil {
						return err
					}
				}
			}

		case *runtime.ErrorEvent:
			if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(acpSess.id),
				Update:    acp.UpdateAgentMessageText(fmt.Sprintf("\n\nError: %s\n", e.Error)),
			}); err != nil {
				return err
			}

		case *runtime.MaxIterationsReachedEvent:
			if err := a.handleMaxIterationsReached(ctx, acpSess, e); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleToolCallConfirmation handles tool call permission requests
func (a *Agent) handleToolCallConfirmation(ctx context.Context, acpSess *Session, e *runtime.ToolCallConfirmationEvent) error {
	toolCallUpdate := buildToolCallUpdate(e.ToolCall, e.ToolDefinition, acp.ToolCallStatusPending)

	permResp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSess.id),
		ToolCall:  toolCallUpdate,
		Options: []acp.PermissionOption{
			{
				Kind:     acp.PermissionOptionKindAllowOnce,
				Name:     "Allow this action",
				OptionId: "allow",
			},
			{
				Kind:     acp.PermissionOptionKindAllowAlways,
				Name:     "Allow and remember my choice",
				OptionId: "allow-always",
			},
			{
				Kind:     acp.PermissionOptionKindRejectOnce,
				Name:     "Skip this action",
				OptionId: "reject",
			},
		},
	})
	if err != nil {
		return err
	}

	// Handle permission outcome
	if permResp.Outcome.Cancelled != nil {
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeReject})
		return nil
	}

	if permResp.Outcome.Selected == nil {
		return fmt.Errorf("unexpected permission outcome")
	}

	switch string(permResp.Outcome.Selected.OptionId) {
	case "allow":
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeApprove})
	case "allow-always":
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeApproveSession})
	case "reject":
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeReject})
	default:
		return fmt.Errorf("unexpected permission option: %s", permResp.Outcome.Selected.OptionId)
	}

	return nil
}

// handleMaxIterationsReached handles max iterations events
func (a *Agent) handleMaxIterationsReached(ctx context.Context, acpSess *Session, e *runtime.MaxIterationsReachedEvent) error {
	permResp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSess.id),
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: "max_iterations",
			Title:      acp.Ptr(fmt.Sprintf("Maximum iterations (%d) reached", e.MaxIterations)),
			Kind:       acp.Ptr(acp.ToolKindExecute),
			Status:     acp.Ptr(acp.ToolCallStatusPending),
		},
		Options: []acp.PermissionOption{
			{
				Kind:     acp.PermissionOptionKindAllowOnce,
				Name:     "Continue",
				OptionId: "continue",
			},
			{
				Kind:     acp.PermissionOptionKindRejectOnce,
				Name:     "Stop",
				OptionId: "stop",
			},
		},
	})
	if err != nil {
		return err
	}

	if permResp.Outcome.Cancelled != nil || permResp.Outcome.Selected == nil ||
		string(permResp.Outcome.Selected.OptionId) == "stop" {
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeReject})
	} else {
		acpSess.rt.Resume(ctx, runtime.ResumeRequest{Type: runtime.ResumeTypeApprove})
	}

	return nil
}

// buildToolCallStart creates a tool call start update
func buildToolCallStart(toolCall tools.ToolCall, tool tools.Tool) acp.SessionUpdate {
	kind := determineToolKind(toolCall.Function.Name, tool)
	title := cmp.Or(tool.Annotations.Title, toolCall.Function.Name)

	args := parseToolCallArguments(toolCall.Function.Arguments)
	locations := extractLocations(args)

	opts := []acp.ToolCallStartOpt{
		acp.WithStartKind(kind),
		acp.WithStartStatus(acp.ToolCallStatusPending),
		acp.WithStartRawInput(args),
	}

	if len(locations) > 0 {
		opts = append(opts, acp.WithStartLocations(locations))
	}

	return acp.StartToolCall(
		acp.ToolCallId(toolCall.ID),
		title,
		opts...,
	)
}

// extractLocations extracts file locations from tool call arguments
func extractLocations(args map[string]any) []acp.ToolCallLocation {
	var locations []acp.ToolCallLocation

	// Check for common path argument names
	pathKeys := []string{"path", "file", "filepath", "filename", "file_path"}
	for _, key := range pathKeys {
		if pathVal, ok := args[key].(string); ok && pathVal != "" {
			loc := acp.ToolCallLocation{Path: pathVal}
			// Check for line number
			if line, ok := args["line"].(float64); ok {
				lineInt := int(line)
				loc.Line = &lineInt
			}
			locations = append(locations, loc)
			break
		}
	}

	// Check for paths array (e.g., read_multiple_files)
	if paths, ok := args["paths"].([]any); ok {
		for _, p := range paths {
			if pathStr, ok := p.(string); ok && pathStr != "" {
				locations = append(locations, acp.ToolCallLocation{Path: pathStr})
			}
		}
	}

	return locations
}

// determineToolKind maps tool names and annotations to ACP tool kinds
func determineToolKind(toolName string, tool tools.Tool) acp.ToolKind {
	// Check annotations first
	if tool.Annotations.ReadOnlyHint {
		return acp.ToolKindRead
	}
	if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
		return acp.ToolKindDelete
	}

	// Map by tool name patterns
	switch {
	// Read operations
	case strings.HasPrefix(toolName, "read_"),
		strings.HasPrefix(toolName, "get_"),
		strings.HasPrefix(toolName, "list_"),
		toolName == "directory_tree":
		return acp.ToolKindRead

	// Edit operations
	case strings.HasPrefix(toolName, "edit_"),
		strings.HasPrefix(toolName, "write_"),
		strings.HasPrefix(toolName, "update_"),
		strings.HasPrefix(toolName, "create_"),
		strings.HasPrefix(toolName, "add_"):
		return acp.ToolKindEdit

	// Delete operations
	case strings.HasPrefix(toolName, "delete_"),
		strings.HasPrefix(toolName, "remove_"),
		strings.HasPrefix(toolName, "stop_"):
		return acp.ToolKindDelete

	// Search operations
	case strings.HasPrefix(toolName, "search_"),
		strings.HasPrefix(toolName, "find_"):
		return acp.ToolKindSearch

	// Think tool
	case toolName == "think":
		return acp.ToolKindThink

	// Fetch/HTTP operations
	case toolName == "fetch",
		strings.HasPrefix(toolName, "http_"):
		return acp.ToolKindFetch

	// Shell/execution operations
	case toolName == "shell",
		strings.HasPrefix(toolName, "run_"),
		strings.HasPrefix(toolName, "exec_"):
		return acp.ToolKindExecute

	// Transfer/handoff
	case toolName == "transfer_task",
		toolName == "handoff":
		return acp.ToolKindSwitchMode

	default:
		return acp.ToolKindOther
	}
}

// buildToolCallComplete creates a tool call completion update
func buildToolCallComplete(toolCall tools.ToolCall, output string) acp.SessionUpdate {
	// Check if this is a file edit operation and try to extract diff info
	if isFileEditTool(toolCall.Function.Name) {
		if diffContent := extractDiffContent(toolCall, output); diffContent != nil {
			return acp.UpdateToolCall(
				acp.ToolCallId(toolCall.ID),
				acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
				acp.WithUpdateContent([]acp.ToolCallContent{*diffContent}),
				acp.WithUpdateRawOutput(map[string]any{"content": output}),
			)
		}
	}

	return acp.UpdateToolCall(
		acp.ToolCallId(toolCall.ID),
		acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
		acp.WithUpdateContent([]acp.ToolCallContent{acp.ToolContent(acp.TextBlock(output))}),
		acp.WithUpdateRawOutput(map[string]any{"content": output}),
	)
}

// isFileEditTool returns true if the tool is a file editing operation
func isFileEditTool(toolName string) bool {
	return slices.Contains([]string{"edit_file", "write_file"}, toolName)
}

// extractDiffContent tries to create a diff content block from edit tool arguments
func extractDiffContent(toolCall tools.ToolCall, _ string) *acp.ToolCallContent {
	args := parseToolCallArguments(toolCall.Function.Arguments)

	// Get the path from arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil
	}

	// For edit_file, extract the edits
	if toolCall.Function.Name == "edit_file" {
		edits, ok := args["edits"].([]any)
		if !ok || len(edits) == 0 {
			return nil
		}

		// Build combined diff from all edits
		var oldText, newText string
		for _, edit := range edits {
			editMap, ok := edit.(map[string]any)
			if !ok {
				continue
			}
			if old, ok := editMap["oldText"].(string); ok {
				oldText += old + "\n"
			}
			if newVal, ok := editMap["newText"].(string); ok {
				newText += newVal + "\n"
			}
		}

		if oldText != "" || newText != "" {
			diff := acp.ToolDiffContent(path, newText, oldText)
			return &diff
		}
	}

	// For write_file, the entire content is new
	if toolCall.Function.Name == "write_file" {
		if content, ok := args["content"].(string); ok {
			diff := acp.ToolDiffContent(path, content)
			return &diff
		}
	}

	return nil
}

// buildToolCallUpdate creates a tool call update for permission requests
func buildToolCallUpdate(toolCall tools.ToolCall, tool tools.Tool, status acp.ToolCallStatus) acp.RequestPermissionToolCall {
	kind := acp.ToolKindExecute
	title := cmp.Or(tool.Annotations.Title, toolCall.Function.Name)

	if tool.Annotations.ReadOnlyHint {
		kind = acp.ToolKindRead
	}

	return acp.RequestPermissionToolCall{
		ToolCallId: acp.ToolCallId(toolCall.ID),
		Title:      acp.Ptr(title),
		Kind:       acp.Ptr(kind),
		Status:     acp.Ptr(status),
		RawInput:   parseToolCallArguments(toolCall.Function.Arguments),
	}
}

// parseToolCallArguments parses JSON tool call arguments into a map
func parseToolCallArguments(argsJSON string) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		slog.Warn("Failed to parse tool call arguments", "error", err)
		return map[string]any{"raw": argsJSON}
	}
	return args
}

// isTodoTool returns true if the tool is a todo management tool
func isTodoTool(toolName string) bool {
	return slices.Contains([]string{
		builtin.ToolNameCreateTodo,
		builtin.ToolNameCreateTodos,
		builtin.ToolNameUpdateTodos,
		builtin.ToolNameListTodos,
	}, toolName)
}

// buildPlanUpdateFromTodos converts todo metadata to an ACP plan update
func buildPlanUpdateFromTodos(meta any) *acp.SessionUpdate {
	// Meta should be a slice of todos
	todos, ok := meta.([]builtin.Todo)
	if !ok {
		slog.Debug("Todo meta is not []builtin.Todo", "type", fmt.Sprintf("%T", meta))
		return nil
	}

	if len(todos) == 0 {
		return nil
	}

	entries := make([]acp.PlanEntry, 0, len(todos))
	for _, todo := range todos {
		entries = append(entries, acp.PlanEntry{
			Content:  todo.Description,
			Status:   mapTodoStatusToACP(todo.Status),
			Priority: acp.PlanEntryPriorityMedium,
		})
	}

	update := acp.UpdatePlan(entries...)
	return &update
}

// mapTodoStatusToACP converts cagent todo status to ACP plan entry status
func mapTodoStatusToACP(status string) acp.PlanEntryStatus {
	switch status {
	case "pending":
		return acp.PlanEntryStatusPending
	case "in-progress":
		return acp.PlanEntryStatusInProgress
	case "completed":
		return acp.PlanEntryStatusCompleted
	default:
		return acp.PlanEntryStatusPending
	}
}

// emitAvailableCommands sends the list of available slash commands to the client
func (a *Agent) emitAvailableCommands(ctx context.Context, acpSess *Session) error {
	commands := []acp.AvailableCommand{
		{
			Name:        "new",
			Description: "Clear session history and start fresh",
		},
		{
			Name:        "compact",
			Description: "Generate summary and compact session history",
		},
		{
			Name:        "usage",
			Description: "Display token usage statistics",
		},
	}

	return a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(acpSess.id),
		Update: acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				SessionUpdate:     "available_commands_update",
				AvailableCommands: commands,
			},
		},
	})
}
