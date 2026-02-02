package styles

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ThemeWatcher watches the current theme file for changes and signals
// when the file is modified. It does NOT apply the theme directly to avoid
// race conditions with the TUI goroutine.
type ThemeWatcher struct {
	mu              sync.Mutex
	watcher         *fsnotify.Watcher
	currentPath     string
	currentThemeRef string
	stopChan        chan struct{}
	onThemeChanged  func(themeRef string) // Callback when theme file changes (themeRef included)

	// themesDir can be set for testing to override the default ThemesDir()
	themesDir string
}

// NewThemeWatcher creates a new theme watcher.
// The onThemeChanged callback is called whenever the theme file is modified.
// The callback receives the theme ref so the caller can load and apply the theme
// on the appropriate goroutine (e.g., the TUI main loop).
func NewThemeWatcher(onThemeChanged func(themeRef string)) *ThemeWatcher {
	return &ThemeWatcher{
		onThemeChanged: onThemeChanged,
	}
}

// Watch starts watching the theme file for the given theme reference.
// Only watches if a user theme file exists for this ref (in ~/.cagent/themes/).
// Handles "user:" prefix - e.g., "user:nord" watches ~/.cagent/themes/nord.yaml.
// If the theme is the built-in default or no user file exists, no watching occurs.
func (tw *ThemeWatcher) Watch(themeRef string) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Stop existing watcher if any
	tw.stopLocked()

	// Don't watch the built-in default (it's embedded, no file to watch)
	// Also handle edge case of "user:default"
	baseRef := strings.TrimPrefix(themeRef, UserThemePrefix)
	if baseRef == DefaultThemeRef && !strings.HasPrefix(themeRef, UserThemePrefix) {
		slog.Debug("Not watching built-in default theme", "theme", themeRef)
		return nil
	}

	// Try to find a user theme file - only watch if one exists
	// (even if there's a built-in with the same name, user can override it)
	themePath, err := tw.findThemePath(themeRef)
	if err != nil {
		slog.Debug("No user theme file found, not watching", "theme", themeRef)
		return nil // Not an error - theme might be built-in only
	}

	// Create the watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the directory containing the theme file (more reliable for editors that
	// do atomic saves by writing to a temp file and renaming)
	dir := filepath.Dir(themePath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return err
	}

	tw.watcher = watcher
	tw.currentPath = themePath
	tw.currentThemeRef = themeRef
	tw.stopChan = make(chan struct{})

	go tw.watchLoop()

	slog.Debug("Started watching theme file", "theme", themeRef, "path", themePath)
	return nil
}

// Stop stops watching the current theme file.
func (tw *ThemeWatcher) Stop() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.stopLocked()
}

func (tw *ThemeWatcher) stopLocked() {
	if tw.stopChan != nil {
		close(tw.stopChan)
		tw.stopChan = nil
	}
	if tw.watcher != nil {
		tw.watcher.Close()
		tw.watcher = nil
	}
	tw.currentPath = ""
	tw.currentThemeRef = ""
}

func (tw *ThemeWatcher) getThemesDir() string {
	if tw.themesDir != "" {
		return tw.themesDir
	}
	return ThemesDir()
}

func (tw *ThemeWatcher) findThemePath(themeRef string) (string, error) {
	dir := tw.getThemesDir()

	// Strip user: prefix if present to get the base filename
	baseRef := strings.TrimPrefix(themeRef, UserThemePrefix)

	// Try .yaml first, then .yml
	yamlPath := filepath.Join(dir, baseRef+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath, nil
	}

	ymlPath := filepath.Join(dir, baseRef+".yml")
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath, nil
	}

	return "", os.ErrNotExist
}

func (tw *ThemeWatcher) watchLoop() {
	// Debounce timer to handle rapid successive events (e.g., editor save operations)
	var debounceTimer *time.Timer
	debounceDuration := 500 * time.Millisecond

	tw.mu.Lock()
	watcher := tw.watcher
	stopChan := tw.stopChan
	tw.mu.Unlock()

	if watcher == nil {
		return
	}

	for {
		select {
		case <-stopChan:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			tw.mu.Lock()
			currentPath := tw.currentPath
			tw.mu.Unlock()

			// Check if this event might affect our theme file.
			// Some editors use atomic saves (write to temp, then rename), so we need to handle:
			// - Write/Create on exact path (direct save)
			// - Rename events where the target becomes our file
			// - Any event with matching basename (covers temp file renames)
			eventPath := filepath.Clean(event.Name)
			targetPath := filepath.Clean(currentPath)

			isExactMatch := eventPath == targetPath
			isBasenameMatch := filepath.Base(eventPath) == filepath.Base(targetPath)

			// React to Write, Create, Rename, or Remove events
			// - Write/Create: direct modifications
			// - Rename: atomic save patterns (temp file renamed to target)
			// - Remove: file deleted then recreated
			relevantOp := event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0

			if !relevantOp {
				continue
			}

			// For exact matches, always trigger
			// For basename matches with Rename/Create, also trigger (catches atomic saves)
			if !isExactMatch && (!isBasenameMatch || event.Op&(fsnotify.Rename|fsnotify.Create) == 0) {
				continue
			}

			// Debounce: reset timer on each event
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDuration, func() {
				// After debounce, verify the file still exists before signaling
				tw.mu.Lock()
				path := tw.currentPath
				tw.mu.Unlock()
				if _, err := os.Stat(path); err == nil {
					tw.signalThemeChange()
				}
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("Theme file watcher error", "error", err)
		}
	}
}

// signalThemeChange signals that the theme file has been modified.
// It does NOT load or apply the theme - that's the caller's responsibility
// to avoid race conditions with global style variables.
func (tw *ThemeWatcher) signalThemeChange() {
	tw.mu.Lock()
	themeRef := tw.currentThemeRef
	tw.mu.Unlock()

	if themeRef == "" {
		return
	}

	slog.Debug("Theme file changed, signaling", "theme", themeRef)

	// Call the callback with the theme ref - caller will load and apply
	if tw.onThemeChanged != nil {
		tw.onThemeChanged(themeRef)
	}
}
