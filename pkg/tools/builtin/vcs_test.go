package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitignoreFile(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")

	gitignoreContent := `# This is a comment
# Another comment

# Ignore build artifacts
*.log
build/
dist/*

# IDE files
.vscode/
.idea

# OS files
.DS_Store
Thumbs.db

# Empty line above

# Leading slash patterns
/root-only.txt
`

	require.NoError(t, os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644))

	patterns, err := parseGitignoreFile(gitignorePath)
	require.NoError(t, err)

	expected := []string{
		"*.log",
		"build/*",
		"dist/*",
		".vscode/*",
		".idea",
		".DS_Store",
		"Thumbs.db",
		"root-only.txt",
	}

	assert.Equal(t, expected, patterns)
}

func TestParseGitignoreFileNotFound(t *testing.T) {
	patterns, err := parseGitignoreFile("/nonexistent/.gitignore")
	require.Error(t, err)
	assert.Nil(t, patterns)
}

func TestNormalizeGitignorePattern(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remove leading slash",
			input:    "/build",
			expected: "build",
		},
		{
			name:     "directory pattern",
			input:    "build/",
			expected: "build/*",
		},
		{
			name:     "file pattern",
			input:    "*.log",
			expected: "*.log",
		},
		{
			name:     "already normalized",
			input:    "node_modules",
			expected: "node_modules",
		},
		{
			name:     "nested directory pattern",
			input:    "src/build/",
			expected: "src/build/*",
		},
		{
			name:     "nested with leading slash",
			input:    "/src/build/",
			expected: "src/build/*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeGitignorePattern(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindGitignorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a nested directory structure
	subDir := filepath.Join(tmpDir, "src", "main")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Create root .gitignore
	rootGitignore := filepath.Join(tmpDir, ".gitignore")
	rootContent := `*.log
build/
.DS_Store`
	require.NoError(t, os.WriteFile(rootGitignore, []byte(rootContent), 0o644))

	// Create nested .gitignore
	nestedGitignore := filepath.Join(tmpDir, "src", ".gitignore")
	nestedContent := `*.tmp
local.config`
	require.NoError(t, os.WriteFile(nestedGitignore, []byte(nestedContent), 0o644))

	// Create .git directory to mark repo root
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))

	patterns := findGitignorePatterns(subDir)

	// Should contain VCS patterns, root .gitignore patterns, and nested patterns
	expectedPatterns := []string{
		".git/*",
		".svn/*",
		".hg/*",
		".bzr/*",
		"CVS/*",
		"_darcs/*",
		"*.tmp",
		"local.config",
		"*.log",
		"build/*",
		".DS_Store",
	}

	for _, expected := range expectedPatterns {
		assert.Contains(t, patterns, expected, "Pattern %s should be included", expected)
	}
}

func TestFindGitignorePatternsNoGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// No .git directory and no .gitignore files
	patterns := findGitignorePatterns(tmpDir)

	// Should only contain VCS patterns
	expectedVCSPatterns := []string{
		".git/*",
		".svn/*",
		".hg/*",
		".bzr/*",
		"CVS/*",
		"_darcs/*",
	}

	for _, expected := range expectedVCSPatterns {
		assert.Contains(t, patterns, expected, "VCS pattern %s should be included", expected)
	}

	// Should be exactly the VCS patterns
	assert.Len(t, patterns, len(expectedVCSPatterns))
}

func TestFilesystemTool_WithIgnoreVCS(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure with VCS directories and gitignore files
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git", "hooks"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "build"), 0o755))

	// Create files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("readme"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("git config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "build", "app"), []byte("binary"), 0o644))

	// Create .gitignore
	gitignoreContent := `build/
*.log`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0o644))

	// Create tool with VCS ignore enabled
	tool := NewFilesystemTool([]string{tmpDir}, WithIgnoreVCS(true))

	// Verify VCS patterns are set
	assert.True(t, tool.ignoreVCS)
	assert.NotEmpty(t, tool.vcsPatterns)

	// Should contain both VCS and gitignore patterns
	hasGitPattern := false
	hasBuildPattern := false
	for _, pattern := range tool.vcsPatterns {
		if pattern == ".git/*" {
			hasGitPattern = true
		}
		if pattern == "build/*" {
			hasBuildPattern = true
		}
	}
	assert.True(t, hasGitPattern, "Should include .git/* pattern")
	assert.True(t, hasBuildPattern, "Should include build/* pattern from .gitignore")
}

func TestFilesystemTool_WithoutIgnoreVCS(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFilesystemTool([]string{tmpDir})

	assert.False(t, tool.ignoreVCS)
	assert.Empty(t, tool.vcsPatterns)
}

func TestFilesystemTool_SearchFilesWithVCSIgnore(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git", "hooks"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "build"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "node_modules", "package"), 0o755))

	// Create files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src", "test.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".git", "test.config"), []byte("git config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "build", "test.bin"), []byte("binary"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "node_modules", "test.js"), []byte("module"), 0o644))

	// Create .gitignore
	gitignoreContent := `build/
node_modules/`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0o644))

	// Test with VCS ignore enabled
	toolWithVCS := NewFilesystemTool([]string{tmpDir}, WithIgnoreVCS(true))
	handler := getToolHandler(t, toolWithVCS, "search_files")

	args := map[string]any{
		"path":    tmpDir,
		"pattern": "test",
	}
	result := callHandler(t, handler, args)

	// Should find files in allowed areas but not in VCS or gitignore areas
	assert.Contains(t, result.Output, "test.txt")
	assert.Contains(t, result.Output, "src/test.go")
	assert.NotContains(t, result.Output, ".git/test.config")
	assert.NotContains(t, result.Output, "build/test.bin")
	assert.NotContains(t, result.Output, "node_modules/test.js")

	// Test with VCS ignore disabled
	toolWithoutVCS := NewFilesystemTool([]string{tmpDir})
	handlerNoVCS := getToolHandler(t, toolWithoutVCS, "search_files")

	resultNoVCS := callHandler(t, handlerNoVCS, args)

	// Should find all files when VCS ignore is disabled
	assert.Contains(t, resultNoVCS.Output, "test.txt")
	assert.Contains(t, resultNoVCS.Output, "src/test.go")
	assert.Contains(t, resultNoVCS.Output, ".git/test.config")
	assert.Contains(t, resultNoVCS.Output, "build/test.bin")
	assert.Contains(t, resultNoVCS.Output, "node_modules/test.js")
}

func TestFilesystemTool_SearchFilesContentWithVCSIgnore(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755))

	// Create files with searchable content
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("func main() { fmt.Println(\"hello\") }"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src", "util.go"), []byte("func hello() { return \"hello\" }"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("hello=world"), 0o644))

	// Create tool with VCS ignore
	tool := NewFilesystemTool([]string{tmpDir}, WithIgnoreVCS(true))
	handler := getToolHandler(t, tool, "search_files_content")

	args := map[string]any{
		"path":  tmpDir,
		"query": "hello",
	}
	result := callHandler(t, handler, args)

	// Should find content in allowed files but not in VCS files
	assert.Contains(t, result.Output, "main.go")
	assert.Contains(t, result.Output, "src/util.go")
	assert.NotContains(t, result.Output, ".git/config")
}
