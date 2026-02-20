package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/coder/acp-go-sdk"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

type contextKey string

const sessionIDKey contextKey = "acp_session_id"

// withSessionID adds the session ID to the context
func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// getSessionID retrieves the session ID from the context
func getSessionID(ctx context.Context) (string, bool) {
	sid, ok := ctx.Value(sessionIDKey).(string)
	return sid, ok
}

// FilesystemToolset wraps a standard FilesystemTool and overrides read_file, write_file,
// and edit_file to use the ACP connection for file operations
type FilesystemToolset struct {
	*builtin.FilesystemTool
	agent      *Agent
	workingDir string
}

var _ tools.ToolSet = (*FilesystemToolset)(nil)

// NewFilesystemToolset creates a new ACP-specific filesystem toolset
func NewFilesystemToolset(agent *Agent, workingDir string, opts ...builtin.FileSystemOpt) *FilesystemToolset {
	return &FilesystemToolset{
		FilesystemTool: builtin.NewFilesystemTool(workingDir, opts...),
		agent:          agent,
		workingDir:     workingDir,
	}
}

// Tools returns the tool definitions with ACP-specific overrides
func (t *FilesystemToolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	baseTools, err := t.FilesystemTool.Tools(ctx)
	if err != nil {
		return nil, err
	}

	for i := range baseTools {
		switch baseTools[i].Name {
		case builtin.ToolNameReadFile:
			baseTools[i].Handler = t.handleReadFile
		case builtin.ToolNameWriteFile:
			baseTools[i].Handler = t.handleWriteFile
		case builtin.ToolNameEditFile:
			baseTools[i].Handler = t.handleEditFile
		}
	}

	return baseTools, nil
}

func (t *FilesystemToolset) handleReadFile(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args builtin.ReadFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	sessionID, ok := getSessionID(ctx)
	if !ok {
		return tools.ResultError("Error: session ID not found in context"), nil
	}

	resp, err := t.agent.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      filepath.Join(t.workingDir, args.Path),
	})
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error reading file: %s", err)), nil
	}

	return tools.ResultSuccess(resp.Content), nil
}

func (t *FilesystemToolset) handleWriteFile(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args builtin.WriteFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	sessionID, ok := getSessionID(ctx)
	if !ok {
		return tools.ResultError("Error: session ID not found in context"), nil
	}

	_, err := t.agent.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      filepath.Join(t.workingDir, args.Path),
		Content:   args.Content,
	})
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error writing file: %s", err)), nil
	}

	return tools.ResultSuccess("File written successfully"), nil
}

func (t *FilesystemToolset) handleEditFile(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args builtin.EditFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	sessionID, ok := getSessionID(ctx)
	if !ok {
		return tools.ResultError("Error: session ID not found in context"), nil
	}

	resp, err := t.agent.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      filepath.Join(t.workingDir, args.Path),
	})
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error reading file: %s", err)), nil
	}

	modifiedContent := resp.Content

	for i, edit := range args.Edits {
		if !strings.Contains(modifiedContent, edit.OldText) {
			return tools.ResultError(fmt.Sprintf("Edit %d failed: old text not found", i+1)), nil
		}
		modifiedContent = strings.Replace(modifiedContent, edit.OldText, edit.NewText, 1)
	}

	_, err = t.agent.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{
		SessionId: acp.SessionId(sessionID),
		Path:      filepath.Join(t.workingDir, args.Path),
		Content:   modifiedContent,
	})
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error writing file: %s", err)), nil
	}

	return tools.ResultSuccess("File edited successfully"), nil
}
