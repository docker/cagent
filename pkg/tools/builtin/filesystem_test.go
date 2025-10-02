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
		toolNames = append(toolNames, tool.Function.Name)
		assert.NotNil(t, tool.Handler)
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
		assert.NotEqual(t, tool.Function.Name, tool.DisplayName())
	}
}

func TestFilesystemTool_IsPathAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Test allowed path
	allowedPath := filepath.Join(tmpDir, "subdir", "file.txt")
	err := tool.isPathAllowed(allowedPath)
	require.NoError(t, err)

	// Test disallowed path
	disallowedPath := "/etc/passwd"
	err = tool.isPathAllowed(disallowedPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not within allowed directories")
}

func TestFilesystemTool_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	handler := getToolHandler(t, tool, "create_directory")

	// Test successful directory creation
	newDir := filepath.Join(tmpDir, "test", "nested", "dir")
	args := map[string]any{"path": newDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Directory created successfully")
	assert.DirExists(t, newDir)

	// Test disallowed path
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

	// Test successful file write
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	args := map[string]any{
		"path":    testFile,
		"content": content,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "File written successfully")
	assert.FileExists(t, testFile)

	// Verify content
	writtenContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(writtenContent))

	// Test disallowed path
	disallowedFile := "/etc/test.txt"
	args = map[string]any{
		"path":    disallowedFile,
		"content": "test",
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error:")
	assert.Contains(t, result.Output, "not within allowed directories")
}

func TestFilesystemTool_ReadFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "read_file")

	// Test successful file read
	args := map[string]any{"path": testFile}
	result := callHandler(t, handler, args)

	assert.Equal(t, content, result.Output)

	// Test non-existent file
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.txt")
	args = map[string]any{"path": nonExistentFile}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error reading file")

	// Test disallowed path
	disallowedFile := "/etc/passwd"
	args = map[string]any{"path": disallowedFile}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error:")
	assert.Contains(t, result.Output, "not within allowed directories")
}

func TestFilesystemTool_ReadMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	content1 := "Content 1"
	content2 := "Content 2"

	require.NoError(t, os.WriteFile(file1, []byte(content1), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte(content2), 0o644))

	handler := getToolHandler(t, tool, "read_multiple_files")

	// Test successful multiple file read
	args := map[string]any{"paths": []string{file1, file2}}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "=== "+file1+" ===")
	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "=== "+file2+" ===")
	assert.Contains(t, result.Output, content2)

	// Test with non-existent file
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.txt")
	args = map[string]any{"paths": []string{file1, nonExistentFile}}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "Error reading file")
}

func TestFilesystemTool_ListDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test files and directories
	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")

	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))
	require.NoError(t, os.Mkdir(testDir, 0o755))

	handler := getToolHandler(t, tool, "list_directory")

	// Test successful directory listing
	args := map[string]any{"path": tmpDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "FILE test.txt")
	assert.Contains(t, result.Output, "DIR  testdir")

	// Test non-existent directory
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")
	args = map[string]any{"path": nonExistentDir}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Error reading directory")
}

func TestFilesystemTool_ListDirectoryWithSizes(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test files and directories
	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")
	content := "Hello World"

	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))
	require.NoError(t, os.Mkdir(testDir, 0o755))

	handler := getToolHandler(t, tool, "list_directory_with_sizes")

	// Test successful directory listing with sizes
	args := map[string]any{"path": tmpDir}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "FILE test.txt (11 bytes)")
	assert.Contains(t, result.Output, "DIR  testdir")
}

func TestFilesystemTool_GetFileInfo(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "get_file_info")

	// Test successful file info
	args := map[string]any{"path": testFile}
	result := callHandler(t, handler, args)

	var fileInfo map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Output), &fileInfo))

	assert.Equal(t, "test.txt", fileInfo["name"])
	assert.InDelta(t, len(content), fileInfo["size"], 0.0)
	assert.Equal(t, false, fileInfo["isDir"])

	// Test directory info
	args = map[string]any{"path": tmpDir}
	result = callHandler(t, handler, args)

	require.NoError(t, json.Unmarshal([]byte(result.Output), &fileInfo))
	assert.Equal(t, true, fileInfo["isDir"])
}

func TestFilesystemTool_MoveFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test file
	sourceFile := filepath.Join(tmpDir, "source.txt")
	destFile := filepath.Join(tmpDir, "dest.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(sourceFile, []byte(content), 0o644))

	handler := getToolHandler(t, tool, "move_file")

	// Test successful file move
	args := map[string]any{
		"source":      sourceFile,
		"destination": destFile,
	}
	result := callHandler(t, handler, args)

	assert.Contains(t, result.Output, "Successfully moved")
	assert.NoFileExists(t, sourceFile)
	assert.FileExists(t, destFile)

	// Verify content preserved
	movedContent, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(movedContent))

	// Test move to existing file (should fail)
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

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "Hello World\nThis is a test\nGoodbye World"
	require.NoError(t, os.WriteFile(testFile, []byte(originalContent), 0o644))

	handler := getToolHandler(t, tool, "edit_file")

	// Test successful file edit
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

	// Verify changes
	editedContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	expected := "Hi Universe\nThis is a test\nSee you later"
	assert.Equal(t, expected, string(editedContent))

	// Test edit with non-existent text
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

func TestFilesystemTool_DirectoryTree(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	// Create test directory structure
	subDir1 := filepath.Join(tmpDir, "subdir1")
	subDir2 := filepath.Join(tmpDir, "subdir1", "subdir2")
	require.NoError(t, os.MkdirAll(subDir2, 0o755))

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(subDir1, "file2.txt")
	file3 := filepath.Join(subDir2, "file3.txt")

	require.NoError(t, os.WriteFile(file1, []byte("test1"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("test2"), 0o644))
	require.NoError(t, os.WriteFile(file3, []byte("test3"), 0o644))

	handler := getToolHandler(t, tool, "directory_tree")

	// Test directory tree without depth limit
	args := map[string]any{"path": tmpDir}
	result := callHandler(t, handler, args)

	var tree TreeNode
	require.NoError(t, json.Unmarshal([]byte(result.Output), &tree))

	assert.Equal(t, "directory", tree.Type)
	assert.GreaterOrEqual(t, len(tree.Children), 2) // file1.txt and subdir1

	// Test with depth limit
	args = map[string]any{
		"path":      tmpDir,
		"max_depth": 2,
	}
	result = callHandler(t, handler, args)

	require.NoError(t, json.Unmarshal([]byte(result.Output), &tree))
	assert.Equal(t, "directory", tree.Type)
}

func TestFilesystemTool_SearchFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.log"), []byte("log"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte("data"), 0o644))

	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "test_sub.txt"), []byte("sub"), 0o644))

	tool := NewFilesystemTool([]string{tmpDir})
	handler := getToolHandler(t, tool, "search_files")

	// Test search for files containing "asdf"
	args := map[string]any{
		"path":    tmpDir,
		"pattern": "asdf",
	}
	result := callHandler(t, handler, args)

	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Len(t, lines, 1) // Should find test.txt, test.log, and test_sub.txt
	assert.Contains(t, lines, "No files found")

	// Test search for files containing "test"
	args = map[string]any{
		"path":    tmpDir,
		"pattern": "test",
	}
	result = callHandler(t, handler, args)

	lines = strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Contains(t, result.Output, "3 files found:\n")
	assert.Len(t, lines, 3+1) // Should find test.txt, test.log, and test_sub.txt

	// Test search with exclude patterns
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

	// Create test files with different content
	file1Content := "This is a test file\nwith multiple lines\ncontaining test data"
	file2Content := "Another file\nwith different content\nno matching terms here"
	file3Content := "Final file\nhas test in it\nand more test content"

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte(file1Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte(file2Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file3.txt"), []byte(file3Content), 0o644))

	handler := getToolHandler(t, tool, "search_files_content")

	// Test literal text search
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

	// Test regex search
	args = map[string]any{
		"path":     tmpDir,
		"pattern":  "*.txt",
		"query":    "test.*data",
		"is_regex": true,
	}
	result = callHandler(t, handler, args)

	assert.Contains(t, result.Output, "file1.txt:3:")

	// Test invalid regex
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

	// Create test files
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "first.txt"), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ignored"), []byte("ignored"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "second.txt"), []byte("second"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "third.txt"), []byte("third"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "ignored"), []byte("ignored"), 0o644))

	tool := NewFilesystemTool([]string{tmpDir})
	handler := getToolHandler(t, tool, "search_files")

	// Test search for files containing ".txt" files
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

	// Test listing allowed directories
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

	// Test invalid JSON
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

	// Test Start method
	err := tool.Start(t.Context())
	require.NoError(t, err)

	// Test Stop method
	err = tool.Stop()
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
		args := map[string]any{
			"path":    testFile,
			"content": testContent,
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

		editArgs := map[string]any{
			"path": testFile,
			"edits": []map[string]any{
				{
					"oldText": "fmt.Println",
					"newText": "fmt.Printf",
				},
			},
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

		// Check that post-edit was run again
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
		if tl.Function.Name == toolName {
			return tl.Handler
		}
	}

	t.Fatalf("Tool %s not found", toolName)
	return nil
}

func callHandler(t *testing.T, handler tools.ToolHandler, args map[string]any) *tools.ToolCallResult {
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
	// Create temporary directories for testing
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create filesystem tool with only tmpDir1 initially allowed
	tool := NewFilesystemTool([]string{tmpDir1})
	handler := getToolHandler(t, tool, "add_allowed_directory")

	t.Run("request consent for new directory", func(t *testing.T) {
		args := map[string]any{
			"path":   tmpDir2,
			"reason": "Need access for testing",
		}
		result := callHandler(t, handler, args)

		// Should return consent request message
		assert.Contains(t, result.Output, "SECURITY CONSENT REQUEST")
		assert.Contains(t, result.Output, tmpDir2)
		assert.Contains(t, result.Output, "Need access for testing")
		assert.Contains(t, result.Output, "confirmed")

		// Directory should not be added yet
		assert.Len(t, tool.allowedDirectories, 1)
		assert.Equal(t, tmpDir1, tool.allowedDirectories[0])
	})

	t.Run("add directory with confirmation", func(t *testing.T) {
		args := map[string]any{
			"path":      tmpDir2,
			"reason":    "Need access for testing",
			"confirmed": true,
		}
		result := callHandler(t, handler, args)

		// Should return success message
		assert.Contains(t, result.Output, "Directory successfully added")
		assert.Contains(t, result.Output, tmpDir2)

		// Directory should now be added
		assert.Len(t, tool.allowedDirectories, 2)
		assert.Contains(t, tool.allowedDirectories, tmpDir1)
		assert.Contains(t, tool.allowedDirectories, tmpDir2)
	})

	t.Run("attempt to add already allowed directory", func(t *testing.T) {
		args := map[string]any{
			"path":      tmpDir1,
			"reason":    "Testing duplicate",
			"confirmed": true,
		}
		result := callHandler(t, handler, args)

		// Should return already allowed message
		assert.Contains(t, result.Output, "already in allowed directories")
		assert.Contains(t, result.Output, tmpDir1)

		// Should not add duplicate
		assert.Len(t, tool.allowedDirectories, 2)
	})

	t.Run("attempt to add subdirectory of allowed directory", func(t *testing.T) {
		subDir := filepath.Join(tmpDir1, "subdir")
		err := os.MkdirAll(subDir, 0o755)
		require.NoError(t, err)

		args := map[string]any{
			"path":      subDir,
			"reason":    "Testing subdirectory",
			"confirmed": true,
		}
		result := callHandler(t, handler, args)

		// Should return already accessible message
		assert.Contains(t, result.Output, "already accessible")
		assert.Contains(t, result.Output, subDir)
		assert.Contains(t, result.Output, tmpDir1)

		// Should not add subdirectory
		assert.Len(t, tool.allowedDirectories, 2)
	})

	t.Run("attempt to add non-existent directory", func(t *testing.T) {
		nonExistent := "/path/that/does/not/exist"
		args := map[string]any{
			"path":      nonExistent,
			"reason":    "Testing non-existent",
			"confirmed": true,
		}
		result := callHandler(t, handler, args)

		// Should return error message
		assert.Contains(t, result.Output, "Error accessing path")

		// Should not add non-existent directory
		assert.Len(t, tool.allowedDirectories, 2)
	})

	t.Run("attempt to add file instead of directory", func(t *testing.T) {
		// Create a file
		tempFile := filepath.Join(tmpDir2, "testfile.txt")
		err := os.WriteFile(tempFile, []byte("test"), 0o644)
		require.NoError(t, err)

		args := map[string]any{
			"path":      tempFile,
			"reason":    "Testing file",
			"confirmed": true,
		}
		result := callHandler(t, handler, args)

		// Should return error message
		assert.Contains(t, result.Output, "is not a directory")

		// Should not add file
		assert.Len(t, tool.allowedDirectories, 2)
	})
}

func TestFilesystemTool_FilterTools(t *testing.T) {
	allowedDirs := []string{"/tmp"}
	tool := NewFilesystemTool(allowedDirs, WithAllowedTools([]string{"list_allowed_directories"}))

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 1)
	require.Equal(t, "list_allowed_directories", allTools[0].Function.Name)
	require.NotNil(t, allTools[0].Handler)
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
		assert.NotEmpty(t, tool.Function.OutputSchema.Type)
	}
}
