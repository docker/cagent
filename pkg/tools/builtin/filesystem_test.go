package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewFilesystemTool(t *testing.T) {
	allowedDirs := []string{"/tmp", "/var/tmp"}
	tool := NewFilesystemTool(allowedDirs)

	assert.NotNil(t, tool)
	assert.Equal(t, allowedDirs, tool.allowedDirectories)
}

func TestFilesystemTool_Instructions(t *testing.T) {
	tool := NewFilesystemTool([]string{"/tmp"})
	instructions := tool.Instructions()

	assert.Contains(t, instructions, "Filesystem Tool Instructions")
	assert.Contains(t, instructions, "Security Model")
	assert.Contains(t, instructions, "allowed directories")
}

func TestFilesystemTool_Tools(t *testing.T) {
	tool := NewFilesystemTool([]string{"/tmp"})
	allTools, err := tool.Tools(t.Context())

	require.NoError(t, err)
	assert.Len(t, allTools, 14)

	expectedTools := []string{
		"add_allowed_directory",
		"create_directory",
		"directory_tree",
		"edit_file",
		"get_file_info",
		"list_allowed_directories",
		"list_directory",
		"list_directory_with_sizes",
		"move_file",
		"read_file",
		"read_multiple_files",
		"search_files",
		"search_files_content",
		"write_file",
	}

	var toolNames []string
	for _, tool := range allTools {
		toolNames = append(toolNames, tool.Name)
		assert.NotNil(t, tool.Handler)
		assert.Equal(t, "filesystem", tool.Category)
	}

	for _, expected := range expectedTools {
		assert.Contains(t, toolNames, expected)
	}
}

func TestFilesystemTool_DisplayNames(t *testing.T) {
	tool := NewFilesystemTool([]string{"/tmp"})

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestFilesystemTool_IsPathAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	allowedPath := filepath.Join(tmpDir, "subdir", "file.txt")
	err := tool.isPathAllowed(allowedPath)
	require.NoError(t, err)

	disallowedPath := "/etc/passwd"
	err = tool.isPathAllowed(disallowedPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not within allowed directories")
}

func TestFilesystemTool_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	handler := getToolHandler(t, tool, "create_directory")

	newDir := filepath.Join(tmpDir, "test", "nested", "dir")
	args := map[string]any{"path": newDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Directory created successfully")
	assert.DirExists(t, newDir)

	disallowedDir := "/etc/test"
	args = map[string]any{"path": disallowedDir}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error:")
	assert.Contains(t, result.Output, "not within allowed directories")
}

func TestFilesystemTool_WriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	handler := getToolHandler(t, tool, "write_file")

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	args := map[string]any{
		"path":    testFile,
		"content": content,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "File written successfully")
	assert.FileExists(t, testFile)

	writtenContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(writtenContent))

	disallowedFile := "/etc/test.txt"
	args = map[string]any{
		"path":    disallowedFile,
		"content": "test",
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error:")
	assert.Contains(t, result.Output, "not within allowed directories")
}

func TestFilesystemTool_WriteFile_NestedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	handler := getToolHandler(t, tool, "write_file")

	// Write to a nested path that doesn't exist
	nestedFile := filepath.Join(tmpDir, "a", "b", "c", "test.txt")
	content := "Hello, nested world!"

	args := map[string]any{
		"path":    nestedFile,
		"content": content,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "File written successfully")
	assert.FileExists(t, nestedFile)

	writtenContent, err := os.ReadFile(nestedFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(writtenContent))

	assert.DirExists(t, filepath.Join(tmpDir, "a"))
	assert.DirExists(t, filepath.Join(tmpDir, "a", "b"))
	assert.DirExists(t, filepath.Join(tmpDir, "a", "b", "c"))
}

func TestFilesystemTool_ReadFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "read_file")

	args := map[string]any{"path": testFile}
	result := callHandler(t, handler, args)

	assert.Equal(t, content, result.Output)

	nonExistentFile := filepath.Join(tmpDir, "nonexistent.txt")
	args = map[string]any{"path": nonExistentFile}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error reading file")

	disallowedFile := "/etc/passwd"
	args = map[string]any{"path": disallowedFile}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error:")
	assert.Contains(t, result.Output, "not within allowed directories")
}

func TestFilesystemTool_ReadMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	content1 := "Content 1"
	content2 := "Content 2"

	require.NoError(t, os.WriteFile(file1, []byte(content1), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte(content2), 0o644))

	handler := getToolHandler(t, tool, "read_multiple_files")

	args := map[string]any{"paths": []string{file1, file2}}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "=== "+file1+" ===")
	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "=== "+file2+" ===")
	assert.Contains(t, result.Output, content2)

	nonExistentFile := filepath.Join(tmpDir, "nonexistent.txt")
	args = map[string]any{"paths": []string{file1, nonExistentFile}}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "Error reading file")
}

func TestFilesystemTool_ListDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")

	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))
	require.NoError(t, os.Mkdir(testDir, 0o755))

	handler := getToolHandler(t, tool, "list_directory")

	args := map[string]any{"path": tmpDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "FILE test.txt")
	assert.Contains(t, result.Output, "DIR  testdir")

	nonExistentDir := filepath.Join(tmpDir, "nonexistent")
	args = map[string]any{"path": nonExistentDir}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error reading directory")
}

func TestFilesystemTool_ListDirectoryWithSizes(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")
	content := "Hello World"

	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))
	require.NoError(t, os.Mkdir(testDir, 0o755))

	handler := getToolHandler(t, tool, "list_directory_with_sizes")

	args := map[string]any{"path": tmpDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "FILE test.txt (11 bytes)")
	assert.Contains(t, result.Output, "DIR  testdir")
}

func TestFilesystemTool_GetFileInfo(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "get_file_info")

	args := map[string]any{"path": testFile}
	result := callHandler(t, handler, args)

	var fileInfo map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Output), &fileInfo))

	assert.Equal(t, "test.txt", fileInfo["name"])
	assert.InDelta(t, len(content), fileInfo["size"], 0.0)
	assert.Equal(t, false, fileInfo["isDir"])

	args = map[string]any{"path": tmpDir}
	result = callHandler(t, handler, args)

	require.NoError(t, json.Unmarshal([]byte(result.Output), &fileInfo))
	assert.Equal(t, true, fileInfo["isDir"])
}

func TestFilesystemTool_MoveFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	sourceFile := filepath.Join(tmpDir, "source.txt")
	destFile := filepath.Join(tmpDir, "dest.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(sourceFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "move_file")

	args := map[string]any{
		"source":      sourceFile,
		"destination": destFile,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Successfully moved")
	assert.NoFileExists(t, sourceFile)
	assert.FileExists(t, destFile)

	movedContent, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(movedContent))

	anotherFile := filepath.Join(tmpDir, "another.txt")
	require.NoError(t, os.WriteFile(anotherFile, []byte("test"), 0o644))

	args = map[string]any{
		"source":      destFile,
		"destination": anotherFile,
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "destination already exists")
}

func TestFilesystemTool_EditFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "Hello World\nThis is a test\nGoodbye World"
	require.NoError(t, os.WriteFile(testFile, []byte(originalContent), 0o644))

	handler := getToolHandler(t, tool, "edit_file")

	args := map[string]any{
		"path": testFile,
		"edits": []map[string]any{
			{
				"oldText": "Hello World",
				"newText": "Hi Universe",
			},
			{
				"oldText": "Goodbye World",
				"newText": "See you later",
			},
		},
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "File edited successfully")

	editedContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	expected := "Hi Universe\nThis is a test\nSee you later"
	assert.Equal(t, expected, string(editedContent))

	args = map[string]any{
		"path": testFile,
		"edits": []map[string]any{
			{
				"oldText": "Non-existent text",
				"newText": "Replacement",
			},
		},
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "old text not found")
}

func TestFilesystemTool_SearchFiles(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.log"), []byte("log"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte("data"), 0o644))

	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "test_sub.txt"), []byte("sub"), 0o644))

	tool := NewFilesystemTool([]string{tmpDir})
	handler := getToolHandler(t, tool, "search_files")

	args := map[string]any{
		"path":    tmpDir,
		"pattern": "asdf",
	}
	result := callHandler(t, handler, args)

	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Len(t, lines, 1) // Should find test.txt, test.log, and test_sub.txt
	assert.Contains(t, lines, "No files found")

	args = map[string]any{
		"path":    tmpDir,
		"pattern": "test",
	}
	result = callHandler(t, handler, args)

	lines = strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Contains(t, result.Output, "3 files found:\n")
	assert.Len(t, lines, 3+1) // Should find test.txt, test.log, and test_sub.txt

	args = map[string]any{
		"path":            tmpDir,
		"pattern":         "test",
		"excludePatterns": []string{"*.log"},
	}
	result = callHandler(t, handler, args)

	assert.NotContains(t, result.Output, "test.log")
	assert.Contains(t, result.Output, "test.txt")
}

func TestFilesystemTool_SearchFilesContent(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	file1Content := "This is a test file\nwith multiple lines\ncontaining test data"
	file2Content := "Another file\nwith different content\nno matching terms here"
	file3Content := "Final file\nhas test in it\nand more test content"

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte(file1Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte(file2Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file3.txt"), []byte(file3Content), 0o644))

	handler := getToolHandler(t, tool, "search_files_content")

	args := map[string]any{
		"path":     tmpDir,
		"pattern":  "*.txt",
		"query":    "test",
		"is_regex": false,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "file1.txt:1:")
	assert.Contains(t, result.Output, "file1.txt:3:")
	assert.Contains(t, result.Output, "file3.txt:2:")
	assert.Contains(t, result.Output, "file3.txt:3:")
	assert.NotContains(t, result.Output, "file2.txt")

	args = map[string]any{
		"path":     tmpDir,
		"pattern":  "*.txt",
		"query":    "test.*data",
		"is_regex": true,
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "file1.txt:3:")

	args = map[string]any{
		"path":     tmpDir,
		"pattern":  "*.txt",
		"query":    "[invalid",
		"is_regex": true,
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Invalid regex pattern")
}

func TestFilesystemTool_SearchFiles_RecursivePattern(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "first.txt"), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ignored"), []byte("ignored"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "second.txt"), []byte("second"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "third.txt"), []byte("third"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "ignored"), []byte("ignored"), 0o644))

	tool := NewFilesystemTool([]string{tmpDir})
	handler := getToolHandler(t, tool, "search_files")

	args := map[string]any{
		"path":    tmpDir,
		"pattern": "*.txt",
	}
	result := callHandler(t, handler, args)

	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Contains(t, result.Output, "3 files found:\n")
	assert.Len(t, lines, 3+1) // Should find first.txt, second.txt, and third.txt
}

func TestFilesystemTool_ListAllowedDirectories(t *testing.T) {
	allowedDirs := []string{"/tmp", "/var/tmp", "/home/user"}
	tool := NewFilesystemTool(allowedDirs)

	handler := getToolHandler(t, tool, "list_allowed_directories")

	args := map[string]any{}
	result := callHandler(t, handler, args)

	var dirs []string
	require.NoError(t, json.Unmarshal([]byte(result.Output), &dirs))

	assert.Equal(t, allowedDirs, dirs)
}

func TestFilesystemTool_InvalidArguments(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	handler := getToolHandler(t, tool, "write_file")

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Name:      "write_file",
			Arguments: "{invalid json",
		},
	}

	result, err := handler(t.Context(), toolCall)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestFilesystemTool_StartStop(t *testing.T) {
	tool := NewFilesystemTool([]string{"/tmp"})

	err := tool.Start(t.Context())
	require.NoError(t, err)

	err = tool.Stop(t.Context())
	require.NoError(t, err)
}

func TestFilesystemTool_PostEditCommands(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.go")
	testContent := `package main

func main() {
	fmt.Println("hello")
}`

	postEditConfigs := []PostEditConfig{
		{
			Path: "*.go",
			Cmd:  "touch $path.formatted",
		},
	}
	tool := NewFilesystemTool([]string{tmpDir}, WithPostEditCommands(postEditConfigs))

	formattedFile := testFile + ".formatted"
	t.Run("write_file", func(t *testing.T) {
		handler := getToolHandler(t, tool, "write_file")

		// Use proper JSON marshaling for the arguments
		args := WriteFileArgs{
			Path:    testFile,
			Content: testContent,
		}
		argsBytes, err := json.Marshal(args)
		require.NoError(t, err)

		toolCall := tools.ToolCall{
			Function: tools.FunctionCall{
				Arguments: string(argsBytes),
			},
		}

		result, err := handler(t.Context(), toolCall)
		require.NoError(t, err)
		assert.Contains(t, result.Output, "File written successfully")

		_, err = os.Stat(formattedFile)
		require.NoError(t, err, "Post-edit command should have created formatted file")
		require.NoError(t, os.Remove(formattedFile))
	})

	t.Run("edit_file", func(t *testing.T) {
		editHandler := getToolHandler(t, tool, "edit_file")

		editArgs := EditFileArgs{
			Path: testFile,
			Edits: []Edit{{
				OldText: "fmt.Println",
				NewText: "fmt.Printf",
			}},
		}
		editArgsBytes, err := json.Marshal(editArgs)
		require.NoError(t, err)

		editCall := tools.ToolCall{
			Function: tools.FunctionCall{
				Arguments: string(editArgsBytes),
			},
		}

		editResult, err := editHandler(t.Context(), editCall)
		require.NoError(t, err)
		assert.Contains(t, editResult.Output, "File edited successfully")

		_, err = os.Stat(formattedFile)
		require.NoError(t, err, "Post-edit command should have run after edit")
	})
}

// Helper functions

func getToolHandler(t *testing.T, tool *FilesystemTool, toolName string) tools.ToolHandler {
	t.Helper()
	tls, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tl := range tls {
		if tl.Name == toolName {
			return tl.Handler
		}
	}

	t.Fatalf("Tool %s not found", toolName)
	return nil
}

func callHandler(t *testing.T, handler tools.ToolHandler, args any) *tools.ToolCallResult {
	t.Helper()
	argsBytes, err := json.Marshal(args)
	require.NoError(t, err)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsBytes),
		},
	}

	result, err := handler(t.Context(), toolCall)
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

func TestFilesystemTool_AddAllowedDirectory(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	tool := NewFilesystemTool([]string{tmpDir1})
	assert.Len(t, tool.allowedDirectories, 1)
	handler := getToolHandler(t, tool, "add_allowed_directory")

	t.Run("attempt to add already allowed directory", func(t *testing.T) {
		args := AddAllowedDirectoryArgs{
			Path: tmpDir1,
		}
		result := callHandler(t, handler, args)

		// Should return already allowed message
		assert.Contains(t, result.Output, "already in allowed directories")
		assert.Contains(t, result.Output, tmpDir1)

		// Should not add duplicate
		assert.Len(t, tool.allowedDirectories, 1)
	})

	t.Run("attempt to add subdirectory of allowed directory", func(t *testing.T) {
		subDir := filepath.Join(tmpDir1, "subdir")
		err := os.MkdirAll(subDir, 0o755)
		require.NoError(t, err)

		args := AddAllowedDirectoryArgs{
			Path: subDir,
		}
		result := callHandler(t, handler, args)

		// Should return already accessible message
		assert.Contains(t, result.Output, "already accessible")
		assert.Contains(t, result.Output, subDir)
		assert.Contains(t, result.Output, tmpDir1)

		// Should not add subdirectory
		assert.Len(t, tool.allowedDirectories, 1)
	})

	t.Run("attempt to add non-existent directory", func(t *testing.T) {
		nonExistent := "/path/that/does/not/exist"
		args := AddAllowedDirectoryArgs{
			Path: nonExistent,
		}
		result := callHandler(t, handler, args)

		// Should return error message
		assert.Contains(t, result.Output, "Error accessing path")

		// Should not add non-existent directory
		assert.Len(t, tool.allowedDirectories, 1)
	})

	t.Run("attempt to add file instead of directory", func(t *testing.T) {
		tempFile := filepath.Join(tmpDir2, "testfile.txt")
		err := os.WriteFile(tempFile, []byte("test"), 0o644)
		require.NoError(t, err)

		args := AddAllowedDirectoryArgs{
			Path: tempFile,
		}
		result := callHandler(t, handler, args)

		// Should return error message
		assert.Contains(t, result.Output, "is not a directory")

		// Should not add file
		assert.Len(t, tool.allowedDirectories, 1)
	})
}

func TestMatchExcludePattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		relPath  string
		expected bool
	}{
		// Directory wildcard patterns
		{
			name:     "matches directory with wildcard",
			pattern:  ".git/*",
			relPath:  ".git/config",
			expected: true,
		},
		{
			name:     "matches directory itself with wildcard",
			pattern:  ".git/*",
			relPath:  ".git",
			expected: true,
		},
		{
			name:     "matches nested file with directory wildcard",
			pattern:  ".git/*",
			relPath:  ".git/hooks/pre-commit",
			expected: true,
		},
		{
			name:     "does not match different directory",
			pattern:  ".git/*",
			relPath:  "src/main.go",
			expected: false,
		},
		// Glob patterns on full path
		{
			name:     "matches full path glob",
			pattern:  "*.log",
			relPath:  "debug.log",
			expected: true,
		},
		{
			name:     "matches nested file glob",
			pattern:  "*.log",
			relPath:  "logs/debug.log",
			expected: true,
		},
		{
			name:     "does not match different extension",
			pattern:  "*.log",
			relPath:  "main.go",
			expected: false,
		},
		// Base name matching for backwards compatibility
		{
			name:     "matches base name glob",
			pattern:  "*.tmp",
			relPath:  "cache/temp.tmp",
			expected: true,
		},
		{
			name:     "matches base name exact",
			pattern:  "README.md",
			relPath:  "docs/README.md",
			expected: true,
		},
		// Parent directory matching
		{
			name:     "matches parent directory",
			pattern:  "node_modules",
			relPath:  "node_modules/package/file.js",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := matchExcludePattern(tc.pattern, tc.relPath)
			assert.Equal(t, tc.expected, result, "Pattern: %s, Path: %s, IsDir: %v", tc.pattern, tc.relPath)
		})
	}
}

func TestFilesystemTool_OutputSchema(t *testing.T) {
	tool := NewFilesystemTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestFilesystemTool_ParametersAreObjects(t *testing.T) {
	tool := NewFilesystemTool(nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
