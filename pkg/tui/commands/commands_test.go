package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tui/messages"
)

func TestParseSlashCommand_Title(t *testing.T) {
	t.Parallel()

	t.Run("title with argument sets title", func(t *testing.T) {
		t.Parallel()

		cmd := ParseSlashCommand("/title My Custom Title")
		require.NotNil(t, cmd, "should return a command for /title with argument")

		// Execute the command and check the message type
		msg := cmd()
		setTitleMsg, ok := msg.(messages.SetSessionTitleMsg)
		require.True(t, ok, "should return SetSessionTitleMsg")
		assert.Equal(t, "My Custom Title", setTitleMsg.Title)
	})

	t.Run("title without argument regenerates", func(t *testing.T) {
		t.Parallel()

		cmd := ParseSlashCommand("/title")
		require.NotNil(t, cmd, "should return a command for /title without argument")

		// Execute the command and check the message type
		msg := cmd()
		_, ok := msg.(messages.RegenerateTitleMsg)
		assert.True(t, ok, "should return RegenerateTitleMsg")
	})

	t.Run("title with only whitespace regenerates", func(t *testing.T) {
		t.Parallel()

		cmd := ParseSlashCommand("/title   ")
		require.NotNil(t, cmd, "should return a command for /title with whitespace")

		// Execute the command and check the message type
		msg := cmd()
		_, ok := msg.(messages.RegenerateTitleMsg)
		assert.True(t, ok, "should return RegenerateTitleMsg for whitespace-only arg")
	})
}

func TestParseSlashCommand_OtherCommands(t *testing.T) {
	t.Parallel()

	t.Run("exit command", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/exit")
		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(messages.ExitSessionMsg)
		assert.True(t, ok)
	})

	t.Run("new command", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/new")
		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(messages.NewSessionMsg)
		assert.True(t, ok)
	})

	t.Run("star command", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/star")
		require.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(messages.ToggleSessionStarMsg)
		assert.True(t, ok)
	})

	t.Run("unknown command returns nil", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/unknown")
		assert.Nil(t, cmd)
	})

	t.Run("non-slash input returns nil", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("hello world")
		assert.Nil(t, cmd)
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("")
		assert.Nil(t, cmd)
	})
}

func TestParseSlashCommand_Compact(t *testing.T) {
	t.Parallel()

	t.Run("compact without argument", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/compact")
		require.NotNil(t, cmd)
		msg := cmd()
		compactMsg, ok := msg.(messages.CompactSessionMsg)
		require.True(t, ok)
		assert.Empty(t, compactMsg.AdditionalPrompt)
	})

	t.Run("compact with argument", func(t *testing.T) {
		t.Parallel()
		cmd := ParseSlashCommand("/compact focus on the API design")
		require.NotNil(t, cmd)
		msg := cmd()
		compactMsg, ok := msg.(messages.CompactSessionMsg)
		require.True(t, ok)
		assert.Equal(t, "focus on the API design", compactMsg.AdditionalPrompt)
	})
}
