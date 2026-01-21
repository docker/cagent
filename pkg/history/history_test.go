package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	assert.Equal(t, -1, h.current)
	assert.Empty(t, h.Messages)
}

func TestHistory_AddAndSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	messages := []string{"first", "second", "third"}
	for _, msg := range messages {
		err := h.Add(msg)
		require.NoError(t, err)
	}

	assert.Equal(t, messages, h.Messages)
	assert.Len(t, messages, h.current)

	h2, err := New()
	require.NoError(t, err)
	assert.Equal(t, messages, h2.Messages)
}

func TestHistory_Navigation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	assert.Empty(t, h.Previous())
	assert.Empty(t, h.Next())

	messages := []string{"first", "second", "third"}
	for _, msg := range messages {
		require.NoError(t, h.Add(msg))
	}

	assert.Equal(t, "third", h.Previous())
	assert.Equal(t, "second", h.Previous())
	assert.Equal(t, "first", h.Previous())
	assert.Equal(t, "first", h.Previous())

	assert.Equal(t, "second", h.Next())
	assert.Equal(t, "third", h.Next())
	assert.Empty(t, h.Next())
}

func TestHistory_EdgeCases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	assert.Empty(t, h.Previous())
	assert.Empty(t, h.Next())

	require.NoError(t, h.Add("only"))

	assert.Equal(t, "only", h.Previous())
	assert.Equal(t, "only", h.Previous()) // Should stay at the beginning
	assert.Empty(t, h.Next())             // Should return empty when going past the end
}

func TestHistory_StayAtTheBeginning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	require.NoError(t, h.Add("first"))

	assert.Equal(t, "first", h.Previous())
	assert.Equal(t, "first", h.Previous())
}

func TestHistory_NoDuplicateMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	require.NoError(t, h.Add("first"))
	require.NoError(t, h.Add("second"))
	require.NoError(t, h.Add("second"))

	assert.Equal(t, "second", h.Previous())
	assert.Equal(t, "first", h.Previous())
	assert.Equal(t, "first", h.Previous())
}

func TestHistory_MoveDuplicateLast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h, err := New()
	require.NoError(t, err)

	require.NoError(t, h.Add("first"))
	require.NoError(t, h.Add("second"))
	require.NoError(t, h.Add("third"))
	require.NoError(t, h.Add("first"))

	assert.Equal(t, "first", h.Previous())
	assert.Equal(t, "third", h.Previous())
	assert.Equal(t, "second", h.Previous())
	assert.Equal(t, "second", h.Previous())
}

func TestHistory_MultilineMessage(t *testing.T) {
	tmpDir := t.TempDir()

	h, err := New(WithBaseDir(tmpDir))
	require.NoError(t, err)

	multiline := "line1\nline2\nline3"
	require.NoError(t, h.Add(multiline))

	h2, err := New(WithBaseDir(tmpDir))
	require.NoError(t, err)

	require.Len(t, h2.Messages, 1)
	assert.Equal(t, multiline, h2.Messages[0])
}

func TestHistory_MigrateOldFormat(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, ".cagent"), 0o755)
	require.NoError(t, err)
	oldHistFile := filepath.Join(tmpDir, ".cagent", "history.json")
	newHistFile := filepath.Join(tmpDir, ".cagent", "history")

	require.NoError(t, os.WriteFile(oldHistFile, []byte(`{"messages":["old1","old2","old3"]}`), 0o644))

	h, err := New(WithBaseDir(tmpDir))
	require.NoError(t, err)
	assert.Equal(t, []string{"old1", "old2", "old3"}, h.Messages)

	_, err = os.Stat(oldHistFile)
	assert.True(t, os.IsNotExist(err), "old history.json should be removed")

	_, err = os.Stat(newHistFile)
	assert.NoError(t, err, "new history file should exist")
}

func TestHistory_LatestMatch(t *testing.T) {
	tmpDir := t.TempDir()

	h, err := New(WithBaseDir(tmpDir))
	require.NoError(t, err)

	// Empty history returns empty string
	assert.Empty(t, h.LatestMatch(""))
	assert.Empty(t, h.LatestMatch("prefix"))

	// Add some messages
	require.NoError(t, h.Add("hello world"))
	require.NoError(t, h.Add("hello there"))
	require.NoError(t, h.Add("goodbye"))

	// Empty prefix returns latest message
	assert.Equal(t, "goodbye", h.LatestMatch(""))

	// Prefix matching returns latest match
	assert.Equal(t, "hello there", h.LatestMatch("hello"))
	assert.Equal(t, "goodbye", h.LatestMatch("good"))

	// No match returns empty string
	assert.Empty(t, h.LatestMatch("xyz"))

	// Exact match doesn't count (must extend prefix)
	assert.Empty(t, h.LatestMatch("goodbye"))
}
