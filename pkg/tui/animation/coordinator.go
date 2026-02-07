// Package animation provides centralized animation tick management for the TUI.
// All animated components (spinners, fades, etc.) share a single tick stream
// to avoid tick storms and ensure synchronized animations.
//
// Thread safety: All exported functions are safe for concurrent use, though the
// typical usage pattern is single-threaded via Bubble Tea's Update loop.
package animation

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TickMsg is broadcast to all animated components on each animation frame.
// Components should handle this message to update their animation state.
type TickMsg struct {
	Frame int
}

// Coordinator manages a single tick stream for all animations.
// It tracks active animations and only generates ticks when at least one is active.
type Coordinator struct {
	// mu guards all fields. While Bubble Tea's Update loop is single-threaded,
	// the mutex protects against accidental misuse from Cmd goroutines and
	// ensures StartTickIfFirst is atomic (no race between check and register).
	mu     sync.Mutex
	frame  int
	active int32
}

// globalCoordinator is the singleton coordinator instance.
var globalCoordinator = &Coordinator{}

// Register increments the active animation count.
// Call this when an animation starts.
func Register() {
	globalCoordinator.mu.Lock()
	globalCoordinator.active++
	globalCoordinator.mu.Unlock()
}

// Unregister decrements the active animation count.
// Call this when an animation stops.
func Unregister() {
	globalCoordinator.mu.Lock()
	if globalCoordinator.active > 0 {
		globalCoordinator.active--
	}
	globalCoordinator.mu.Unlock()
}

// HasActive returns true if any animations are currently active.
func HasActive() bool {
	globalCoordinator.mu.Lock()
	active := globalCoordinator.active > 0
	globalCoordinator.mu.Unlock()
	return active
}

// StartTick starts the global animation tick if any animations are active.
// Call this after processing a TickMsg to continue the tick stream.
func StartTick() tea.Cmd {
	globalCoordinator.mu.Lock()
	defer globalCoordinator.mu.Unlock()
	if globalCoordinator.active <= 0 {
		return nil
	}
	return globalCoordinator.tickLocked()
}

// StartTickIfFirst registers an animation and starts the tick if this is the first.
// This is atomic: no race between checking and registering.
// Returns the tick command if the tick stream was started, nil otherwise.
func StartTickIfFirst() tea.Cmd {
	globalCoordinator.mu.Lock()
	defer globalCoordinator.mu.Unlock()
	wasEmpty := globalCoordinator.active == 0
	globalCoordinator.active++
	if wasEmpty {
		return globalCoordinator.tickLocked()
	}
	return nil
}

// tickLocked returns a tick command. Must be called with mu held.
// 14 FPS - smooth enough for most animations without being too CPU-intensive.
func (c *Coordinator) tickLocked() tea.Cmd {
	return tea.Tick(time.Second/14, func(time.Time) tea.Msg {
		c.mu.Lock()
		c.frame++
		frame := c.frame
		c.mu.Unlock()
		return TickMsg{Frame: frame}
	})
}
