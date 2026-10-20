package lifecycle_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

func TestOwner_StartJoinsBackgroundReconnect(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first, second := newFakeSession(), newFakeSession()
		reconnecting := make(chan struct{})
		release := make(chan struct{})
		var calls atomic.Int32
		connector := runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
			switch calls.Add(1) {
			case 1:
				return first, nil
			case 2:
				close(reconnecting)
				<-release
				return second, nil
			default:
				return nil, errors.New("overlapping connection")
			}
		})
		owner := lifecycle.NewOwner("shared", connector, lifecycle.Policy{Backoff: fastBackoff})
		require.NoError(t, owner.Start(t.Context()))
		first.fail(errors.New("lost"))
		<-reconnecting
		done := make(chan error, 1)
		go func() { done <- owner.Start(t.Context()) }()
		synctest.Wait()
		assert.EqualValues(t, 2, calls.Load())
		close(release)
		require.NoError(t, <-done)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_TimeoutDoesNotAutomaticallyAdopt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		connector := &blockingConnector{release: make(chan struct{}), session: newFakeSession()}
		owner := lifecycle.NewOwner("late", connector, lifecycle.Policy{StartupTimeout: time.Second})
		require.ErrorIs(t, owner.Start(t.Context()), lifecycle.ErrInitTimeout)
		close(connector.release)
		synctest.Wait()
		assert.Equal(t, lifecycle.StateStopped, owner.State().State)
		require.NoError(t, owner.Stop(t.Context()))
		connector.session.waitClosed(t)
	})
}

func TestOwner_ReconnectUsesAdoptingRequestValues(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		type key struct{}
		first, second := newFakeSession(), newFakeSession()
		release := make(chan struct{})
		values := make(chan any, 2)
		var calls atomic.Int32
		connector := runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
			values <- ctx.Value(key{})
			if calls.Add(1) == 1 {
				<-release
				return first, nil
			}
			return second, nil
		})
		owner := lifecycle.NewOwner("values", connector, lifecycle.Policy{StartupTimeout: time.Second, Backoff: fastBackoff})
		require.ErrorIs(t, owner.Start(context.WithValue(t.Context(), key{}, "initial")), lifecycle.ErrInitTimeout)
		close(release)
		require.NoError(t, owner.Start(context.WithValue(t.Context(), key{}, "adopter")))
		first.fail(errors.New("lost"))
		assert.Equal(t, "initial", <-values)
		assert.Equal(t, "adopter", <-values)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_StopPreservesCleanupError(t *testing.T) {
	t.Parallel()
	sess := newRunnerSession()
	sess.closeErr = errors.New("close failed")
	owner := lifecycle.NewOwner("cleanup", runnerConnector(func(context.Context) (lifecycle.Session, error) {
		return sess, nil
	}), lifecycle.Policy{})
	require.NoError(t, owner.Start(t.Context()))
	require.ErrorIs(t, owner.Stop(t.Context()), sess.closeErr)
	require.ErrorIs(t, owner.Stop(t.Context()), sess.closeErr)
}

func TestOwner_StartContextDoesNotOwnEstablishedSession(t *testing.T) {
	t.Parallel()
	sess := newRunnerSession()
	connected := make(chan context.Context, 1)
	owner := lifecycle.NewOwner("lifetime", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
		connected <- ctx
		return sess, nil
	}), lifecycle.Policy{})
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, owner.Start(ctx))
	cancel()
	connectCtx := <-connected
	require.ErrorIs(t, connectCtx.Err(), context.Canceled)
	assert.True(t, owner.IsReady(), "the initial request ending must not stop supervision")
	require.NoError(t, owner.Stop(t.Context()))
}

type slowCloseSession struct {
	*fakeSession

	closing chan struct{}
	release chan struct{}
	once    atomic.Bool
}

func (s *slowCloseSession) Close(ctx context.Context) error {
	if s.once.CompareAndSwap(false, true) {
		close(s.closing)
		<-s.release
	}
	return s.fakeSession.Close(ctx)
}

func TestOwner_ConcurrentRestartCoalescesDuringClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first := &slowCloseSession{fakeSession: newFakeSession(), closing: make(chan struct{}), release: make(chan struct{})}
		second := newFakeSession()
		var calls atomic.Int32
		owner := lifecycle.NewOwner("restart", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			return second, nil
		}), lifecycle.Policy{Backoff: fastBackoff})
		require.NoError(t, owner.Start(t.Context()))
		results := make(chan error, 2)
		go func() { results <- owner.RestartAndWait(t.Context(), time.Second) }()
		<-first.closing
		go func() { results <- owner.RestartAndWait(t.Context(), time.Second) }()
		synctest.Wait()
		close(first.release)
		require.NoError(t, <-results)
		require.NoError(t, <-results)
		synctest.Wait()
		assert.EqualValues(t, 2, calls.Load())
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_CanceledStartDoesNotCancelReconnect(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first, second := newFakeSession(), newFakeSession()
		reconnecting, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		owner := lifecycle.NewOwner("cancel-wait", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			close(reconnecting)
			<-release
			require.NoError(t, ctx.Err())
			return second, nil
		}), lifecycle.Policy{Backoff: fastBackoff})
		require.NoError(t, owner.Start(t.Context()))
		first.fail(errors.New("lost"))
		<-reconnecting
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- owner.Start(ctx) }()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		close(release)
		synctest.Wait()
		assert.True(t, owner.IsReady())
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_RecoveryWaitHonorsStartupTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first := newFakeSession()
		failed, release := make(chan struct{}), make(chan struct{})
		owner := lifecycle.NewOwner("terminal-wait", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			return first, nil
		}), lifecycle.Policy{
			Restart: lifecycle.RestartNever, StartupTimeout: time.Second,
			OnFailed: func(error) { close(failed); <-release },
		})
		require.NoError(t, owner.Start(t.Context()))
		first.fail(errors.New("failed"))
		<-failed
		require.ErrorIs(t, owner.Start(t.Context()), lifecycle.ErrInitTimeout)
		close(release)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_StartDuringCleanTerminationDoesNotReportReady(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sess := &slowCloseSession{fakeSession: newFakeSession(), closing: make(chan struct{}), release: make(chan struct{})}
		owner := lifecycle.NewOwner("clean-exit", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			return sess, nil
		}), lifecycle.Policy{Restart: lifecycle.RestartNever})
		require.NoError(t, owner.Start(t.Context()))
		sess.fail(nil)
		<-sess.closing
		result := make(chan error, 1)
		go func() { result <- owner.Start(t.Context()) }()
		synctest.Wait()
		close(sess.release)
		require.ErrorIs(t, <-result, lifecycle.ErrNotStarted)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_RetainsCleanupErrorAcrossRecovery(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		first, second := newRunnerSession(), newRunnerSession()
		first.closeErr = errors.New("cleanup failed")
		var calls atomic.Int32
		owner := lifecycle.NewOwner("recovery", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			return second, nil
		}), lifecycle.Policy{Restart: lifecycle.RestartNever})
		require.NoError(t, owner.Start(t.Context()))
		first.ended <- errors.New("lost")
		synctest.Wait()
		require.ErrorIs(t, owner.State().LastError, first.closeErr)
		require.NoError(t, owner.Start(t.Context()))
		require.ErrorIs(t, owner.Stop(t.Context()), first.closeErr)
	})
}

func TestOwner_InitialConnectPreservesDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	want, _ := ctx.Deadline()
	owner := lifecycle.NewOwner("deadline", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
		got, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.Equal(t, want, got)
		return newFakeSession(), nil
	}), lifecycle.Policy{})
	require.NoError(t, owner.Start(ctx))
	require.NoError(t, owner.Stop(t.Context()))
}

func TestOwner_CanceledAttemptDoesNotPoisonNextCaller(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		owner := lifecycle.NewOwner("retry", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
			if calls.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				<-release
				return nil, ctx.Err()
			}
			return newFakeSession(), nil
		}), lifecycle.Policy{})
		ctx, cancel := context.WithCancel(t.Context())
		first := make(chan error, 1)
		go func() { first <- owner.Start(ctx) }()
		<-entered
		cancel()
		require.ErrorIs(t, <-first, context.Canceled)
		second := make(chan error, 1)
		go func() { second <- owner.Start(t.Context()) }()
		synctest.Wait()
		assert.EqualValues(t, 1, calls.Load(), "cleanup must finish before retry")
		close(release)
		require.NoError(t, <-second)
		assert.EqualValues(t, 2, calls.Load())
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_StopCancelsInitialConnect(t *testing.T) {
	t.Parallel()
	for _, timeout := range []time.Duration{0, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			entered, canceled := make(chan struct{}), make(chan struct{})
			owner := lifecycle.NewOwner("cancel", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
				close(entered)
				<-ctx.Done()
				close(canceled)
				return nil, ctx.Err()
			}), lifecycle.Policy{StartupTimeout: timeout})
			done := make(chan error, 1)
			go func() { done <- owner.Start(t.Context()) }()
			<-entered
			require.NoError(t, owner.Stop(t.Context()))
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("Stop did not cancel initial Connect")
			}
			require.Error(t, <-done)
		})
	}
}

func TestOwner_QueuedStartCanCancel(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		owner := lifecycle.NewOwner("queued", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			close(entered)
			<-release
			return newFakeSession(), nil
		}), lifecycle.Policy{})
		first := make(chan error, 1)
		go func() { first <- owner.Start(t.Context()) }()
		<-entered
		ctx, cancel := context.WithCancel(t.Context())
		second := make(chan error, 1)
		go func() { second <- owner.Start(ctx) }()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-second, context.Canceled)
		close(release)
		require.NoError(t, <-first)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_RetainsUnadoptedCleanupFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		session := newRunnerSession()
		session.closeErr = errors.New("late close failed")
		owner := lifecycle.NewOwner("late", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			<-release
			return session, nil
		}), lifecycle.Policy{StartupTimeout: time.Second})
		require.ErrorIs(t, owner.Start(t.Context()), lifecycle.ErrInitTimeout)
		stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, owner.Stop(stopCtx), context.DeadlineExceeded)
		close(release)
		synctest.Wait()
		require.ErrorIs(t, owner.Stop(t.Context()), session.closeErr)
	})
}

type closeValueSession struct {
	*fakeSession

	value chan any
	key   any
}

func (s *closeValueSession) Close(ctx context.Context) error {
	s.value <- ctx.Value(s.key)
	return s.fakeSession.Close(ctx)
}

func TestOwner_StopPassesCleanupValues(t *testing.T) {
	t.Parallel()
	type key struct{}
	session := &closeValueSession{fakeSession: newFakeSession(), value: make(chan any, 1), key: key{}}
	owner := lifecycle.NewOwner("cleanup-values", runnerConnector(func(context.Context) (lifecycle.Session, error) {
		return session, nil
	}), lifecycle.Policy{})
	require.NoError(t, owner.Start(context.WithValue(t.Context(), key{}, "start")))
	require.NoError(t, owner.Stop(context.WithValue(t.Context(), key{}, "stop")))
	assert.Equal(t, "stop", <-session.value)
}

func TestOwner_QueuedStartHonorsTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		owner := lifecycle.NewOwner("queued-timeout", runnerConnector(func(context.Context) (lifecycle.Session, error) {
			close(entered)
			<-release
			return newFakeSession(), nil
		}), lifecycle.Policy{StartupTimeout: time.Second})
		first := make(chan error, 1)
		go func() { first <- owner.Start(t.Context()) }()
		<-entered
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()
		require.ErrorIs(t, owner.Start(ctx), context.DeadlineExceeded)
		close(release)
		require.NoError(t, <-first)
		require.NoError(t, owner.Stop(t.Context()))
	})
}

func TestOwner_CanceledDetachedStartRemainsAdoptable(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		owner := lifecycle.NewOwner("adopt", runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
			calls.Add(1)
			close(entered)
			<-release
			require.NoError(t, ctx.Err(), "canceling the wait must not cancel detached setup")
			return newFakeSession(), nil
		}), lifecycle.Policy{StartupTimeout: time.Hour})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- owner.Start(ctx) }()
		<-entered
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		assert.Equal(t, lifecycle.StateStopped, owner.State().State)
		close(release)
		synctest.Wait()
		assert.Equal(t, lifecycle.StateStopped, owner.State().State)
		require.NoError(t, owner.Start(t.Context()))
		assert.EqualValues(t, 1, calls.Load())
		require.NoError(t, owner.Stop(t.Context()))
	})
}
