package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPICommand_ExitOnStdinEOFFlag(t *testing.T) {
	t.Parallel()

	t.Run("flag exists and defaults to false", func(t *testing.T) {
		t.Parallel()

		cmd := newAPICmd()

		flag := cmd.PersistentFlags().Lookup("exit-on-stdin-eof")
		require.NotNil(t, flag, "exit-on-stdin-eof flag should exist")
		assert.Equal(t, "false", flag.DefValue, "exit-on-stdin-eof should default to false")
	})

	t.Run("flag is hidden", func(t *testing.T) {
		t.Parallel()

		cmd := newAPICmd()

		flag := cmd.PersistentFlags().Lookup("exit-on-stdin-eof")
		require.NotNil(t, flag, "exit-on-stdin-eof flag should exist")
		assert.True(t, flag.Hidden, "exit-on-stdin-eof flag should be hidden")
	})

	t.Run("flag can be set to true", func(t *testing.T) {
		t.Parallel()

		cmd := newAPICmd()

		err := cmd.PersistentFlags().Set("exit-on-stdin-eof", "true")
		require.NoError(t, err)

		flag := cmd.PersistentFlags().Lookup("exit-on-stdin-eof")
		require.NotNil(t, flag)
		assert.Equal(t, "true", flag.Value.String())
	})
}
