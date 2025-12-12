package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo initializes a git repository in the given directory
// This is needed for go-git's gitignore parsing to work properly
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Create .git directory structure
	gitDir := filepath.Join(dir, ".git")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755))

	// Create minimal git config
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte(`[core]
	repositoryformatversion = 0
	filemode = false
	bare = false
`), 0o644))
}

func TestFilesystemTool_DisplayNames(t *testing.T) {
	t.Parallel()
	tool := NewFilesystemTool("/tmp")

	all, err := tool.Tools(t.Context())
	require.NoError(t, err)

	for _, tool := range all {
		assert.NotEmpty(t, tool.DisplayName())
		assert.NotEqual(t, tool.Name, tool.DisplayName())
	}
}

func TestFilesystemTool_WriteFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	result, err := tool.handleWriteFile(t.Context(), WriteFileArgs{
		Path:    testFile,
		Content: content,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "File written successfully")
	assert.FileExists(t, testFile)

	writtenContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(writtenContent))
}

func TestFilesystemTool_WriteFile_NestedDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	nestedFile := filepath.Join(tmpDir, "a", "b", "c", "test.txt")
	content := "Hello, nested world!"

	result, err := tool.handleWriteFile(t.Context(), WriteFileArgs{
		Path:    nestedFile,
		Content: content,
	})
	require.NoError(t, err)

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
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!"
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	result, err := tool.handleReadFile(t.Context(), ReadFileArgs{
		Path: testFile,
	})
	require.NoError(t, err)
	assert.Equal(t, content, result.Output)

	result, err = tool.handleReadFile(t.Context(), ReadFileArgs{
		Path: filepath.Join(tmpDir, "nonexistent.txt"),
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Error reading file")
}

func TestFilesystemTool_ReadMultipleFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	content1 := "Content 1"
	content2 := "Content 2"

	require.NoError(t, os.WriteFile(file1, []byte(content1), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte(content2), 0o644))

	result, err := tool.handleReadMultipleFiles(t.Context(), ReadMultipleFilesArgs{
		Paths: []string{file1, file2},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "=== "+file1+" ===")
	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "=== "+file2+" ===")
	assert.Contains(t, result.Output, content2)

	result, err = tool.handleReadMultipleFiles(t.Context(), ReadMultipleFilesArgs{
		Paths: []string{file1, filepath.Join(tmpDir, "nonexistent.txt")},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, content1)
	assert.Contains(t, result.Output, "Error reading file")
}

func TestFilesystemTool_ListDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")

	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))
	require.NoError(t, os.Mkdir(testDir, 0o755))

	result, err := tool.handleListDirectory(t.Context(), ListDirectoryArgs{
		Path: tmpDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "FILE test.txt")
	assert.Contains(t, result.Output, "DIR  testdir")

	result, err = tool.handleListDirectory(t.Context(), ListDirectoryArgs{
		Path: filepath.Join(tmpDir, "nonexistent"),
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Error reading directory")
}

func TestFilesystemTool_EditFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "Hello World\nThis is a test\nGoodbye World"
	require.NoError(t, os.WriteFile(testFile, []byte(originalContent), 0o644))

	result, err := tool.handleEditFile(t.Context(), EditFileArgs{
		Path: testFile,
		Edits: []Edit{
			{OldText: "Hello World", NewText: "Hi Universe"},
			{OldText: "Goodbye World", NewText: "See you later"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "File edited successfully")

	editedContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	expected := "Hi Universe\nThis is a test\nSee you later"
	assert.Equal(t, expected, string(editedContent))
	result, err = tool.handleEditFile(t.Context(), EditFileArgs{
		Path: testFile,
		Edits: []Edit{
			{
				OldText: "Non-existent text",
				NewText: "Replacement",
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "old text not found")
}

func TestFilesystemTool_SearchFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM scratch"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.log"), []byte("log"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte("data"), 0o644))

	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "test_sub.txt"), []byte("sub"), 0o644))

	tool := NewFilesystemTool(tmpDir)
	tests := []struct {
		name            string
		pattern         string
		excludePatterns []string
		wantCount       int
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "no files found",
			pattern:         "asdf",
			wantCount:       0,
			wantContains:    []string{"No files found"},
			wantNotContains: nil,
		},
		{
			name:            "all files found",
			pattern:         "*",
			wantCount:       0,
			wantContains:    []string{"5 files found:\n"},
			wantNotContains: nil,
		},
		{
			name:            "all files found with dot",
			pattern:         "*.*",
			wantCount:       0,
			wantContains:    []string{"4 files found:\n"},
			wantNotContains: nil,
		},
		{
			name:            "dockerfile pattern",
			pattern:         "Dockerfile*",
			wantCount:       1,
			wantContains:    []string{"1 files found:\n", "Dockerfile"},
			wantNotContains: nil,
		},
		{
			name:            "test pattern finds all",
			pattern:         "test",
			wantCount:       3,
			wantContains:    []string{"3 files found:\n"},
			wantNotContains: nil,
		},
		{
			name:            "test pattern with log exclusion",
			pattern:         "test",
			excludePatterns: []string{"*.log"},
			wantCount:       2,
			wantContains:    []string{"test.txt"},
			wantNotContains: []string{"test.log"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
				Path:            tmpDir,
				Pattern:         tt.pattern,
				ExcludePatterns: tt.excludePatterns,
			})
			require.NoError(t, err)

			for _, want := range tt.wantContains {
				assert.Contains(t, result.Output, want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, result.Output, notWant)
			}

			if tt.wantCount > 0 {
				lines := strings.Split(strings.TrimSpace(result.Output), "\n")
				assert.Len(t, lines, tt.wantCount+1) // +1 for the header line
			}
		})
	}
}

func TestFilesystemTool_SearchFilesContent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	file1Content := "This is a test file\nwith multiple lines\ncontaining test data"
	file2Content := "Another file\nwith different content\nno matching terms here"
	file3Content := "Final file\nhas test in it\nand more test content"

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte(file1Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte(file2Content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file3.txt"), []byte(file3Content), 0o644))

	result, err := tool.handleSearchFilesContent(t.Context(), SearchFilesContentArgs{
		Path:  tmpDir,
		Query: "test",
	})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "file1.txt:1:")
	assert.Contains(t, result.Output, "file1.txt:3:")
	assert.Contains(t, result.Output, "file3.txt:2:")
	assert.Contains(t, result.Output, "file3.txt:3:")
	assert.NotContains(t, result.Output, "file2.txt")

	result, err = tool.handleSearchFilesContent(t.Context(), SearchFilesContentArgs{
		Path:    tmpDir,
		Query:   "test.*data",
		IsRegex: true,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "file1.txt:3:")

	result, err = tool.handleSearchFilesContent(t.Context(), SearchFilesContentArgs{
		Path:    tmpDir,
		Query:   "[invalid",
		IsRegex: true,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Invalid regex pattern")
}

func TestFilesystemTool_SearchFiles_RecursivePattern(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "first.txt"), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ignored"), []byte("ignored"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "second.txt"), []byte("second"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "third.txt"), []byte("third"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child", "ignored"), []byte("ignored"), 0o644))

	tool := NewFilesystemTool(tmpDir)
	result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
		Path:    tmpDir,
		Pattern: "*.txt",
	})
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	assert.Contains(t, result.Output, "3 files found:\n")
	assert.Len(t, lines, 3+1) // Should find first.txt, second.txt, and third.txt
}

func TestFilesystemTool_PostEditCommands(t *testing.T) {
	t.Parallel()
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
	tool := NewFilesystemTool(tmpDir, WithPostEditCommands(postEditConfigs))

	formattedFile := testFile + ".formatted"
	t.Run("write_file", func(t *testing.T) {
		result, err := tool.handleWriteFile(t.Context(), WriteFileArgs{
			Path:    testFile,
			Content: testContent,
		})
		require.NoError(t, err)
		assert.Contains(t, result.Output, "File written successfully")

		_, err = os.Stat(formattedFile)
		require.NoError(t, err, "Post-edit command should have created formatted file")
		require.NoError(t, os.Remove(formattedFile))
	})

	t.Run("edit_file", func(t *testing.T) {
		result, err := tool.handleEditFile(t.Context(), EditFileArgs{
			Path: testFile,
			Edits: []Edit{{
				OldText: "fmt.Println",
				NewText: "fmt.Printf",
			}},
		})
		require.NoError(t, err)
		assert.Contains(t, result.Output, "File edited successfully")

		_, err = os.Stat(formattedFile)
		require.NoError(t, err, "Post-edit command should have run after edit")
	})
}

func TestMatchExcludePattern(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := matchExcludePattern(tc.pattern, tc.relPath)
			assert.Equal(t, tc.expected, result, "Pattern: %s, Path: %s, IsDir: %v", tc.pattern, tc.relPath)
		})
	}
}

func TestFilesystemTool_OutputSchema(t *testing.T) {
	tool := NewFilesystemTool("")

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestFilesystemTool_IgnoreVCS_Default(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test content"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
		Path:    tmpDir,
		Pattern: "*",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test.txt")
	assert.NotContains(t, result.Output, ".git")
}

func TestFilesystemTool_IgnoreVCS_Disabled(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test content"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(false))
	result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
		Path:    tmpDir,
		Pattern: "*",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test.txt")
	assert.Contains(t, result.Output, ".git")
}

func TestFilesystemTool_GitignorePatterns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Initialize git repository
	initGitRepo(t, tmpDir)

	// Create .gitignore
	gitignoreContent := `*.log
node_modules/
build/
temp_*
!important.log
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "debug.log"), []byte("log"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "important.log"), []byte("important"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "temp_file.txt"), []byte("temp"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "node_modules", "package.json"), []byte("{}"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "build", "output.js"), []byte("code"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
		Path:    tmpDir,
		Pattern: "*",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test.txt")
	assert.Contains(t, result.Output, "important.log")   // negated pattern
	assert.NotContains(t, result.Output, "debug.log")    // ignored
	assert.NotContains(t, result.Output, "temp_file")    // ignored
	assert.NotContains(t, result.Output, "node_modules") // ignored directory
	assert.NotContains(t, result.Output, "build")        // ignored directory
}

func TestFilesystemTool_SearchContent_WithGitignore(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	initGitRepo(t, tmpDir)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "source.txt"), []byte("findme"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "debug.log"), []byte("findme"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleSearchFilesContent(t.Context(), SearchFilesContentArgs{
		Path:  tmpDir,
		Query: "findme",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "source.txt")
	assert.NotContains(t, result.Output, "debug.log")
}

func TestFilesystemTool_ListDirectory_IgnoresVCS(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("test"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleListDirectory(t.Context(), ListDirectoryArgs{
		Path: tmpDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "file1.txt")
	assert.Contains(t, result.Output, "file2.txt")
	assert.NotContains(t, result.Output, ".git")
}

func TestFilesystemTool_SubdirectoryGitignorePatterns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Initialize git repository
	initGitRepo(t, tmpDir)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.log\n"), 0o644))
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, ".gitignore"), []byte("*.tmp\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.log"), []byte("log"), 0o644)) // ignored by root .gitignore
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.tmp"), []byte("tmp"), 0o644)) // NOT ignored (subdir .gitignore doesn't apply here)
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.txt"), []byte("sub"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.log"), []byte("log"), 0o644)) // ignored by root .gitignore
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.tmp"), []byte("tmp"), 0o644)) // ignored by subdir .gitignore

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleSearchFiles(t.Context(), SearchFilesArgs{
		Path:    tmpDir,
		Pattern: "*",
	})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "root.txt")    // not ignored
	assert.NotContains(t, result.Output, "root.log") // ignored by root .gitignore
	assert.Contains(t, result.Output, "root.tmp")    // NOT ignored - subdir .gitignore doesn't apply to root
	assert.Contains(t, result.Output, "sub.txt")     // not ignored
	assert.NotContains(t, result.Output, "sub.log")  // ignored by root .gitignore
	assert.NotContains(t, result.Output, "sub.tmp")  // ignored by subdir .gitignore
}

func TestFilesystemTool_DirectoryTree_IgnoresVCS(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0o644))
	srcDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.Mkdir(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0o644))

	tool := NewFilesystemTool(tmpDir, WithIgnoreVCS(true))
	result, err := tool.handleDirectoryTree(t.Context(), DirectoryTreeArgs{
		Path: tmpDir,
	})
	require.NoError(t, err)

	var tree map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Output), &tree))

	// Should include src but not .git
	children := tree["children"].([]any)
	var childNames []string
	for _, child := range children {
		childMap := child.(map[string]any)
		childNames = append(childNames, childMap["name"].(string))
	}

	assert.Contains(t, childNames, "src")
	assert.NotContains(t, childNames, ".git")
}
