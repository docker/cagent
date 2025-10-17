package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/tools"
)

type mcpClient interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, request *mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error]
	CallTool(ctx context.Context, request *mcp.CallToolParams) (*mcp.CallToolResult, error)
	Close(ctx context.Context) error
}

// Toolset represents a set of MCP tools
type Toolset struct {
	mcpClient  mcpClient
	logType    string
	logID      string
	toolFilter []string

	instructions string
	started      atomic.Bool
}

var _ tools.ToolSet = (*Toolset)(nil)

// NewToolsetCommand creates a new MCP toolset from a command.
func NewToolsetCommand(command string, args, env, toolFilter []string) *Toolset {
	slog.Debug("Creating Stdio MCP toolset", "command", command, "args", args, "toolFilter", toolFilter)

	return &Toolset{
		mcpClient:  newStdioCmdClient(command, args, env),
		logType:    "command",
		logID:      command,
		toolFilter: toolFilter,
	}
}

// NewRemoteToolset creates a new MCP toolset from a remote MCP Server.
func NewRemoteToolset(url, transport string, headers map[string]string, toolFilter []string, redirectURI string) (*Toolset, error) {
	slog.Debug("Creating Remote MCP toolset", "url", url, "transport", transport, "headers", headers, "toolFilter", toolFilter, "redirectURI", redirectURI)

	tokenStore := NewInMemoryTokenStore()

	// Create without elicitation handler initially - it will be set later by runtime
	mcpClient := newRemoteClient(url, transport, headers, redirectURI, tokenStore)

	return &Toolset{
		mcpClient:  mcpClient,
		logType:    "remote",
		logID:      url,
		toolFilter: toolFilter,
	}, nil
}

func (ts *Toolset) Start(ctx context.Context) error {
	if ts.started.Load() {
		return errors.New("toolset already started")
	}

	// The MCP toolset connection needs to persist beyond the initial HTTP request that triggered its creation.
	// When OAuth succeeds, subsequent agent requests should reuse the already-authenticated MCP connection.
	// But if the connection's underlying context is tied to the first HTTP request, it gets cancelled when that request
	// completes, killing the connection even though OAuth succeeded.
	// This is critical for OAuth flows where the toolset connection needs to remain alive after the initial HTTP request completes.
	ctx = context.WithoutCancel(ctx)

	slog.Debug("Starting MCP toolset", "server", ts.logID)

	if err := ts.mcpClient.Start(ctx); err != nil {
		return err
	}

	initRequest := &mcp.InitializeRequest{
		Params: &mcp.InitializeParams{
			ClientInfo: &mcp.Implementation{
				Name:    "cagent",
				Version: "1.0.0",
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
	ts.started.Store(true)
	return nil
}

func (ts *Toolset) Instructions() string {
	if !ts.started.Load() {
		panic("toolset not started")
	}
	return ts.instructions
}

func (ts *Toolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	if !ts.started.Load() {
		return nil, errors.New("toolset not started")
	}

	slog.Debug("Listing MCP tools", "toolFilter", ts.toolFilter)

	resp := ts.mcpClient.ListTools(ctx, &mcp.ListToolsParams{})

	var toolsList []tools.Tool
	for t, err := range resp {
		if err != nil {
			return nil, err
		}

		// If toolFilter is not empty, only include tools that are in the filter
		if len(ts.toolFilter) > 0 && !slices.Contains(ts.toolFilter, t.Name) {
			slog.Debug("Filtering out tool", "tool", t.Name)
			continue
		}

		tool := tools.Tool{
			Name:         t.Name,
			Description:  t.Description,
			Parameters:   t.InputSchema,
			OutputSchema: t.OutputSchema,
			Handler:      ts.callTool,
		}
		if t.Annotations != nil {
			tool.Annotations = tools.ToolAnnotations(*t.Annotations)
		}
		toolsList = append(toolsList, tool)

		slog.Debug("Added MCP tool", "tool", t.Name)
	}

	slog.Debug("Listed MCP tools", "count", len(toolsList))
	return toolsList, nil
}

func (ts *Toolset) callTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	slog.Debug("Calling MCP tool", "tool", toolCall.Function.Name, "arguments", toolCall.Function.Arguments)

	if toolCall.Function.Arguments == "" {
		toolCall.Function.Arguments = "{}"
	}
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
	if finalContent == "" {
		finalContent = "no output"
	}

	return &tools.ToolCallResult{
		Output: finalContent,
	}
}

// SetElicitationHandler sets the elicitation handler for remote MCP clients
// This allows the runtime to provide a handler that propagates elicitation requests
func (ts *Toolset) SetElicitationHandler(handler tools.ElicitationHandler) {
	if remoteClient, ok := ts.mcpClient.(*remoteMCPClient); ok {
		remoteClient.mu.Lock()
		remoteClient.elicitationHandler = handler
		remoteClient.mu.Unlock()
	}
}

func (ts *Toolset) SetOAuthSuccessHandler(handler func()) {
	if remoteClient, ok := ts.mcpClient.(*remoteMCPClient); ok {
		remoteClient.mu.Lock()
		remoteClient.oauthSuccessHandler = handler
		remoteClient.mu.Unlock()
	}
}
