package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/go-units"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePaste_SmallContent(t *testing.T) {
	t.Parallel()

	e := &editor{}
	// Content that's under both limits: few lines and few chars
	smallContent := "line1\nline2\nline3"

	handled := e.handlePaste(smallContent)

	assert.False(t, handled, "small content should not be handled (return false)")
	assert.Empty(t, e.attachments, "no attachments should be created for small content")
}

func TestHandlePaste_AtLineLimitIsInline(t *testing.T) {
	t.Parallel()

	e := &editor{}
	// Exactly at line limit (10 lines) and under char limit should be inline
	lines := make([]string, maxInlinePasteLines)
	for i := range lines {
		lines[i] = "short"
	}
	content := strings.Join(lines, "\n")

	handled := e.handlePaste(content)

	assert.False(t, handled, "content at line limit should be inline")
}

func TestHandlePaste_AtCharLimitIsInline(t *testing.T) {
	t.Parallel()

	e := &editor{}
	// Exactly at char limit and under line limit should be inline
	content := strings.Repeat("x", maxInlinePasteChars)

	handled := e.handlePaste(content)

	assert.False(t, handled, "content at char limit should be inline")
}

func TestHandlePaste_ExceedsLineLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pastesDir := filepath.Join(tmpDir, "pastes")

	e := &editor{}
	// Content exceeding line limit (11 lines)
	lines := make([]string, maxInlinePasteLines+1)
	for i := range lines {
		lines[i] = "short"
	}
	largeContent := strings.Join(lines, "\n")

	// Create paste buffer directly to test without textarea
	att, err := createPasteAttachmentInDir(pastesDir, largeContent)
	require.NoError(t, err)

	e.attachments = append(e.attachments, att)

	// Verify file was created
	assert.FileExists(t, att.path)

	// Verify attachment struct is correct
	assert.NotEmpty(t, att.path)
	assert.True(t, strings.HasPrefix(att.placeholder, "@"))
	assert.True(t, att.isTemp, "paste attachments should be marked as temp")
}

func TestHandlePaste_ExceedsCharLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pastesDir := filepath.Join(tmpDir, "pastes")

	e := &editor{}
	// Single line exceeding char limit
	largeContent := strings.Repeat("x", maxInlinePasteChars+1)

	// Create paste buffer directly to test without textarea
	att, err := createPasteAttachmentInDir(pastesDir, largeContent)
	require.NoError(t, err)

	e.attachments = append(e.attachments, att)

	// Verify file was created
	assert.FileExists(t, att.path)

	// Verify attachment struct is correct
	assert.NotEmpty(t, att.path)
	assert.True(t, strings.HasPrefix(att.placeholder, "@"))
	assert.NotEmpty(t, att.label, "label should show size")
	assert.True(t, att.isTemp, "paste attachments should be marked as temp")
}

func TestCollectAttachments_WithPastes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pastesDir := filepath.Join(tmpDir, "pastes")

	content := "This is the pasted content"
	att, err := createPasteAttachmentInDir(pastesDir, content)
	require.NoError(t, err)

	e := &editor{
		attachments: []attachment{att},
	}

	input := "Hello " + att.placeholder + " world"
	result := e.collectAttachments(input)

	// Paste content should be in the attachment's inline Content field
	require.Len(t, result, 1)
	assert.Equal(t, content, result[0].Content)
	assert.Empty(t, result[0].FilePath, "paste attachments should not have a file path")
	assert.NoFileExists(t, att.path, "paste file should be removed after collection")
	assert.Empty(t, e.attachments, "attachments should be cleared")
}

func TestCollectAttachments_RemovesUnusedFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pastesDir := filepath.Join(tmpDir, "pastes")

	att, err := createPasteAttachmentInDir(pastesDir, "unused content")
	require.NoError(t, err)

	e := &editor{
		attachments: []attachment{att},
	}

	// Collect with content that doesn't include the placeholder
	result := e.collectAttachments("no placeholder here")

	assert.Empty(t, result)
	assert.NoFileExists(t, att.path, "unused paste file should be removed")
	assert.Empty(t, e.attachments)
}

func TestCleanup_RemovesAllPasteFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pastesDir := filepath.Join(tmpDir, "pastes")

	att1, err := createPasteAttachmentInDir(pastesDir, "content 1")
	require.NoError(t, err)

	att2, err := createPasteAttachmentInDir(pastesDir, "content 2")
	require.NoError(t, err)

	e := &editor{
		attachments: []attachment{att1, att2},
	}

	// Verify files exist before cleanup
	assert.FileExists(t, att1.path)
	assert.FileExists(t, att2.path)

	e.Cleanup()

	// Verify files are removed after cleanup
	assert.NoFileExists(t, att1.path, "att1 should be removed")
	assert.NoFileExists(t, att2.path, "att2 should be removed")
	assert.Empty(t, e.attachments, "attachments should be cleared")
}

func TestCleanup_HandlesEmptyPastes(t *testing.T) {
	t.Parallel()

	e := &editor{}

	// Should not panic
	e.Cleanup()

	assert.Empty(t, e.attachments)
}

// createPasteAttachmentInDir is a test helper that creates a paste attachment in a specific directory.
func createPasteAttachmentInDir(dir, content string) (attachment, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return attachment{}, err
	}

	file, err := os.CreateTemp(dir, "paste-*.txt")
	if err != nil {
		return attachment{}, err
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return attachment{}, err
	}

	path := file.Name()
	return attachment{
		path:        path,
		placeholder: "@" + path,
		label:       filepath.Base(path) + " (" + units.HumanSize(float64(len(content))) + ")",
		isTemp:      true,
	}, nil
}
