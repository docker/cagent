package tools

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failedStartTool struct {
	startErr error
	stopErr  error
	stops    int
	starting chan struct{}
	release  chan struct{}
}

func (*failedStartTool) Tools(context.Context) ([]Tool, error) { return nil, nil }
func (s *failedStartTool) Start(context.Context) error {
	if s.starting != nil {
		close(s.starting)
		<-s.release
	}
	return s.startErr
}

func (s *failedStartTool) Stop(context.Context) error                   { s.stops++; return s.stopErr }
func (s *failedStartTool) CleanupFailedStart(ctx context.Context) error { return s.Stop(ctx) }

func TestFailedStartCleanupNeverStarted(t *testing.T) {
	t.Parallel()
	inner := &failedStartTool{}
	wrapper := NewStartable(inner)
	require.NoError(t, wrapper.StopIfStarted(t.Context()))
	assert.Zero(t, inner.stops)
}

func TestFailedStartCleanupRunsOnceAndRetainsError(t *testing.T) {
	t.Parallel()
	inner := &failedStartTool{startErr: errors.New("start timed out"), stopErr: errors.New("close failed")}
	wrapper := NewStartable(inner)
	require.ErrorIs(t, wrapper.Start(t.Context()), inner.startErr)
	require.ErrorIs(t, wrapper.StopIfStarted(t.Context()), inner.stopErr)
	require.ErrorIs(t, wrapper.StopIfStarted(t.Context()), inner.stopErr)
	assert.Equal(t, 1, inner.stops)
}

func TestFailedStartCleanupAfterShutdownWaitExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		inner := &failedStartTool{
			startErr: errors.New("start failed"), starting: make(chan struct{}), release: make(chan struct{}),
		}
		wrapper := NewStartable(inner)
		done := make(chan error, 1)
		go func() { done <- wrapper.Start(t.Context()) }()
		<-inner.starting
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, wrapper.StopIfStarted(ctx), context.DeadlineExceeded)
		close(inner.release)
		require.ErrorIs(t, <-done, inner.startErr)
		assert.Equal(t, 1, inner.stops)
	})
}

func TestFailedStartCleanupCanRetryInterruptedWait(t *testing.T) {
	t.Parallel()
	inner := &failedStartTool{startErr: errors.New("start failed"), stopErr: context.DeadlineExceeded}
	wrapper := NewStartable(inner)
	require.ErrorIs(t, wrapper.Start(t.Context()), inner.startErr)
	require.ErrorIs(t, wrapper.StopIfStarted(t.Context()), context.DeadlineExceeded)
	inner.stopErr = nil
	require.NoError(t, wrapper.StopIfStarted(t.Context()))
	assert.Equal(t, 2, inner.stops)
}
