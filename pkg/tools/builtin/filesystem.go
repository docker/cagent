package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/tools"
)

// PostEditConfig represents a post-edit command configuration
type PostEditConfig struct {
	Path string // File path pattern (glob-style)
	Cmd  string // Command to execute (with $path placeholder)
}

type FilesystemTool struct {
	elicitationTool

	allowedDirectories []string
	allowedTools       []string
	postEditCommands   []PostEditConfig
}

var _ tools.ToolSet = (*FilesystemTool)(nil)

type FileSystemOpt func(*FilesystemTool)

func WithAllowedTools(allowedTools []string) FileSystemOpt {
	return func(t *FilesystemTool) {
		t.allowedTools = allowedTools
	}
}

func WithPostEditCommands(postEditCommands []PostEditConfig) FileSystemOpt {
	return func(t *FilesystemTool) {
		t.postEditCommands = postEditCommands
	}
}

func NewFilesystemTool(allowedDirectories []string, opts ...FileSystemOpt) *FilesystemTool {
	t := &FilesystemTool{
		allowedDirectories: allowedDirectories,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *FilesystemTool) Instructions() string {
	return `## Filesystem Tool Instructions

This toolset provides comprehensive filesystem operations with built-in security restrictions.

### Security Model
- All operations are restricted to allowed directories only
- Use list_allowed_directories to see available paths
- Subdirectories within allowed directories are accessible
- Use add_allowed_directory to request access to new directories (requires user consent)

### Directory Access Management
- If you need access to a directory outside the allowed list, use add_allowed_directory
- This will request user consent before expanding filesystem access
- Always provide a clear reason when requesting new directory access

### Common Patterns
- Always check if directories exist before creating files
- Prefer read_multiple_files for batch operations
- Use search_files_content for finding specific code or text

### Performance Tips
- Use read_multiple_files instead of multiple read_file calls
- Use directory_tree with max_depth to limit large traversals
- Use appropriate exclude patterns in search operations`
}

type CreateDirectoryArgs struct {
	Path string `json:"path" jsonschema:"The directory path to create"`
}

type DirectoryTreeArgs struct {
	Path     string `json:"path" jsonschema:"The directory path to traverse"`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"Maximum depth to traverse (optional)"`
}

type GetFileInfoArgs struct {
	Path string `json:"path" jsonschema:"The file or directory path to inspect"`
}

type AddAllowedDirectoryArgs struct {
	Path      string `json:"path" jsonschema:"The directory path to add to allowed directories"`
	Reason    string `json:"reason" jsonschema:"Explanation of why this directory needs to be added"`
	Confirmed bool   `json:"confirmed,omitempty" jsonschema:"Set to true to confirm that you consent to adding this directory"`
}

type WriteFileArgs struct {
	Path    string `json:"path" jsonschema:"The file path to write"`
	Content string `json:"content" jsonschema:"The content to write to the file"`
}

type ReadMultipleFilesArgs struct {
	Paths []string `json:"paths" jsonschema:"Array of file paths to read"`
	JSON  bool     `json:"json,omitempty" jsonschema:"Whether to return the result as JSON"`
}

type SearchFilesArgs struct {
	Path            string   `json:"path" jsonschema:"The starting directory path"`
	Pattern         string   `json:"pattern" jsonschema:"The search pattern"`
	ExcludePatterns []string `json:"excludePatterns,omitempty" jsonschema:"Patterns to exclude from search"`
}

type SearchFilesContentArgs struct {
	Path            string   `json:"path" jsonschema:"The starting directory path"`
	Query           string   `json:"query" jsonschema:"The text or regex pattern to search for"`
	IsRegex         bool     `json:"is_regex,omitempty" jsonschema:"If true, treat query as regex; otherwise literal text"`
	ExcludePatterns []string `json:"excludePatterns,omitempty" jsonschema:"Patterns to exclude from search"`
}

type MoveFileArgs struct {
	Source      string `json:"source" jsonschema:"The source path"`
	Destination string `json:"destination" jsonschema:"The destination path"`
}

type ListDirectoryArgs struct {
	Path string `json:"path" jsonschema:"The directory path to list"`
}

type ReadFileArgs struct {
	Path string `json:"path" jsonschema:"The file path to read"`
}

type Edit struct {
	OldText string `json:"oldText" jsonschema:"The exact text to replace"`
	NewText string `json:"newText" jsonschema:"The replacement text"`
}

type EditFileArgs struct {
	Path  string `json:"path" jsonschema:"The file path to edit"`
	Edits []Edit `json:"edits" jsonschema:"Array of edit operations"`
}

func (t *FilesystemTool) Tools(context.Context) ([]tools.Tool, error) {
	tls := []tools.Tool{
		{
			Name:         "create_directory",
			Category:     "filesystem",
			Description:  "Create a new directory or ensure a directory exists. Can create multiple nested directories in one operation.",
			Parameters:   tools.MustSchemaFor[CreateDirectoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleCreateDirectory,
			Annotations: tools.ToolAnnotations{
				Title: "Create Directory",
			},
		},
		{
			Name:        "directory_tree",
			Category:    "filesystem",
			Description: "Get a recursive tree view of files and directories as a JSON structure.",
			Parameters:  tools.MustSchemaFor[DirectoryTreeArgs](),
			// Manually define the schema here because
			// tools.MustSchemaFor(reflect.TypeFor[*TreeNode]()) doesn't support recursive types.
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "The name of the node",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "The type of the node (file or directory)",
					},
					"children": map[string]any{
						"type":        "array",
						"description": "Optional list of child nodes",
						"items": map[string]any{
							"$ref": "#",
						},
					},
				},
				"required":             []string{"name", "type"},
				"additionalProperties": false,
			},
			Handler: t.handleDirectoryTree,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Directory Tree",
			},
		},
		{
			Name:         "edit_file",
			Category:     "filesystem",
			Description:  "Make line-based edits to a text file. Each edit replaces exact line sequences with new content.",
			Parameters:   tools.MustSchemaFor[EditFileArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleEditFile,
			Annotations: tools.ToolAnnotations{
				Title: "Edit File",
			},
		},
		{
			Name:         "get_file_info",
			Category:     "filesystem",
			Description:  "Retrieve detailed metadata about a file or directory.",
			Parameters:   tools.MustSchemaFor[GetFileInfoArgs](),
			OutputSchema: tools.MustSchemaFor[FileInfo](),
			Handler:      t.handleGetFileInfo,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Get File Info",
			},
		},
		{
			Name:         "list_allowed_directories",
			Category:     "filesystem",
			Description:  "Returns a list of directories that the server has permission to access. Don't call if you access only the current working directory. It's always allowed.",
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleListAllowedDirectories,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "List Allowed Directories",
			},
		},
		{
			Name:         "add_allowed_directory",
			Category:     "filesystem",
			Description:  "Request to add a new directory to the allowed directories list. This requires explicit user consent for security reasons.",
			Parameters:   tools.MustSchemaFor[AddAllowedDirectoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleAddAllowedDirectory,
			Annotations: tools.ToolAnnotations{
				Title: "Add Allowed Directory",
			},
		},
		{
			Name:         "list_directory",
			Category:     "filesystem",
			Description:  "Get a detailed listing of all files and directories in a specified path.",
			Parameters:   tools.MustSchemaFor[ListDirectoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleListDirectory,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "List Directory",
			},
		},
		{
			Name:         "list_directory_with_sizes",
			Category:     "filesystem",
			Description:  "Get a detailed listing of all files and directories in a specified path, including sizes.",
			Parameters:   tools.MustSchemaFor[ListDirectoryArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleListDirectoryWithSizes,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "List Directory With Sizes",
			},
		},
		{
			Name:         "move_file",
			Category:     "filesystem",
			Description:  "Move or rename files and directories.",
			Parameters:   tools.MustSchemaFor[MoveFileArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleMoveFile,
			Annotations: tools.ToolAnnotations{
				Title: "Move File",
			},
		},
		{
			Name:         "read_file",
			Category:     "filesystem",
			Description:  "Read the complete contents of a file from the file system.",
			Parameters:   tools.MustSchemaFor[ReadFileArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleReadFile,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Read File",
			},
		},
		{
			Name:        "read_multiple_files",
			Category:    "filesystem",
			Description: "Read the contents of multiple files simultaneously.",
			Parameters:  tools.MustSchemaFor[ReadMultipleFilesArgs](),
			// TODO(dga): depends on the json param
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleReadMultipleFiles,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Read Multiple Files",
			},
		},
		{
			Name:         "search_files",
			Category:     "filesystem",
			Description:  "Recursively search for files and directories matching a pattern. Prints the full paths of matching files and the total number of files found.",
			Parameters:   tools.MustSchemaFor[SearchFilesArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleSearchFiles,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Search Files",
			},
		},
		{
			Name:         "search_files_content",
			Category:     "filesystem",
			Description:  "Searches for text or regex patterns in the content of files matching a GLOB pattern.",
			Parameters:   tools.MustSchemaFor[SearchFilesContentArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleSearchFilesContent,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Search Files Content",
			},
		},
		{
			Name:         "write_file",
			Category:     "filesystem",
			Description:  "Create a new file or completely overwrite an existing file with new content.",
			Parameters:   tools.MustSchemaFor[WriteFileArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handleWriteFile,
			Annotations: tools.ToolAnnotations{
				Title: "Write File",
			},
		},
	}

	if len(t.allowedTools) == 0 {
		return tls, nil
	}

	var allowedTools []tools.Tool
	for _, tool := range tls {
		if slices.Contains(t.allowedTools, tool.Name) {
			allowedTools = append(allowedTools, tool)
		}
	}

	return allowedTools, nil
}

// executePostEditCommands executes any matching post-edit commands for the given file path
func (t *FilesystemTool) executePostEditCommands(ctx context.Context, filePath string) error {
	if len(t.postEditCommands) == 0 {
		return nil
	}

	for _, postEdit := range t.postEditCommands {
		matched, err := filepath.Match(postEdit.Path, filepath.Base(filePath))
		if err != nil {
			slog.WarnContext(ctx, "Invalid post-edit pattern", "pattern", postEdit.Path, "error", err)
			continue
		}
		if !matched {
			continue
		}

		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", postEdit.Cmd)
		cmd.Env = cmd.Environ()
		cmd.Env = append(cmd.Env, "path="+filePath)

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("post-edit command failed for %s: %w", filePath, err)
		}

	}
	return nil
}

// Security helper to check if path is allowed
func (t *FilesystemTool) isPathAllowed(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("unable to resolve absolute path: %w", err)
	}

	if len(t.allowedDirectories) == 0 {
		return fmt.Errorf("no allowed directories configured")
	}

	for _, allowedDir := range t.allowedDirectories {
		allowedAbs, err := filepath.Abs(allowedDir)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absPath, allowedAbs) {
			return nil
		}
	}

	return fmt.Errorf("path %s is not within allowed directories", path)
}

// Handler implementations

func (t *FilesystemTool) handleCreateDirectory(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args CreateDirectoryArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	if err := os.MkdirAll(args.Path, 0o755); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error creating directory: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: fmt.Sprintf("Directory created successfully: %s", args.Path)}, nil
}

func (t *FilesystemTool) handleDirectoryTree(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args DirectoryTreeArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	tree, err := t.buildDirectoryTree(args.Path, args.MaxDepth, 0)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error building directory tree: %s", err)}, nil
	}

	result, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error formatting tree: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: string(result)}, nil
}

type TreeNode struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []*TreeNode `json:"children,omitempty"`
}

func (t *FilesystemTool) buildDirectoryTree(path string, maxDepth, currentDepth int) (*TreeNode, error) {
	if maxDepth > 0 && currentDepth >= maxDepth {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	node := &TreeNode{
		Name: filepath.Base(path),
		Type: "file",
	}

	if info.IsDir() {
		node.Type = "directory"
		node.Children = []*TreeNode{}

		entries, err := os.ReadDir(path)
		if err != nil {
			return node, nil // Return partial result on error
		}

		for _, entry := range entries {
			childPath := filepath.Join(path, entry.Name())
			if err := t.isPathAllowed(childPath); err != nil {
				continue // Skip disallowed paths
			}

			childNode, err := t.buildDirectoryTree(childPath, maxDepth, currentDepth+1)
			if err != nil || childNode == nil {
				continue
			}
			node.Children = append(node.Children, childNode)
		}
	}

	return node, nil
}

func (t *FilesystemTool) handleEditFile(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args EditFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	content, err := os.ReadFile(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error reading file: %s", err)}, nil
	}

	originalContent := string(content)
	modifiedContent := originalContent

	var changes []string
	for i, edit := range args.Edits {
		if !strings.Contains(modifiedContent, edit.OldText) {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Edit %d failed: old text not found", i+1)}, nil
		}
		modifiedContent = strings.Replace(modifiedContent, edit.OldText, edit.NewText, 1)
		changes = append(changes, fmt.Sprintf("Edit %d: Replaced %d characters", i+1, len(edit.OldText)))
	}

	if err := os.WriteFile(args.Path, []byte(modifiedContent), 0o644); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error writing file: %s", err)}, nil
	}

	// Execute post-edit commands
	if err := t.executePostEditCommands(ctx, args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("File edited successfully but post-edit command failed: %s", err)}, nil
	}

	if len(changes) == 1 {
		return &tools.ToolCallResult{Output: fmt.Sprintf("File edited successfully. %s", strings.TrimPrefix(changes[0], "Edit 1: "))}, nil
	}

	return &tools.ToolCallResult{Output: fmt.Sprintf("File edited successfully. Changes:\n%s", strings.Join(changes, "\n"))}, nil
}

type FileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

func (t *FilesystemTool) handleGetFileInfo(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error getting file info: %s", err)}, nil
	}

	fileInfo := FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode().String(),
		ModTime: info.ModTime().Format(time.RFC3339),
		IsDir:   info.IsDir(),
	}

	result, err := json.MarshalIndent(fileInfo, "", "  ")
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error formatting file info: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: string(result)}, nil
}

func (t *FilesystemTool) handleListAllowedDirectories(context.Context, tools.ToolCall) (*tools.ToolCallResult, error) {
	result, err := json.MarshalIndent(t.allowedDirectories, "", "  ")
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error formatting directories: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: string(result)}, nil
}

func (t *FilesystemTool) handleAddAllowedDirectory(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args AddAllowedDirectoryArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	// Validate the path exists and is a directory
	absPath, err := filepath.Abs(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error resolving path: %s", err)}, nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error accessing path: %s", err)}, nil
	}

	if !info.IsDir() {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s is not a directory", absPath)}, nil
	}

	// Check if the directory is already allowed
	for _, allowedDir := range t.allowedDirectories {
		allowedAbs, err := filepath.Abs(allowedDir)
		if err != nil {
			continue
		}
		if allowedAbs == absPath {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Directory %s is already in allowed directories list", absPath)}, nil
		}
		// Check if the requested path is already covered by an existing allowed directory
		if strings.HasPrefix(absPath, allowedAbs) {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Directory %s is already accessible (covered by %s)", absPath, allowedAbs)}, nil
		}
	}

	// If not confirmed, show consent request
	if !args.Confirmed {
		consentMsg := fmt.Sprintf(`SECURITY CONSENT REQUEST

The agent is requesting permission to add a new directory to the allowed filesystem access list:

Path: %s
Reason: %s

This will grant the agent read/write access to this directory and all its subdirectories.

IMPORTANT: Only grant this permission if:
1. You trust this request and understand the security implications
2. The directory contains files the agent legitimately needs to access
3. The directory doesn't contain sensitive personal data or system files

To proceed, call this tool again with the same parameters but add "confirmed": true
To deny, do not call the tool again.

Current allowed directories:
%s`, absPath, args.Reason, strings.Join(t.allowedDirectories, "\n"))

		return &tools.ToolCallResult{Output: consentMsg}, nil
	}

	// User has confirmed, add the directory
	return t.addAllowedDirectory(absPath)
}

// addAllowedDirectory adds a directory to the allowed directories list
func (t *FilesystemTool) addAllowedDirectory(absPath string) (*tools.ToolCallResult, error) {
	// Add the directory to the allowed list
	t.allowedDirectories = append(t.allowedDirectories, absPath)

	successMsg := fmt.Sprintf(`Directory successfully added to allowed directories list.

Added: %s

The agent now has filesystem access to this directory and all its subdirectories.

Updated allowed directories:
%s`, absPath, strings.Join(t.allowedDirectories, "\n"))

	return &tools.ToolCallResult{Output: successMsg}, nil
}

func (t *FilesystemTool) handleListDirectory(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args ListDirectoryArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	entries, err := os.ReadDir(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error reading directory: %s", err)}, nil
	}

	var result strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			result.WriteString(fmt.Sprintf("DIR  %s\n", entry.Name()))
		} else {
			result.WriteString(fmt.Sprintf("FILE %s\n", entry.Name()))
		}
	}

	return &tools.ToolCallResult{Output: result.String()}, nil
}

func (t *FilesystemTool) handleListDirectoryWithSizes(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args ListDirectoryArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	entries, err := os.ReadDir(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error reading directory: %s", err)}, nil
	}

	var result strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if entry.IsDir() {
			result.WriteString(fmt.Sprintf("DIR  %s\n", entry.Name()))
		} else {
			result.WriteString(fmt.Sprintf("FILE %s (%d bytes)\n", entry.Name(), info.Size()))
		}
	}

	return &tools.ToolCallResult{Output: result.String()}, nil
}

func (t *FilesystemTool) handleMoveFile(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args MoveFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Source); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error (source): %s", err)}, nil
	}
	if err := t.isPathAllowed(args.Destination); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error (destination): %s", err)}, nil
	}

	if _, err := os.Stat(args.Destination); err == nil {
		return &tools.ToolCallResult{Output: "Error: destination already exists"}, nil
	}

	if err := os.Rename(args.Source, args.Destination); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error moving file: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: fmt.Sprintf("Successfully moved %s to %s", args.Source, args.Destination)}, nil
}

func (t *FilesystemTool) handleReadFile(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args ReadFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	content, err := os.ReadFile(args.Path)
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error reading file: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: string(content)}, nil
}

func (t *FilesystemTool) handleReadMultipleFiles(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args ReadMultipleFilesArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	type PathContent struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	var contents []PathContent

	for _, path := range args.Paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if err := t.isPathAllowed(path); err != nil {
			contents = append(contents, PathContent{
				Path:    path,
				Content: fmt.Sprintf("Error: %s", err),
			})
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			contents = append(contents, PathContent{
				Path:    path,
				Content: fmt.Sprintf("Error reading file: %s", err),
			})
			continue
		}

		contents = append(contents, PathContent{
			Path:    path,
			Content: string(content),
		})
	}

	if args.JSON {
		jsonResult, err := json.MarshalIndent(contents, "", "  ")
		if err != nil {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Error formatting JSON: %s", err)}, nil
		}

		return &tools.ToolCallResult{
			Output: string(jsonResult),
		}, nil
	}

	var result strings.Builder
	for _, content := range contents {
		result.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", content.Path, content.Content))
	}

	return &tools.ToolCallResult{
		Output: result.String(),
	}, nil
}

func (t *FilesystemTool) handleSearchFiles(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args SearchFilesArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	var matches []string
	pattern := strings.ToLower(args.Pattern)

	err := filepath.WalkDir(args.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors and continue
		}

		if err := t.isPathAllowed(path); err != nil {
			return nil // Skip disallowed paths
		}

		// Check exclude patterns against relative path from search root
		relPath, err := filepath.Rel(args.Path, path)
		if err != nil {
			return nil
		}

		for _, exclude := range args.ExcludePatterns {
			if matchExcludePattern(exclude, relPath) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if match(pattern, filepath.Base(path)) {
			matches = append(matches, path)
		}

		return nil
	})
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error searching files: %s", err)}, nil
	}

	if len(matches) == 0 {
		return &tools.ToolCallResult{Output: "No files found"}, nil
	}

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("%d files found:\n%s", len(matches), strings.Join(matches, "\n")),
	}, nil
}

func (t *FilesystemTool) handleSearchFilesContent(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args SearchFilesContentArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	var regex *regexp.Regexp
	if args.IsRegex {
		var err error
		regex, err = regexp.Compile(args.Query)
		if err != nil {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Invalid regex pattern: %s", err)}, nil
		}
	}

	var results []string

	err := filepath.WalkDir(args.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if err := t.isPathAllowed(path); err != nil {
			return nil
		}

		// Check exclude patterns against relative path from search root
		relPath, err := filepath.Rel(args.Path, path)
		if err != nil {
			return nil
		}

		for _, exclude := range args.ExcludePatterns {
			if matchExcludePattern(exclude, relPath) {
				if d.IsDir() {
					return fs.SkipDir // Skip entire directory
				}
				return nil // Skip this file
			}
		}

		// Only process files, not directories
		if d.IsDir() {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			var matched bool
			var matchStart, matchEnd int

			if args.IsRegex {
				if loc := regex.FindStringIndex(line); loc != nil {
					matched = true
					matchStart, matchEnd = loc[0], loc[1]
				}
			} else {
				if idx := strings.Index(line, args.Query); idx != -1 {
					matched = true
					matchStart, matchEnd = idx, idx+len(args.Query)
				}
			}

			if matched {
				preview := line
				if len(preview) > 100 {
					start := max(matchStart-20, 0)
					end := matchEnd + 20
					end = min(end, len(preview))
					preview = preview[start:end]
				}

				result := fmt.Sprintf("%s:%d:%d: %s", path, lineNum+1, matchStart+1, preview)
				results = append(results, result)
			}
		}

		return nil
	})
	if err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error searching file contents: %s", err)}, nil
	}

	if len(results) == 0 {
		return &tools.ToolCallResult{Output: "No results found"}, nil
	}

	return &tools.ToolCallResult{Output: strings.Join(results, "\n")}, nil
}

func (t *FilesystemTool) handleWriteFile(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args WriteFileArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if err := t.isPathAllowed(args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error: %s", err)}, nil
	}

	if err := os.WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("Error writing file: %s", err)}, nil
	}

	// Execute post-edit commands
	if err := t.executePostEditCommands(ctx, args.Path); err != nil {
		return &tools.ToolCallResult{Output: fmt.Sprintf("File written successfully but post-edit command failed: %s", err)}, nil
	}

	return &tools.ToolCallResult{Output: fmt.Sprintf("File written successfully: %s (%d bytes)", args.Path, len(args.Content))}, nil
}

func (t *FilesystemTool) Start(context.Context) error {
	return nil
}

func (t *FilesystemTool) Stop() error {
	return nil
}

// matchExcludePattern checks if a path should be excluded based on the exclude pattern
// It supports glob patterns and directory wildcards like .git/*
func matchExcludePattern(pattern, relPath string) bool {
	// Normalize path separators to forward slashes for consistent matching
	normalizedPath := filepath.ToSlash(relPath)
	normalizedPattern := filepath.ToSlash(pattern)

	// Handle directory patterns ending with /*
	if strings.HasSuffix(normalizedPattern, "/*") {
		dirPattern := strings.TrimSuffix(normalizedPattern, "/*")
		// Check if path starts with the directory pattern
		if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
			return true
		}
	}

	// Try glob pattern matching on the full relative path
	matched, _ := filepath.Match(normalizedPattern, normalizedPath)
	if matched {
		return true
	}

	// Try glob pattern matching on just the base name for backwards compatibility
	matched, _ = filepath.Match(normalizedPattern, filepath.Base(normalizedPath))
	if matched {
		return true
	}

	// Check if pattern matches any parent directory path
	pathParts := strings.Split(normalizedPath, "/")
	for i := range pathParts {
		subPath := strings.Join(pathParts[:i+1], "/")
		matched, _ := filepath.Match(normalizedPattern, subPath)
		if matched {
			return true
		}
	}

	return false
}

func match(pattern, name string) bool {
	matched, _ := filepath.Match(pattern, name)
	if matched {
		return true
	}

	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}
