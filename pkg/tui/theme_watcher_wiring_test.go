package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/components/statusbar"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

type themeWatchRun struct {
	ctx  context.Context //nolint:containedctx // Captures the run context for cancellation assertions.
	ref  string
	done chan struct{}
}

type fakeThemeWatcher struct {
	runs chan themeWatchRun
}

func (f *fakeThemeWatcher) Run(ctx context.Context, themeRef string) error {
	done := make(chan struct{})
	defer close(done)
	f.runs <- themeWatchRun{ctx: ctx, ref: themeRef, done: done}
	<-ctx.Done()
	return nil
}

func setupThemeWatcher(t *testing.T, m *appModel) *fakeThemeWatcher {
	t.Helper()
	m.themeCtx, m.cancelThemes = context.WithCancel(t.Context())
	t.Cleanup(m.cleanupManagedResources)
	fw := &fakeThemeWatcher{runs: make(chan themeWatchRun, 10)}
	m.themeWatcher = fw
	return fw
}

func nextThemeWatchRun(t *testing.T, fw *fakeThemeWatcher) themeWatchRun {
	t.Helper()
	select {
	case run := <-fw.runs:
		return run
	case <-time.After(5 * time.Second):
		t.Fatal("theme watcher did not run")
		return themeWatchRun{}
	}
}

func waitThemeWatchRun(t *testing.T, run themeWatchRun) {
	t.Helper()
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Fatal("theme watcher did not stop")
	}
	assert.ErrorIs(t, run.ctx.Err(), context.Canceled)
}

func TestWatchCurrentTheme_TargetsAppliedTheme(t *testing.T) {
	m, _ := newTestModel(t)
	fw := setupThemeWatcher(t, m)

	m.watchCurrentTheme()

	assert.Equal(t, styles.CurrentTheme().Ref, nextThemeWatchRun(t, fw).ref)
}

func TestWatchCurrentTheme_NoWatcherIsNoop(t *testing.T) {
	m, _ := newTestModel(t)

	assert.NotPanics(t, func() { m.watchCurrentTheme() })
}

func TestApplyThemeChanged_RetargetsWatcher(t *testing.T) {
	m, _ := newTestModel(t)
	m.statusBar = statusbar.New(m)
	fw := setupThemeWatcher(t, m)
	m.watchCurrentTheme()
	first := nextThemeWatchRun(t, fw)

	_, _ = m.applyThemeChanged()

	second := nextThemeWatchRun(t, fw)
	assert.Equal(t, styles.CurrentTheme().Ref, second.ref)
	waitThemeWatchRun(t, first)
	require.NoError(t, second.ctx.Err())
}

func TestCleanupManagedResources_CancelsThemeWatcher(t *testing.T) {
	m, _ := newTestModel(t)
	fw := setupThemeWatcher(t, m)
	m.watchCurrentTheme()
	run := nextThemeWatchRun(t, fw)

	m.cleanupManagedResources()

	waitThemeWatchRun(t, run)

	// A late theme change must not outlive shutdown either.
	m.watchCurrentTheme()
	waitThemeWatchRun(t, nextThemeWatchRun(t, fw))
}

func TestWatchCurrentTheme_ConcurrentShutdown(t *testing.T) {
	m, _ := newTestModel(t)
	fw := setupThemeWatcher(t, m)
	var shutdown sync.WaitGroup
	shutdown.Go(m.cleanupManagedResources)

	m.watchCurrentTheme()
	shutdown.Wait()

	waitThemeWatchRun(t, nextThemeWatchRun(t, fw))
}

func TestWatchCurrentTheme_ParentCancellation(t *testing.T) {
	m, _ := newTestModel(t)
	fw := setupThemeWatcher(t, m)
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.themeCtx = parent
	m.watchCurrentTheme()
	run := nextThemeWatchRun(t, fw)

	cancel()

	waitThemeWatchRun(t, run)
}

func TestHandleThemeFileChanged_IgnoresPreviousTheme(t *testing.T) {
	m, _ := newTestModel(t)
	current := styles.CurrentTheme()

	_, cmd := m.handleThemeFileChanged("user:previous-theme")

	assert.Nil(t, cmd)
	assert.Same(t, current, styles.CurrentTheme())
}
