package config

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeEvent represents a configuration file change event
type ChangeEvent struct {
	Path      string
	Timestamp time.Time
}

// Watcher watches configuration files for changes
type Watcher struct {
	watcher       *fsnotify.Watcher
	events        chan ChangeEvent
	debounceTimer *time.Timer
	debounceMu    sync.Mutex
	watchedPath   string
	closed        bool
	closeMu       sync.RWMutex
}

const debounceDelay = 500 * time.Millisecond

// NewConfigWatcher creates a new configuration file watcher
func NewConfigWatcher() (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &Watcher{
		watcher: watcher,
		events:  make(chan ChangeEvent, 16),
	}, nil
}

// Watch starts watching the specified configuration file
func (cw *Watcher) Watch(path string) error {
	cw.closeMu.RLock()
	if cw.closed {
		cw.closeMu.RUnlock()
		return fmt.Errorf("watcher is closed")
	}
	cw.closeMu.RUnlock()

	// Get absolute path for consistent tracking
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Watch the directory containing the file (more reliable than watching file directly)
	dir := filepath.Dir(absPath)
	if err := cw.watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	cw.watchedPath = absPath
	slog.Debug("Started watching config file", "path", absPath)

	return nil
}

// Events returns the channel for receiving configuration change events
func (cw *Watcher) Events() <-chan ChangeEvent {
	return cw.events
}

// Start begins processing file system events
func (cw *Watcher) Start(ctx context.Context) {
	go cw.processEvents(ctx)
}

// processEvents handles file system events with debouncing
func (cw *Watcher) processEvents(ctx context.Context) {
	defer close(cw.events)

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Config watcher context cancelled")
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				slog.Debug("Config watcher events channel closed")
				return
			}

			// Check if this is the file we're watching
			eventPath, err := filepath.Abs(event.Name)
			if err != nil {
				slog.Warn("Failed to get absolute path for event", "path", event.Name, "error", err)
				continue
			}

			if eventPath != cw.watchedPath {
				continue
			}

			// Only process Write and Create events (common for file saves)
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			slog.Debug("Config file changed", "path", event.Name, "op", event.Op)

			// Debounce: only emit event after delay to handle multiple rapid writes
			cw.scheduleReload(eventPath)

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				slog.Debug("Config watcher errors channel closed")
				return
			}
			slog.Error("Config watcher error", "error", err)
		}
	}
}

// scheduleReload schedules a reload after the debounce delay
func (cw *Watcher) scheduleReload(path string) {
	cw.debounceMu.Lock()
	defer cw.debounceMu.Unlock()

	// Cancel previous timer if it exists
	if cw.debounceTimer != nil {
		cw.debounceTimer.Stop()
	}

	// Schedule new reload
	cw.debounceTimer = time.AfterFunc(debounceDelay, func() {
		cw.closeMu.RLock()
		defer cw.closeMu.RUnlock()

		if cw.closed {
			return
		}

		select {
		case cw.events <- ChangeEvent{
			Path:      path,
			Timestamp: time.Now(),
		}:
			slog.Debug("Config reload event emitted", "path", path)
		default:
			slog.Warn("Config reload event channel full, skipping event")
		}
	})
}

// Close stops the watcher and releases resources
func (cw *Watcher) Close() error {
	cw.closeMu.Lock()
	defer cw.closeMu.Unlock()

	if cw.closed {
		return nil
	}

	cw.closed = true

	// Stop debounce timer
	cw.debounceMu.Lock()
	if cw.debounceTimer != nil {
		cw.debounceTimer.Stop()
	}
	cw.debounceMu.Unlock()

	// Close watcher
	if err := cw.watcher.Close(); err != nil {
		return fmt.Errorf("failed to close file watcher: %w", err)
	}

	slog.Debug("Config watcher closed")
	return nil
}
