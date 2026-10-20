package mcp

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/codemode"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

type cleanupConnector func(context.Context) (lifecycle.Session, error)

func (f cleanupConnector) Connect(ctx context.Context) (lifecycle.Session, error) { return f(ctx) }

type cleanupSession struct {
	closed  chan struct{}
	release <-chan struct{}
	err     error
}

func (s *cleanupSession) Wait() error { <-s.closed; return nil }
func (s *cleanupSession) Close(context.Context) error {
	if s.release != nil {
		<-s.release
	}
	close(s.closed)
	return s.err
}

func TestFailedStartupCleanupRemainsJoinableThroughWrapper(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		failure := errors.New("late cleanup failed")
		sess := &cleanupSession{closed: make(chan struct{}), err: failure}
		ts := &Toolset{supervisor: lifecycle.NewOwner("late", cleanupConnector(func(context.Context) (lifecycle.Session, error) {
			<-release
			return sess, nil
		}), lifecycle.Policy{StartupTimeout: time.Second})}
		wrapped := tools.NewStartable(ts)
		require.ErrorIs(t, wrapped.Start(t.Context()), lifecycle.ErrInitTimeout)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, wrapped.StopIfStarted(ctx), context.DeadlineExceeded)
		close(release)
		require.ErrorIs(t, wrapped.StopIfStarted(t.Context()), failure)
	})
}

func TestActiveCleanupRemainsJoinableThroughWrapper(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		failure := errors.New("active cleanup failed")
		sess := &cleanupSession{closed: make(chan struct{}), release: release, err: failure}
		ts := &Toolset{supervisor: lifecycle.NewOwner("active", cleanupConnector(func(context.Context) (lifecycle.Session, error) {
			return sess, nil
		}), lifecycle.Policy{})}
		wrapped := tools.NewStartable(ts)
		require.NoError(t, wrapped.Start(t.Context()))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, wrapped.StopIfStarted(ctx), context.DeadlineExceeded)
		close(release)
		require.ErrorIs(t, wrapped.StopIfStarted(t.Context()), failure)
	})
}

func TestPartialCompositeCleanupRemainsJoinable(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		failure := errors.New("partial child cleanup failed")
		sess := &cleanupSession{closed: make(chan struct{}), err: failure}
		ts := &Toolset{supervisor: lifecycle.NewOwner("partial", cleanupConnector(func(context.Context) (lifecycle.Session, error) {
			<-release
			return sess, nil
		}), lifecycle.Policy{StartupTimeout: time.Second})}
		wrapped := tools.NewStartable(codemode.Wrap(&simpleToolset{}, ts))
		err := wrapped.Start(t.Context())
		require.True(t, tools.IsPartialStart(err))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, wrapped.StopIfStarted(ctx), context.DeadlineExceeded)
		close(release)
		require.ErrorIs(t, wrapped.StopIfStarted(t.Context()), failure)
	})
}

type simpleToolset struct{}

func (*simpleToolset) Tools(context.Context) ([]tools.Tool, error) { return nil, nil }
