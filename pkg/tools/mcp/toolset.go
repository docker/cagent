package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/docker/cagent/pkg/oauth"
	"github.com/docker/cagent/pkg/tools"
)

type mcpClient interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
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
		mcpClient:  newStdioCmdClient(command, env, args),
		logType:    "command",
		logID:      command,
		toolFilter: toolFilter,
	}
}

// NewRemoteToolset creates a new MCP toolset from a remote MCP Server.
func NewRemoteToolset(url, transport string, headers map[string]string, toolFilter []string, redirectURI string) (*Toolset, error) {
	slog.Debug("Creating Remote MCP toolset", "url", url, "transport", transport, "headers", headers, "toolFilter", toolFilter, "redirectURI", redirectURI)

	tokenStore := GetTokenStore(url)

	mcpClient, err := newRemoteClient(url, transport, headers, redirectURI, tokenStore)
	if err != nil {
		slog.Error("Failed to create remote MCP client", "error", err)
		return nil, fmt.Errorf("failed to create remote mcp client: %w", err)
	}

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

	slog.Debug("Starting MCP toolset", "server", ts.logID)

	if err := ts.mcpClient.Start(ctx); err != nil {
		// When the MCP client is remote, Start() can fail due to OAuth authorization errors.
		// Provide more context to the caller.
		if client.IsOAuthAuthorizationRequiredError(err) {
			return &oauth.AuthorizationRequiredError{
				Err:        err,
				ServerURL:  ts.logID,
				ServerType: ts.logType,
			}
		}
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	slog.Debug("Initializing MCP client", ts.logType, ts.logID)
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
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
			if client.IsOAuthAuthorizationRequiredError(err) {
				return &oauth.AuthorizationRequiredError{
					Err:        err,
					ServerURL:  ts.logID,
					ServerType: ts.logType,
				}
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

	resp, err := ts.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("MCP tools listing canceled by context")
			return nil, err
		}

		slog.Error("Failed to list MCP tools", "error", err)
		return nil, err
	}

	slog.Debug("Received tools from MCP server", "count", len(resp.Tools))

	var toolsList []tools.Tool
	for i := range resp.Tools {
		t := &resp.Tools[i]
		// If toolFilter is not empty, only include tools that are in the filter
		if len(ts.toolFilter) > 0 && !slices.Contains(ts.toolFilter, t.Name) {
			slog.Debug("Filtering out tool", "tool", t.Name)
			continue
		}

		tool := tools.Tool{
			Handler: ts.callTool,
			Function: &tools.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters: tools.FunctionParameters{
					Type:       t.InputSchema.Type,
					Properties: t.InputSchema.Properties,
					Required:   t.InputSchema.Required,
				},
				Annotations: tools.ToolAnnotation{
					Title:           t.Annotations.Title,
					ReadOnlyHint:    t.Annotations.ReadOnlyHint,
					DestructiveHint: t.Annotations.DestructiveHint,
					IdempotentHint:  t.Annotations.IdempotentHint,
					OpenWorldHint:   t.Annotations.OpenWorldHint,
				},
				OutputSchema: tools.ToolOutputSchema{
					// See missing field in MCP Spec: https://github.com/modelcontextprotocol/modelcontextprotocol/issues/834
					// Items:      t.OutputSchema.Items,
					Items:      nil,
					Type:       t.OutputSchema.Type,
					Properties: t.OutputSchema.Properties,
					Required:   t.OutputSchema.Required,
				},
			},
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

	request := mcp.CallToolRequest{}
	request.Params.Name = toolCall.Function.Name
	request.Params.Arguments = args

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

func (ts *Toolset) Stop() error {
	slog.Debug("Stopping MCP toolset", "server", ts.logID)

	if err := ts.mcpClient.Close(); err != nil {
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
		if textContent, ok := resultContent.(mcp.TextContent); ok {
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
