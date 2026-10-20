package styles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runThemeWatcher(t *testing.T, watcher *ThemeWatcher, themeRef string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- watcher.Run(ctx, themeRef)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		waitThemeWatcher(t, done)
	})
	return cancel, done
}

func waitThemeWatcher(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("theme watcher did not return")
	}
}

func TestThemeWatcher_RunNoop(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{DefaultThemeRef, "missing", "user:missing"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte("ignored"), 0o644))
			watcher := &ThemeWatcher{themesDir: dir, onThemeChanged: func(string) {
				t.Error("unexpected theme change")
			}}
			_, done := runThemeWatcher(t, watcher, ref)
			waitThemeWatcher(t, done)
		})
	}
}

func TestThemeWatcher_RunCanceledContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), nil, 0o644))
	watcher := &ThemeWatcher{themesDir: dir}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, watcher.Run(ctx, "test"))
}

func TestThemeWatcher_RunFileChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ref  string
		file string
	}{
		{name: "user theme", ref: "user:test", file: "test.yaml"},
		{name: "yml extension", ref: "test", file: "test.yml"},
		{name: "builtin override", ref: "tokyo-night", file: "tokyo-night.yaml"},
		{name: "explicit user default", ref: "user:default", file: "default.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.file)
			require.NoError(t, os.WriteFile(path, []byte("initial"), 0o644))
			refs := make(chan string, 10)
			watcher := NewThemeWatcher(func(ref string) { refs <- ref })
			watcher.themesDir = dir
			cancel, done := runThemeWatcher(t, watcher, tc.ref)

			// Repeat writes until Run has attached its watcher; invalid YAML also signals.
			require.Eventually(t, func() bool {
				require.NoError(t, os.WriteFile(path, []byte("this is: [not: valid yaml"), 0o644))
				select {
				case ref := <-refs:
					assert.Equal(t, tc.ref, ref)
					return true
				default:
					return false
				}
			}, 5*time.Second, 700*time.Millisecond)

			cancel()
			waitThemeWatcher(t, done)
		})
	}
}

func TestThemeWatcher_ConcurrentRuns(t *testing.T) {
	t.Parallel()
	for _, sameTheme := range []bool{false, true} {
		name := "different themes"
		if sameTheme {
			name = "same theme"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			firstPath := filepath.Join(dir, "first.yaml")
			secondPath := filepath.Join(dir, "second.yaml")
			require.NoError(t, os.WriteFile(firstPath, nil, 0o644))
			require.NoError(t, os.WriteFile(secondPath, nil, 0o644))
			secondRef := "second"
			if sameTheme {
				secondRef = "first"
			}
			refs := make(chan string, 20)
			watcher := NewThemeWatcher(func(ref string) { refs <- ref })
			watcher.themesDir = dir
			cancelFirst, firstDone := runThemeWatcher(t, watcher, "first")
			_, secondDone := runThemeWatcher(t, watcher, secondRef)

			seen := make(map[string]int)
			require.Eventually(t, func() bool {
				require.NoError(t, os.WriteFile(firstPath, []byte("first"), 0o644))
				require.NoError(t, os.WriteFile(secondPath, []byte("second"), 0o644))
				for {
					select {
					case ref := <-refs:
						seen[ref]++
					default:
						if sameTheme {
							return seen["first"] >= 2
						}
						return seen["first"] > 0 && seen["second"] > 0
					}
				}
			}, 5*time.Second, 700*time.Millisecond)

			cancelFirst()
			waitThemeWatcher(t, firstDone)
			select {
			case err := <-secondDone:
				t.Fatalf("canceling the first run stopped the second: %v", err)
			default:
			}
		drain:
			for {
				select {
				case <-refs:
				default:
					break drain
				}
			}
			remainingPath := secondPath
			if sameTheme {
				remainingPath = firstPath
			}
			require.Eventually(t, func() bool {
				require.NoError(t, os.WriteFile(remainingPath, []byte("still watching"), 0o644))
				select {
				case ref := <-refs:
					assert.Equal(t, secondRef, ref)
					return true
				default:
					return false
				}
			}, 5*time.Second, 700*time.Millisecond)
		})
	}
}

func TestThemeWatcher_Debounce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.yaml")
		require.NoError(t, os.WriteFile(path, nil, 0o644))
		refs := make(chan string, 2)
		watcher := NewThemeWatcher(func(ref string) { refs <- ref })
		events := make(chan fsnotify.Event)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go watcher.watchLoop(ctx, "user:test", path, &fsnotify.Watcher{Events: events})

		for range 3 {
			events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
			time.Sleep(400 * time.Millisecond) //nolint:forbidigo // Advance the synctest clock.
			assert.Empty(t, refs)
		}
		time.Sleep(100 * time.Millisecond) //nolint:forbidigo // Advance the synctest clock.
		synctest.Wait()
		require.Len(t, refs, 1)
		assert.Equal(t, "user:test", <-refs)

		events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
		cancel()
		synctest.Wait()
		time.Sleep(time.Second) //nolint:forbidigo // Pending debounce must not outlive cancellation.
		assert.Empty(t, refs)
	})
}

func TestThemeWatcher_Events(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		op      fsnotify.Op
		other   bool
		deleted bool
		signal  bool
	}{
		{name: "write", op: fsnotify.Write, signal: true},
		{name: "atomic save", op: fsnotify.Create, signal: true},
		{name: "rename", op: fsnotify.Rename, signal: true},
		{name: "delete and recreate", op: fsnotify.Remove, signal: true},
		{name: "deleted", op: fsnotify.Remove, deleted: true},
		{name: "unrelated file", op: fsnotify.Write, other: true},
		{name: "unrelated creation", op: fsnotify.Create, other: true},
		{name: "chmod", op: fsnotify.Chmod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "test.yaml")
				if !tc.deleted {
					require.NoError(t, os.WriteFile(path, nil, 0o644))
				}
				var refs []string
				watcher := NewThemeWatcher(func(ref string) { refs = append(refs, ref) })
				events := make(chan fsnotify.Event)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go watcher.watchLoop(ctx, "test", path, &fsnotify.Watcher{Events: events})
				eventPath := path
				if tc.other {
					eventPath = filepath.Join(filepath.Dir(path), "other.yaml")
				}
				events <- fsnotify.Event{Name: eventPath, Op: tc.op}
				time.Sleep(time.Second) //nolint:forbidigo // Advance past the debounce in fake time.
				synctest.Wait()
				if tc.signal {
					assert.Equal(t, []string{"test"}, refs)
				} else {
					assert.Empty(t, refs)
				}
			})
		})
	}
}
