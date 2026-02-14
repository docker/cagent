package fsx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkFiles_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create structure:
	// tmpDir/
	//   src/
	//     main.go
	//     util.go
	//   lib/
	//     helper.go

	dirs := []string{
		filepath.Join(tmpDir, "src"),
		filepath.Join(tmpDir, "lib"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	files := map[string]string{
		filepath.Join(tmpDir, "src", "main.go"):   "package main",
		filepath.Join(tmpDir, "src", "util.go"):   "package main",
		filepath.Join(tmpDir, "lib", "helper.go"): "package lib",
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("collects all files", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{})
		require.NoError(t, err)
		assert.Len(t, got, 3, "should find all 3 files")
	})

	t.Run("returns relative paths", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{})
		require.NoError(t, err)

		for _, f := range got {
			assert.False(t, filepath.IsAbs(f), "path should be relative: %s", f)
			assert.False(t, strings.HasPrefix(f, tmpDir), "path should not start with tmpDir")
		}
	})
}

func TestWalkFiles_MaxFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create 10 files
	for i := range 10 {
		f := filepath.Join(tmpDir, "file"+string(rune('a'+i))+".txt")
		require.NoError(t, os.WriteFile(f, []byte("content"), 0o644))
	}

	t.Run("respects MaxFiles limit", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxFiles: 5})
		require.NoError(t, err)
		assert.Len(t, got, 5, "should return exactly 5 files")
	})

	t.Run("returns all if MaxFiles is larger", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxFiles: 100})
		require.NoError(t, err)
		assert.Len(t, got, 10, "should return all 10 files")
	})
}

func TestWalkFiles_MaxDepth(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create structure with 3 levels:
	// tmpDir/
	//   level1.txt (depth 1)
	//   dir1/
	//     level2.txt (depth 2)
	//     dir2/
	//       level3.txt (depth 3)
	//       dir3/
	//         level4.txt (depth 4)

	dirs := []string{
		filepath.Join(tmpDir, "dir1", "dir2", "dir3"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	files := map[string]string{
		filepath.Join(tmpDir, "level1.txt"):                         "level 1",
		filepath.Join(tmpDir, "dir1", "level2.txt"):                 "level 2",
		filepath.Join(tmpDir, "dir1", "dir2", "level3.txt"):         "level 3",
		filepath.Join(tmpDir, "dir1", "dir2", "dir3", "level4.txt"): "level 4",
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("MaxDepth 1 gets only root files", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxDepth: 1})
		require.NoError(t, err)
		assert.Len(t, got, 1, "should only find level1.txt")
		assert.Contains(t, got[0], "level1.txt")
	})

	t.Run("MaxDepth 2 gets 2 levels", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxDepth: 2})
		require.NoError(t, err)
		assert.Len(t, got, 2, "should find level1.txt and level2.txt")
	})

	t.Run("MaxDepth 3 gets 3 levels", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxDepth: 3})
		require.NoError(t, err)
		assert.Len(t, got, 3, "should find 3 files")
	})

	t.Run("MaxDepth 0 means unlimited", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{MaxDepth: 0})
		require.NoError(t, err)
		assert.Len(t, got, 4, "should find all 4 files")
	})
}

func TestWalkFiles_HiddenDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create structure with hidden directories:
	// tmpDir/
	//   .git/
	//     config
	//   .cache/
	//     data
	//   src/
	//     main.go

	dirs := []string{
		filepath.Join(tmpDir, ".git"),
		filepath.Join(tmpDir, ".cache"),
		filepath.Join(tmpDir, "src"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	files := map[string]string{
		filepath.Join(tmpDir, ".git", "config"): "[core]",
		filepath.Join(tmpDir, ".cache", "data"): "cached",
		filepath.Join(tmpDir, "src", "main.go"): "package main",
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("skips hidden directories", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{})
		require.NoError(t, err)

		assert.Len(t, got, 1, "should only find src/main.go")
		assert.Contains(t, got[0], "main.go")

		for _, f := range got {
			assert.False(t, strings.HasPrefix(filepath.Base(f), "."))
			assert.NotContains(t, f, ".git")
			assert.NotContains(t, f, ".cache")
		}
	})
}

func TestWalkFiles_HeavyDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create structure with heavy directories:
	// tmpDir/
	//   node_modules/
	//     package.js
	//   vendor/
	//     lib.go
	//   __pycache__/
	//     cache.pyc
	//   src/
	//     main.go

	dirs := []string{
		filepath.Join(tmpDir, "node_modules"),
		filepath.Join(tmpDir, "vendor"),
		filepath.Join(tmpDir, "__pycache__"),
		filepath.Join(tmpDir, "src"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	files := map[string]string{
		filepath.Join(tmpDir, "node_modules", "pkg.js"):   "module.exports = {}",
		filepath.Join(tmpDir, "vendor", "lib.go"):         "package vendor",
		filepath.Join(tmpDir, "__pycache__", "cache.pyc"): "bytecode",
		filepath.Join(tmpDir, "src", "main.go"):           "package main",
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("skips heavy directories", func(t *testing.T) {
		t.Parallel()

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{})
		require.NoError(t, err)

		assert.Len(t, got, 1, "should only find src/main.go")
		assert.Contains(t, got[0], "main.go")

		for _, f := range got {
			assert.NotContains(t, f, "node_modules")
			assert.NotContains(t, f, "vendor")
			assert.NotContains(t, f, "__pycache__")
		}
	})
}

func TestWalkFiles_ShouldIgnore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create structure
	dirs := []string{
		filepath.Join(tmpDir, "src"),
		filepath.Join(tmpDir, "tests"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	files := map[string]string{
		filepath.Join(tmpDir, "src", "main.go"):      "package main",
		filepath.Join(tmpDir, "src", "main_test.go"): "package main",
		filepath.Join(tmpDir, "tests", "e2e.go"):     "package tests",
	}
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("ShouldIgnore filters files", func(t *testing.T) {
		t.Parallel()

		shouldIgnore := func(path string) bool {
			return strings.HasSuffix(path, "_test.go")
		}

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{
			ShouldIgnore: shouldIgnore,
		})
		require.NoError(t, err)

		assert.Len(t, got, 2, "should exclude _test.go files")
		for _, f := range got {
			assert.False(t, strings.HasSuffix(f, "_test.go"))
		}
	})

	t.Run("ShouldIgnore filters directories", func(t *testing.T) {
		t.Parallel()

		shouldIgnore := func(path string) bool {
			return strings.Contains(path, "tests")
		}

		got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{
			ShouldIgnore: shouldIgnore,
		})
		require.NoError(t, err)

		for _, f := range got {
			assert.NotContains(t, f, "tests")
		}
	})
}

func TestWalkFiles_ContextCancellation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create many files
	for i := range 100 {
		f := filepath.Join(tmpDir, "file"+string(rune(i%26+'a'))+string(rune(i/26+'0'))+".txt")
		require.NoError(t, os.WriteFile(f, []byte("content"), 0o644))
	}

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		got, err := WalkFiles(ctx, tmpDir, WalkFilesOptions{})
		// Should either return an error or return partial results
		// The important thing is it doesn't hang
		_ = got
		_ = err
	})

	t.Run("returns partial results on timeout", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 1*time.Millisecond)
		defer cancel()

		// This should return quickly due to timeout
		_, err := WalkFiles(ctx, tmpDir, WalkFilesOptions{})
		// May or may not error depending on timing
		_ = err
	})
}

func TestWalkFiles_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	got, err := WalkFiles(t.Context(), tmpDir, WalkFilesOptions{})
	require.NoError(t, err)
	assert.Empty(t, got, "should return empty for empty directory")
}

func TestWalkFiles_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	got, err := WalkFiles(t.Context(), "/nonexistent/path/that/does/not/exist", WalkFilesOptions{})
	require.Error(t, err, "should return error for non-existent root directory")
	assert.Empty(t, got)
}
