package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/tools"
)

type mcpClient interface {
	Initialize(ctx context.Context, request *mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error]
	CallTool(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error)
	ListPrompts(ctx context.Context, request *mcp.ListPromptsParams) iter.Seq2[*mcp.Prompt, error]
	GetPrompt(ctx context.Context, request *mcp.GetPromptParams) (*mcp.GetPromptResult, error)
	SetElicitationHandler(handler tools.ElicitationHandler)
	SetOAuthSuccessHandler(handler func())
	SetManagedOAuth(managed bool)
	Close(ctx context.Context) error
}

// Toolset represents a set of MCP tools
type Toolset struct {
	name         string
	mcpClient    mcpClient
	logID        string
	instructions string
	mu           sync.Mutex
	started      bool
}

var _ tools.ToolSet = (*Toolset)(nil)

// Verify that Toolset implements optional capability interfaces
var (
	_ tools.Instructable = (*Toolset)(nil)
	_ tools.Elicitable   = (*Toolset)(nil)
	_ tools.OAuthCapable = (*Toolset)(nil)
)

// NewToolsetCommand creates a new MCP toolset from a command.
func NewToolsetCommand(name, command string, args, env []string, cwd string) *Toolset {
	slog.Debug("Creating Stdio MCP toolset", "command", command, "args", args)

	return &Toolset{
		name:      name,
		mcpClient: newStdioCmdClient(command, args, env, cwd),
		logID:     command,
	}
}

// NewRemoteToolset creates a new MCP toolset from a remote MCP Server.
func NewRemoteToolset(name, url, transport string, headers map[string]string) *Toolset {
	slog.Debug("Creating Remote MCP toolset", "url", url, "transport", transport, "headers", headers)

	return &Toolset{
		name:      name,
		mcpClient: newRemoteClient(url, transport, headers, NewInMemoryTokenStore()),
		logID:     url,
	}
}

func (ts *Toolset) Start(ctx context.Context) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.started {
		return nil
	}

	err := ts.doStart(ctx)
	if err == nil {
		ts.started = true
	}
	return err
}

func (ts *Toolset) doStart(ctx context.Context) error {
	// The MCP toolset connection needs to persist beyond the initial HTTP request that triggered its creation.
	// When OAuth succeeds, subsequent agent requests should reuse the already-authenticated MCP connection.
	// But if the connection's underlying context is tied to the first HTTP request, it gets cancelled when that request
	// completes, killing the connection even though OAuth succeeded.
	// This is critical for OAuth flows where the toolset connection needs to remain alive after the initial HTTP request completes.
	ctx = context.WithoutCancel(ctx)

	slog.Debug("Starting MCP toolset", "server", ts.logID)

	initRequest := &mcp.InitializeRequest{
		Params: &mcp.InitializeParams{
			ClientInfo: &mcp.Implementation{
				Name:    "cagent",
				Version: "1.0.0",
			},
			Capabilities: &mcp.ClientCapabilities{
				Elicitation: &mcp.ElicitationCapabilities{
					Form: &mcp.FormElicitationCapabilities{},
					URL:  &mcp.URLElicitationCapabilities{},
				},
			},
		},
	}

	var result *mcp.InitializeResult
	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		var err error
		result, err = ts.mcpClient.Initialize(ctx, initRequest)
		if err == nil {
			break
		}
		// TODO(krissetto): This is a temporary fix to handle the case where the remote server hasn't finished its async init
		// and we send the notifications/initialized message before the server is ready. Fix upstream in mcp-go if possible.
		//
		// Only retry when initialization fails due to sending the initialized notification.
		if !isInitNotificationSendError(err) {

			// EOF means the MCP server is unavailable or closed the connection.
			// This is not a fatal error and should not fail the agent execution.
			if errors.Is(err, io.EOF) {
				slog.Debug(
					"MCP client unavailable (EOF), skipping MCP toolset",
					"server", ts.logID,
				)
				return nil
			}

			slog.Error("Failed to initialize MCP client", "error", err)
			return fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		if attempt >= maxRetries {
			slog.Error("Failed to initialize MCP client after retries", "error", err)
			return fmt.Errorf("failed to initialize MCP client after retries: %w", err)
		}
		backoff := time.Duration(200*(attempt+1)) * time.Millisecond
		slog.Debug("MCP initialize failed to send initialized notification; retrying", "id", ts.logID, "attempt", attempt+1, "backoff_ms", backoff.Milliseconds())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("failed to initialize MCP client: %w", ctx.Err())
		}
	}

	slog.Debug("Started MCP toolset successfully", "server", ts.logID)
	ts.instructions = result.Instructions
	return nil
}

func (ts *Toolset) Instructions() string {
	ts.mu.Lock()
	started := ts.started
	ts.mu.Unlock()
	if !started {
		// TODO: this should never happen...
		return ""
	}
	return ts.instructions
}

func (ts *Toolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	ts.mu.Lock()
	started := ts.started
	ts.mu.Unlock()
	if !started {
		return nil, errors.New("toolset not started")
	}

	slog.Debug("Listing MCP tools")

	resp := ts.mcpClient.ListTools(ctx, &mcp.ListToolsParams{})

	var toolsList []tools.Tool
	for t, err := range resp {
		if err != nil {
			return nil, err
		}

		name := t.Name
		if ts.name != "" {
			name = fmt.Sprintf("%s_%s", ts.name, name)
		}

		tool := tools.Tool{
			Name:         name,
			Description:  t.Description,
			Parameters:   t.InputSchema,
			OutputSchema: t.OutputSchema,
			Handler:      ts.callTool,
		}
		if t.Annotations != nil {
			tool.Annotations = tools.ToolAnnotations(*t.Annotations)
		}
		toolsList = append(toolsList, tool)

		slog.Debug("Added MCP tool", "tool", name)
	}

	slog.Debug("Listed MCP tools", "count", len(toolsList))
	return toolsList, nil
}

func (ts *Toolset) callTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	slog.Debug("Calling MCP tool", "tool", toolCall.Function.Name, "arguments", toolCall.Function.Arguments)

	toolCall.Function.Arguments = cmp.Or(toolCall.Function.Arguments, "{}")
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		slog.Error("Failed to parse tool arguments", "tool", toolCall.Function.Name, "error", err)
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	request := &mcp.CallToolParams{}
	request.Name = toolCall.Function.Name
	request.Arguments = args

	resp, err := ts.mcpClient.CallTool(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("CallTool canceled by context", "tool", toolCall.Function.Name)
			return nil, err
		}
		slog.Error("Failed to call MCP tool", "tool", toolCall.Function.Name, "error", err)
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}

	result := processMCPContent(resp)
	slog.Debug("MCP tool call completed", "tool", toolCall.Function.Name, "output_length", len(result.Output))
	slog.Debug(result.Output)
	return result, nil
}

func (ts *Toolset) Stop(ctx context.Context) error {
	slog.Debug("Stopping MCP toolset", "server", ts.logID)

	if err := ts.mcpClient.Close(context.WithoutCancel(ctx)); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		slog.Error("Failed to stop MCP toolset", "server", ts.logID, "error", err)
		return err
	}

	slog.Debug("Stopped MCP toolset successfully", "server", ts.logID)
	return nil
}

// isInitNotificationSendError returns true if initialization failed while sending the
// notifications/initialized message to the server.
func isInitNotificationSendError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// mcp-go client returns this error
	if strings.Contains(msg, "failed to send initialized notification") {
		return true
	}
	return false
}

func processMCPContent(toolResult *mcp.CallToolResult) *tools.ToolCallResult {
	finalContent := ""
	for _, resultContent := range toolResult.Content {
		if textContent, ok := resultContent.(*mcp.TextContent); ok {
			finalContent += textContent.Text
		}
	}

	// Handle an empty response. This can happen if the MCP tool does not return any content.
	finalContent = cmp.Or(finalContent, "no output")

	if toolResult.IsError {
		return tools.ResultError(finalContent)
	}
	return tools.ResultSuccess(finalContent)
}

func (ts *Toolset) SetElicitationHandler(handler tools.ElicitationHandler) {
	ts.mcpClient.SetElicitationHandler(handler)
}

func (ts *Toolset) SetOAuthSuccessHandler(handler func()) {
	ts.mcpClient.SetOAuthSuccessHandler(handler)
}

func (ts *Toolset) SetManagedOAuth(managed bool) {
	ts.mcpClient.SetManagedOAuth(managed)
}

// ListPrompts retrieves available prompts from the MCP server.
// Returns a slice of PromptInfo containing metadata about each available prompt
// including name, description, and argument specifications.
func (ts *Toolset) ListPrompts(ctx context.Context) ([]PromptInfo, error) {
	ts.mu.Lock()
	started := ts.started
	ts.mu.Unlock()
	if !started {
		return nil, errors.New("toolset not started")
	}

	slog.Debug("Listing MCP prompts")

	// Call the underlying MCP client to list prompts
	resp := ts.mcpClient.ListPrompts(ctx, &mcp.ListPromptsParams{})

	var promptsList []PromptInfo
	for prompt, err := range resp {
		if err != nil {
			slog.Warn("Error listing MCP prompt", "error", err)
			return promptsList, err
		}

		// Convert MCP prompt to our internal PromptInfo format
		promptInfo := PromptInfo{
			Name:        prompt.Name,
			Description: prompt.Description,
			Arguments:   make([]PromptArgument, 0),
		}

		// Convert arguments if they exist
		if prompt.Arguments != nil {
			for _, arg := range prompt.Arguments {
				promptArg := PromptArgument{
					Name:        arg.Name,
					Description: arg.Description,
					Required:    arg.Required,
				}
				promptInfo.Arguments = append(promptInfo.Arguments, promptArg)
			}
		}

		promptsList = append(promptsList, promptInfo)
		slog.Debug("Added MCP prompt", "prompt", prompt.Name, "args_count", len(promptInfo.Arguments))
	}

	slog.Debug("Listed MCP prompts", "count", len(promptsList))
	return promptsList, nil
}

// GetPrompt retrieves a specific prompt with provided arguments from the MCP server.
// This method executes the prompt and returns the result content.
func (ts *Toolset) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*mcp.GetPromptResult, error) {
	ts.mu.Lock()
	started := ts.started
	ts.mu.Unlock()
	if !started {
		return nil, errors.New("toolset not started")
	}

	slog.Debug("Getting MCP prompt", "prompt", name, "arguments", arguments)

	// Prepare the request parameters
	request := &mcp.GetPromptParams{
		Name:      name,
		Arguments: arguments,
	}

	// Call the underlying MCP client to get the prompt
	result, err := ts.mcpClient.GetPrompt(ctx, request)
	if err != nil {
		slog.Error("Failed to get MCP prompt", "prompt", name, "error", err)
		return nil, fmt.Errorf("failed to get prompt %s: %w", name, err)
	}

	slog.Debug("Retrieved MCP prompt", "prompt", name, "messages_count", len(result.Messages))
	return result, nil
}
