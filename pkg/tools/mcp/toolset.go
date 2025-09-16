package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/tools"
)

// Toolset represents a set of MCP tools
type Toolset struct {
	c          *Client
	toolFilter []string
}

// Make sure the MCP Toolset always implements _our_ ToolSet interface
var _ tools.ToolSet = (*Toolset)(nil)

// NewToolsetCommand creates a new MCP toolset from a command.
func NewToolsetCommand(command string, args, env, toolFilter []string) *Toolset {
	slog.Debug("Creating MCP toolset", "command", command, "args", args, "toolFilter", toolFilter)

	return &Toolset{
		c:          NewStdioClient(command, args, env),
		toolFilter: toolFilter,
	}
}

// NewToolsetRemote creates a new MCP toolset from a remote MCP Server.
func NewToolsetRemote(url, transport string, headers map[string]string, toolFilter []string, redirectURI string) (*Toolset, error) {
	slog.Debug("Creating Remote MCP toolset", "url", url, "transport", transport, "headers", headers, "toolFilter", toolFilter, "redirectURI", redirectURI)

	tokenStore := GetTokenStore(url)
	mcpc, err := NewRemoteClient(url, transport, headers, redirectURI, tokenStore)
	if err != nil {
		slog.Error("Failed to create remote MCP client", "error", err)
		return nil, fmt.Errorf("failed to create remote mcp client: %w", err)
	}

	return &Toolset{
		c:          mcpc,
		toolFilter: toolFilter,
	}, nil
}

// Instructions returns the toolset instructions
func (t *Toolset) Instructions() string {
	return ""
}

// GetServerInfo returns server identification information
func (t *Toolset) GetServerInfo() (serverURL, serverType string) {
	return t.c.GetServerInfo()
}

// Tools returns the available tools
func (t *Toolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	slog.Debug("Listing MCP tools", "toolFilter", t.toolFilter)
	mcpTools, err := t.c.ListTools(ctx, t.toolFilter)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			// Log at debug level on cancellation
			slog.Debug("MCP tools listing canceled by context")
			return nil, err
		}
		slog.Error("Failed to list MCP tools", "error", err)
		return nil, err
	}
	slog.Debug("Listed MCP tools", "count", len(mcpTools))
	return mcpTools, nil
}

// Start starts the toolset
func (t *Toolset) Start(ctx context.Context) error {
	slog.Debug("Starting MCP toolset")
	err := t.c.Start(ctx)
	if err != nil {
		slog.Error("Failed to start MCP toolset", "error", err)
		return err
	}
	slog.Debug("Started MCP toolset successfully")
	return nil
}

// Stop stops the toolset
func (t *Toolset) Stop() error {
	slog.Debug("Stopping MCP toolset")
	err := t.c.Stop()
	if err != nil {
		slog.Error("Failed to stop MCP toolset", "error", err)
		return err
	}
	slog.Debug("Stopped MCP toolset successfully")
	return nil
}
