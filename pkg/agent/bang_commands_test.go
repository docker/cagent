package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnableBangCommands(t *testing.T) {
	t.Run("default is false", func(t *testing.T) {
		a := New("test", "test prompt")
		assert.False(t, a.EnableBangCommands())
	})

	t.Run("can enable bang commands", func(t *testing.T) {
		a := New("test", "test prompt", WithEnableBangCommands(true))
		assert.True(t, a.EnableBangCommands())
	})

	t.Run("can explicitly disable bang commands", func(t *testing.T) {
		a := New("test", "test prompt", WithEnableBangCommands(false))
		assert.False(t, a.EnableBangCommands())
	})
}
