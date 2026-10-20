package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

type recoverySession struct{ ended chan error }

func (s *recoverySession) Wait() error { return <-s.ended }
func (s *recoverySession) Close(context.Context) error {
	select {
	case s.ended <- nil:
	default:
	}
	return nil
}

func TestCanceledRecoveryRemainsAdoptableThroughToolset(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		initial := &recoverySession{ended: make(chan error, 1)}
		recovered := &recoverySession{ended: make(chan error, 1)}
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		owner := lifecycle.NewOwner("recover", cleanupConnector(func(ctx context.Context) (lifecycle.Session, error) {
			if calls.Add(1) == 1 {
				return initial, nil
			}
			close(entered)
			<-release
			require.NoError(t, ctx.Err())
			return recovered, nil
		}), lifecycle.Policy{Restart: lifecycle.RestartNever, StartupTimeout: time.Hour})
		wrapper := tools.NewStartable(&Toolset{supervisor: owner})
		require.NoError(t, wrapper.Start(t.Context()))
		initial.ended <- lifecycle.ErrTransport
		synctest.Wait()
		assert.Equal(t, lifecycle.StateFailed, owner.State().State)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- wrapper.Start(ctx) }()
		<-entered
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		close(release)
		synctest.Wait()
		assert.Equal(t, lifecycle.StateStopped, owner.State().State)

		// This invokes Toolset.Restart, not Owner.Start directly.
		require.NoError(t, wrapper.Start(t.Context()))
		assert.True(t, wrapper.IsStarted())
		assert.True(t, owner.IsReady())
		assert.EqualValues(t, 2, calls.Load(), "recovery must adopt, not open another connection")
		require.NoError(t, wrapper.StopIfStarted(t.Context()))
	})
}
