package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonblockingChannelReplacementWatcher(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../pkg/gitbranch/watcher.go")
	require.NoError(t, err)
	// Keep the actual method while avoiding the unrelated fsnotify dependency.
	_, method, ok := strings.Cut(string(src), "func (w *Watcher) refresh()")
	require.True(t, ok)
	offenses := coptest.RunProgram(t, registeredSharedCop(t, programCops, "Lint/NonblockingChannelReplacement"), coptest.ProgramFiles{
		"gitbranch/watcher.go": `package gitbranch
import "sync"
type Watcher struct { mu sync.Mutex; dir, current string; changes chan string }
func Current(string) string { return "" }
func (w *Watcher) refresh()` + method,
	})
	assert.Empty(t, offenses)
}
