package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandAllAttachments(t *testing.T) {
	t.Parallel()

	t.Run("no attachments returns content unchanged", func(t *testing.T) {
		t.Parallel()
		e := &editor{attachments: nil}
		content := "hello world"

		result := e.expandAllAttachments(content)

		assert.Equal(t, content, result)
	})

	t.Run("empty refs returns content unchanged", func(t *testing.T) {
		t.Parallel()
		e := &editor{attachments: []attachment{}}
		content := "hello world"

		result := e.expandAllAttachments(content)

		assert.Equal(t, content, result)
	})

	t.Run("file content in attachments section", func(t *testing.T) {
		t.Parallel()

		// Create a temp file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(tmpFile, []byte("file content here"), 0o644))

		ref := "@" + tmpFile
		e := &editor{attachments: []attachment{{
			path:        tmpFile,
			placeholder: ref,
			label:       "test.txt (17 B)",
			isTemp:      false,
		}}}
		content := "analyze " + ref

		result := e.expandAllAttachments(content)

		// Placeholder stays in message
		assert.True(t, strings.HasPrefix(result, "analyze "+ref), "placeholder should stay in message")
		// Content is in attachments section
		assert.Contains(t, result, "<attachments>")
		assert.Contains(t, result, "</attachments>")
		assert.Contains(t, result, "<"+ref+">")
		assert.Contains(t, result, "</"+ref+">")
		assert.Contains(t, result, "file content here")
		assert.Nil(t, e.attachments, "attachments should be cleared after expansion")
	})

	t.Run("multiple file references", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		file1 := filepath.Join(tmpDir, "first.go")
		file2 := filepath.Join(tmpDir, "second.go")
		require.NoError(t, os.WriteFile(file1, []byte("package first"), 0o644))
		require.NoError(t, os.WriteFile(file2, []byte("package second"), 0o644))

		ref1 := "@" + file1
		ref2 := "@" + file2
		e := &editor{attachments: []attachment{
			{path: file1, placeholder: ref1, isTemp: false},
			{path: file2, placeholder: ref2, isTemp: false},
		}}
		content := "compare " + ref1 + " with " + ref2

		result := e.expandAllAttachments(content)

		assert.Contains(t, result, "<"+ref1+">")
		assert.Contains(t, result, "package first")
		assert.Contains(t, result, "<"+ref2+">")
		assert.Contains(t, result, "package second")
	})

	t.Run("skips refs not in content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(tmpFile, []byte("content"), 0o644))

		ref := "@" + tmpFile
		e := &editor{attachments: []attachment{{
			path:        tmpFile,
			placeholder: ref,
			isTemp:      false,
		}}}
		content := "message without the reference"

		result := e.expandAllAttachments(content)

		assert.Equal(t, content, result, "should return unchanged when ref not in content")
		assert.Nil(t, e.attachments, "attachments should be cleared after expansion")
	})

	t.Run("skips nonexistent files", func(t *testing.T) {
		t.Parallel()

		ref := "@/nonexistent/path/file.txt"
		e := &editor{attachments: []attachment{{
			path:        "/nonexistent/path/file.txt",
			placeholder: ref,
			isTemp:      false,
		}}}
		content := "analyze " + ref

		result := e.expandAllAttachments(content)

		// Ref stays in content since file doesn't exist, no attachments section
		assert.Equal(t, content, result)
		assert.Nil(t, e.attachments, "attachments should still be cleared")
	})

	t.Run("skips directories", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		ref := "@" + tmpDir
		// Note: addFileAttachment would normally reject directories, but we test
		// expandAllAttachments directly here - it will fail to read as file
		e := &editor{attachments: []attachment{{
			path:        tmpDir,
			placeholder: ref,
			isTemp:      false,
		}}}
		content := "analyze " + ref

		result := e.expandAllAttachments(content)

		// os.ReadFile on a directory returns an error, so no attachment added
		assert.Equal(t, content, result)
		assert.Nil(t, e.attachments, "attachments should be cleared after expansion")
	})

	t.Run("mixed valid and invalid refs", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		validFile := filepath.Join(tmpDir, "valid.txt")
		require.NoError(t, os.WriteFile(validFile, []byte("valid content"), 0o644))

		validRef := "@" + validFile
		invalidRef := "@/nonexistent/file.txt"
		e := &editor{attachments: []attachment{
			{path: validFile, placeholder: validRef, isTemp: false},
			{path: "/nonexistent/file.txt", placeholder: invalidRef, isTemp: false},
		}}
		content := "check " + validRef + " and " + invalidRef

		result := e.expandAllAttachments(content)

		assert.Contains(t, result, "<"+validRef+">")
		assert.Contains(t, result, "valid content")
		// Invalid ref stays as-is in content (no attachment for it)
		assert.Contains(t, result, invalidRef)
		assert.Nil(t, e.attachments, "attachments should be cleared after expansion")
	})
}

func TestTryAddFileRef(t *testing.T) {
	t.Parallel()

	t.Run("adds valid file path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "manual.txt")
		require.NoError(t, os.WriteFile(tmpFile, []byte("content"), 0o644))

		e := &editor{attachments: nil}
		e.tryAddFileRef("@" + tmpFile)

		require.Len(t, e.attachments, 1)
		assert.Equal(t, "@"+tmpFile, e.attachments[0].placeholder)
		assert.Equal(t, tmpFile, e.attachments[0].path)
		assert.False(t, e.attachments[0].isTemp)
	})

	t.Run("ignores @mentions without path characters", func(t *testing.T) {
		t.Parallel()

		e := &editor{attachments: nil}
		e.tryAddFileRef("@username")

		assert.Nil(t, e.attachments, "@mentions without / or . should be ignored")
	})

	t.Run("ignores nonexistent files", func(t *testing.T) {
		t.Parallel()

		e := &editor{attachments: nil}
		e.tryAddFileRef("@/nonexistent/file.txt")

		assert.Nil(t, e.attachments, "nonexistent files should be ignored")
	})

	t.Run("ignores directories", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		e := &editor{attachments: nil}
		e.tryAddFileRef("@" + tmpDir)

		assert.Nil(t, e.attachments, "directories should be ignored")
	})

	t.Run("avoids duplicates", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "file.go")
		require.NoError(t, os.WriteFile(tmpFile, []byte("content"), 0o644))

		ref := "@" + tmpFile
		e := &editor{attachments: []attachment{{
			path:        tmpFile,
			placeholder: ref,
			isTemp:      false,
		}}}
		e.tryAddFileRef(ref)

		assert.Len(t, e.attachments, 1, "should not add duplicate")
	})

	t.Run("combines with completion refs", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		completedFile := filepath.Join(tmpDir, "completed.go")
		manualFile := filepath.Join(tmpDir, "manual.go")
		require.NoError(t, os.WriteFile(completedFile, []byte("package completed"), 0o644))
		require.NoError(t, os.WriteFile(manualFile, []byte("package manual"), 0o644))

		// completedFile was selected via completion
		e := &editor{attachments: []attachment{{
			path:        completedFile,
			placeholder: "@" + completedFile,
			isTemp:      false,
		}}}
		// User typed manualFile and cursor left the word
		e.tryAddFileRef("@" + manualFile)

		require.Len(t, e.attachments, 2)

		// Verify both get expanded
		content := "compare @" + completedFile + " with @" + manualFile
		result := e.expandAllAttachments(content)

		assert.Contains(t, result, "package completed")
		assert.Contains(t, result, "package manual")
	})
}
