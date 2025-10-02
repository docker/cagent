package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/docker/cagent/pkg/tools"
)

type mcpClient interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	ListPrompts(ctx context.Context, request mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error)
	ListResources(ctx context.Context, request mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error)
	ListResourceTemplates(ctx context.Context, request mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	GetPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
	ReadResource(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
	Close() error
}

// Client implements an MCP client for interacting with MCP servers
type Client struct {
	client  mcpClient
	tools   []tools.Tool
	logType string
	logId   string
}

// Start initializes and starts the MCP server connection
func (c *Client) Start(ctx context.Context) error {
	slog.Debug("Starting MCP client", c.logType, c.logId)

	if err := c.client.Start(ctx); err != nil {
		return err
	}

	slog.Debug("Initializing MCP client", c.logType, c.logId)
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
	}

	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		_, err := c.client.Initialize(ctx, initRequest)
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
		slog.Debug("MCP initialize failed to send initialized notification; retrying", "id", c.logId, "attempt", attempt+1, "backoff_ms", backoff.Milliseconds())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("failed to initialize MCP client: %w", ctx.Err())
		}
	}

	slog.Debug("MCP client started and initialized successfully")
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

// Stop stops the MCP server
func (c *Client) Stop() error {
	slog.Debug("Stopping MCP client")
	err := c.client.Close()
	if err != nil {
		slog.Error("Failed to stop MCP client", "error", err)
		return err
	}
	slog.Debug("MCP client stopped successfully")
	return nil
}

// ListTools fetches available tools from the MCP server
func (c *Client) ListTools(ctx context.Context, toolFilter []string) ([]tools.Tool, error) {
	slog.Debug("Listing tools from MCP server", "toolFilter", toolFilter)

	resp, err := c.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("ListTools canceled by context")
			return nil, err
		}
		slog.Error("Failed to list tools from MCP server", "error", err)
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	slog.Debug("Received tools from MCP server", "count", len(resp.Tools))

	var toolsList []tools.Tool
	for i := range resp.Tools {
		t := &resp.Tools[i]
		// If toolFilter is not empty, only include tools that are in the filter
		if len(toolFilter) > 0 && !slices.Contains(toolFilter, t.Name) {
			slog.Debug("Filtering out tool", "tool", t.Name)
			continue
		}

		tool := tools.Tool{
			Handler: c.CallTool,
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

	c.tools = toolsList
	slog.Debug("Finished listing MCP tools", "filtered_count", len(toolsList))
	return toolsList, nil
}

// CallTool calls a tool on the MCP server
func (c *Client) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
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

	resp, err := c.client.CallTool(ctx, request)
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

// GetToolByName returns a tool by name
func (c *Client) GetToolByName(name string) (tools.Tool, error) {
	for _, tool := range c.tools {
		if tool.Function != nil && tool.Function.Name == name {
			return tool, nil
		}
	}
	return tools.Tool{}, fmt.Errorf("tool %s not found", name)
}

// GetServerInfo returns server identification information
func (c *Client) GetServerInfo() (serverURL, serverType string) {
	return c.logId, c.logType
}

// CallToolWithArgs is a convenience method to call a tool with arguments
func (c *Client) CallToolWithArgs(ctx context.Context, toolName string, args any) (*tools.ToolCallResult, error) {
	argsBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	toolCall := tools.ToolCall{
		Type: "function",
		Function: tools.FunctionCall{
			Name:      toolName,
			Arguments: string(argsBytes),
		},
	}

	return c.CallTool(ctx, toolCall)
}
