package animation

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetGlobalCoordinator(t *testing.T) {
	t.Helper()
	globalCoordinator.mu.Lock()
	globalCoordinator.active = 0
	globalCoordinator.frame = 0
	globalCoordinator.mu.Unlock()
}

func getActiveCount() int32 {
	globalCoordinator.mu.Lock()
	defer globalCoordinator.mu.Unlock()
	return globalCoordinator.active
}

func runCmdWithTimeout(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	require.NotNil(t, cmd)

	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	timeout := time.NewTimer(250 * time.Millisecond)
	defer timeout.Stop()

	select {
	case msg := <-done:
		return msg
	case <-timeout.C:
		t.Fatal("timed out waiting for tick command")
	}

	return nil
}

func runTickCmd(t *testing.T, cmd tea.Cmd) TickMsg {
	t.Helper()

	msg := runCmdWithTimeout(t, cmd)
	tickMsg, ok := msg.(TickMsg)
	require.True(t, ok)

	return tickMsg
}

func TestGlobalCoordinatorLifecycle(t *testing.T) {
	resetGlobalCoordinator(t)

	// No active animations = no tick
	require.Nil(t, StartTick())

	// First registration starts tick
	firstTick := StartTickIfFirst()
	tickMsg := runTickCmd(t, firstTick)
	assert.Equal(t, 1, tickMsg.Frame)

	// Subsequent tick continues
	nextTick := StartTick()
	tickMsg = runTickCmd(t, nextTick)
	assert.Equal(t, 2, tickMsg.Frame)

	// Second StartTickIfFirst registers but doesn't return tick (not first)
	cmd := StartTickIfFirst()
	require.Nil(t, cmd)
	assert.Equal(t, int32(2), getActiveCount())

	// Unregister one, still active
	Unregister()
	require.True(t, HasActive())
	require.NotNil(t, StartTick())

	// Unregister last one
	Unregister()
	require.False(t, HasActive())
	require.Nil(t, StartTick())
}

func TestUnregisterNeverGoesNegative(t *testing.T) {
	resetGlobalCoordinator(t)

	// Multiple unregisters when already at 0
	Unregister()
	Unregister()
	Unregister()

	assert.Equal(t, int32(0), getActiveCount())
	require.False(t, HasActive())
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	resetGlobalCoordinator(t)

	const goroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half goroutines do register
	for range goroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				Register()
			}
		}()
	}

	// Half goroutines do unregister
	for range goroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				Unregister()
			}
		}()
	}

	wg.Wait()

	// Should have exactly goroutines * opsPerGoroutine registers
	// minus whatever unregisters succeeded (capped at 0)
	// Final count should be >= 0
	count := getActiveCount()
	assert.GreaterOrEqual(t, count, int32(0), "active count should never be negative")
}

func TestConcurrentStartTickIfFirst(t *testing.T) {
	resetGlobalCoordinator(t)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	cmds := make(chan tea.Cmd, goroutines)

	// Many goroutines race to be "first"
	for range goroutines {
		go func() {
			defer wg.Done()
			cmd := StartTickIfFirst()
			cmds <- cmd
		}()
	}

	wg.Wait()
	close(cmds)

	// Count non-nil commands (ticks started)
	ticksStarted := 0
	for cmd := range cmds {
		if cmd != nil {
			ticksStarted++
		}
	}

	// Exactly one should have started the tick
	assert.Equal(t, 1, ticksStarted, "exactly one goroutine should start the tick")
	// All should have registered
	assert.Equal(t, int32(goroutines), getActiveCount())
}
