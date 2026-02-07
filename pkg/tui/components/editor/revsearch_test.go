package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
)

func TestReverseSearch(t *testing.T) {
	t.Parallel()

	setupEditor := func(t *testing.T, messages []string) *editor {
		t.Helper()
		tmpDir := t.TempDir()
		h, err := history.New(history.WithBaseDir(tmpDir))
		require.NoError(t, err)

		for _, msg := range messages {
			require.NoError(t, h.Add(msg))
		}

		e := New(&app.App{}, h).(*editor)
		e.textarea.SetWidth(80)
		return e
	}

	press := func(t *testing.T, e *editor, msg tea.Msg) *editor {
		t.Helper()
		m, _ := e.Update(msg)
		return m.(*editor)
	}

	ctrlR := tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	backspace := tea.KeyPressMsg{Code: tea.KeyBackspace}

	typeStr := func(t *testing.T, e *editor, s string) *editor {
		t.Helper()
		for _, r := range s {
			e = press(t, e, tea.KeyPressMsg{Text: string(r)})
		}
		return e
	}

	t.Run("enter and exit search mode", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"cmd1", "cmd2"})

		assert.False(t, e.revSearchActive)

		e = press(t, e, ctrlR)
		assert.True(t, e.revSearchActive)
		assert.Empty(t, e.revSearchQuery)
		assert.Equal(t, "cmd2", e.revSearchMatch)

		e = press(t, e, esc)
		assert.False(t, e.revSearchActive)
		assert.Empty(t, e.Value())
	})

	t.Run("search query matching", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"deploy staging", "run tests", "deploy production"})

		e = press(t, e, ctrlR)
		assert.Equal(t, "deploy production", e.revSearchMatch)

		e = typeStr(t, e, "te")
		assert.Equal(t, "te", e.revSearchQuery)
		assert.Equal(t, "run tests", e.revSearchMatch)
		assert.False(t, e.revSearchFailing)

		e = press(t, e, backspace)
		assert.Equal(t, "t", e.revSearchQuery)
		assert.Equal(t, "deploy production", e.revSearchMatch)
	})

	t.Run("cycling matches with Ctrl+R", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"echo 1", "echo 2", "echo 3"})

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "echo")
		assert.Equal(t, "echo 3", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.Equal(t, "echo 2", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.Equal(t, "echo 1", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.True(t, e.revSearchFailing)
		assert.Equal(t, "echo 1", e.revSearchMatch)
	})

	t.Run("accept match", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"cmd1", "cmd2", "cmd3"})
		e.SetValue("partial input")

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "cmd2")
		assert.Equal(t, "cmd2", e.revSearchMatch)

		e = press(t, e, enter)
		assert.False(t, e.revSearchActive)
		assert.Equal(t, "cmd2", e.Value())

		e = press(t, e, up)
		assert.Equal(t, "cmd1", e.Value())

		e = press(t, e, down)
		assert.Equal(t, "cmd2", e.Value())
		e = press(t, e, down)
		assert.Equal(t, "cmd3", e.Value())
	})

	t.Run("cancel restores original input", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"history"})
		e.SetValue("original input")

		e = press(t, e, ctrlR)
		assert.Equal(t, "history", e.textarea.Value())

		e = press(t, e, esc)
		assert.False(t, e.revSearchActive)
		assert.Equal(t, "original input", e.textarea.Value())
	})

	t.Run("failing search status", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"foo"})

		e = press(t, e, ctrlR)
		assert.False(t, e.revSearchFailing)

		e = typeStr(t, e, "z")
		assert.True(t, e.revSearchFailing)
		assert.Equal(t, "foo", e.textarea.Value())
	})

	t.Run("empty history", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{})

		e = press(t, e, ctrlR)
		assert.True(t, e.revSearchActive)
		assert.True(t, e.revSearchFailing)
		assert.Empty(t, e.revSearchMatch)
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"Deploy Staging", "run tests"})

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "deploy")
		assert.Equal(t, "Deploy Staging", e.revSearchMatch)
		assert.False(t, e.revSearchFailing)
	})

	t.Run("cycling with empty query", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"first", "second", "third"})

		e = press(t, e, ctrlR)
		assert.Equal(t, "third", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.Equal(t, "second", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.Equal(t, "first", e.revSearchMatch)

		e = press(t, e, ctrlR)
		assert.True(t, e.revSearchFailing)
	})

	t.Run("backspace when query is empty", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"foo"})

		e = press(t, e, ctrlR)
		matchBefore := e.revSearchMatch
		failingBefore := e.revSearchFailing

		e = press(t, e, backspace)
		assert.Empty(t, e.revSearchQuery)
		assert.Equal(t, matchBefore, e.revSearchMatch)
		assert.Equal(t, failingBefore, e.revSearchFailing)
	})

	t.Run("enter while failing accepts last valid match", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"foo", "bar"})
		e.SetValue("original")

		e = press(t, e, ctrlR)
		assert.Equal(t, "bar", e.revSearchMatch)

		e = typeStr(t, e, "zzz")
		assert.True(t, e.revSearchFailing)

		e = press(t, e, enter)
		assert.False(t, e.revSearchActive)
		assert.Equal(t, "bar", e.Value())
	})

	t.Run("cancel does not change history pointer", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"first", "second", "third"})

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "first")
		assert.Equal(t, "first", e.revSearchMatch)

		e = press(t, e, esc)
		assert.False(t, e.revSearchActive)

		e = press(t, e, up)
		assert.Equal(t, "third", e.Value())
	})

	t.Run("state fully reset on exit", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"hello"})

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "hel")

		e = press(t, e, enter)
		assert.False(t, e.revSearchActive)
		assert.Empty(t, e.revSearchQuery)
		assert.Empty(t, e.revSearchMatch)
		assert.Equal(t, -1, e.revSearchMatchIndex)
		assert.False(t, e.revSearchFailing)
		assert.Empty(t, e.revSearchOrigValue)
	})

	t.Run("re-enter search after exiting", func(t *testing.T) {
		t.Parallel()
		e := setupEditor(t, []string{"aaa", "bbb"})

		e = press(t, e, ctrlR)
		e = typeStr(t, e, "aaa")
		e = press(t, e, enter)
		assert.Equal(t, "aaa", e.Value())

		e = press(t, e, ctrlR)
		assert.True(t, e.revSearchActive)
		assert.Empty(t, e.revSearchQuery)
		assert.Equal(t, "aaa", e.revSearchOrigValue)
		assert.Equal(t, "bbb", e.revSearchMatch)
	})
}
