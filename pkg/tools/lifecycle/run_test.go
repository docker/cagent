package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gotest.tools/v3/assert"

	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

type runnerConnector func(context.Context) (lifecycle.Session, error)

func (f runnerConnector) Connect(ctx context.Context) (lifecycle.Session, error) { return f(ctx) }

type runnerSession struct {
	ended        chan error
	waiting      chan struct{}
	waitExited   chan struct{}
	closed       chan struct{}
	waitRelease  <-chan struct{}
	closeOnce    sync.Once
	closeCount   atomic.Int32
	closeErr     error
	closeWaitErr error
	closeCtxErr  error
}

func newRunnerSession() *runnerSession {
	return &runnerSession{
		ended:      make(chan error, 1),
		waiting:    make(chan struct{}),
		waitExited: make(chan struct{}),
		closed:     make(chan struct{}),
	}
}

func (s *runnerSession) Wait() error {
	close(s.waiting)
	err := <-s.ended
	if s.waitRelease != nil {
		<-s.waitRelease
	}
	close(s.waitExited)
	return err
}

func (s *runnerSession) Close(ctx context.Context) error {
	s.closeCount.Add(1)
	s.closeOnce.Do(func() {
		s.closeCtxErr = ctx.Err()
		close(s.closed)
		select {
		case s.ended <- s.closeWaitErr:
		default:
		}
	})
	return s.closeErr
}

func runnerReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runner")
		var zero T
		return zero
	}
}

var runnerBackoff = lifecycle.Backoff{Initial: time.Millisecond, Max: time.Millisecond}

func TestRunnerInitialFailure(t *testing.T) {
	t.Parallel()

	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "no session", true: "partial session"}[partial], func(t *testing.T) {
			want := errors.New("connect failed")
			var states []lifecycle.State
			session := newRunnerSession()
			connector := runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
				assert.Assert(t, !lifecycle.IsBackgroundReconnect(ctx))
				assert.DeepEqual(t, states, []lifecycle.State{lifecycle.StateStarting})
				if partial {
					return session, want
				}
				return nil, want
			})
			err := lifecycle.New("test", lifecycle.Policy{
				OnFailed: func(error) { t.Error("initial failure must not invoke OnFailed") },
			}).Run(t.Context(), connector, lifecycle.RunHooks{
				OnState: func(info lifecycle.StateInfo) { states = append(states, info.State) },
				OnReady: func(context.Context) error {
					t.Error("failed connect must not invoke OnReady")
					return nil
				},
			})
			assert.ErrorIs(t, err, want)
			assert.DeepEqual(t, states, []lifecycle.State{lifecycle.StateStarting, lifecycle.StateStopped})
			if partial {
				assert.Equal(t, session.closeCount.Load(), int32(1))
			}
		})
	}
}

func TestRunnerReadinessErrorCleansUp(t *testing.T) {
	t.Parallel()

	session := newRunnerSession()
	readyErr := errors.New("adoption rejected")
	session.closeErr = errors.New("close failed")
	var states []lifecycle.State
	err := lifecycle.New("test", lifecycle.Policy{}).Run(t.Context(), runnerConnector(
		func(context.Context) (lifecycle.Session, error) { return session, nil },
	), lifecycle.RunHooks{
		OnState: func(info lifecycle.StateInfo) { states = append(states, info.State) },
		OnReady: func(context.Context) error {
			assert.DeepEqual(t, states, []lifecycle.State{lifecycle.StateStarting})
			return readyErr
		},
	})
	assert.ErrorIs(t, err, readyErr)
	assert.ErrorIs(t, err, session.closeErr)
	assert.Equal(t, session.closeCount.Load(), int32(1))
	runnerReceive(t, session.waitExited)
	assert.DeepEqual(t, states, []lifecycle.State{lifecycle.StateStarting, lifecycle.StateStopped})
}

func TestRunnerCancellationClosesAndJoinsWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newRunnerSession()
		release := make(chan struct{})
		session.waitRelease = release
		session.closeErr = errors.New("cleanup failed")
		states := make(chan lifecycle.StateInfo, 10)
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{}).Run(ctx, runnerConnector(
				func(context.Context) (lifecycle.Session, error) { return session, nil },
			), lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { states <- info }})
		}()
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateStarting)
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateReady)
		runnerReceive(t, session.waiting)
		cancel()
		runnerReceive(t, session.closed)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Run returned before Wait completed")
		default:
		}
		close(release)
		err := runnerReceive(t, done)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, session.closeErr)
		assert.NilError(t, session.closeCtxErr)
		assert.Equal(t, session.closeCount.Load(), int32(1))
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateStopped)
	})
}

func TestRunnerCancellationDuringReadiness(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newRunnerSession()
		adopting := make(chan struct{})
		states := make(chan lifecycle.StateInfo, 10)
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{}).Run(ctx, runnerConnector(
				func(context.Context) (lifecycle.Session, error) { return session, nil },
			), lifecycle.RunHooks{
				OnState: func(info lifecycle.StateInfo) { states <- info },
				OnReady: func(ctx context.Context) error {
					close(adopting)
					<-ctx.Done()
					return ctx.Err()
				},
			})
		}()
		runnerReceive(t, adopting)
		cancel()
		assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateStarting)
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateStopped)
		assert.Equal(t, session.closeCount.Load(), int32(1))
		runnerReceive(t, session.waitExited)
	})
}

func TestRunnerIndependentConcurrentRuns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		runner := lifecycle.New("shared", lifecycle.Policy{})
		ctx1, cancel1 := context.WithCancel(t.Context())
		defer cancel1()
		ctx2, cancel2 := context.WithCancel(t.Context())
		defer cancel2()
		session1, session2 := newRunnerSession(), newRunnerSession()
		done1, done2 := make(chan error, 1), make(chan error, 1)
		go func() {
			done1 <- runner.Run(ctx1, runnerConnector(func(context.Context) (lifecycle.Session, error) {
				return session1, nil
			}), lifecycle.RunHooks{})
		}()
		go func() {
			done2 <- runner.Run(ctx2, runnerConnector(func(context.Context) (lifecycle.Session, error) {
				return session2, nil
			}), lifecycle.RunHooks{})
		}()
		runnerReceive(t, session1.waiting)
		runnerReceive(t, session2.waiting)
		cancel1()
		assert.ErrorIs(t, runnerReceive(t, done1), context.Canceled)
		assert.Equal(t, session2.closeCount.Load(), int32(0))
		select {
		case <-done2:
			t.Fatal("cancelling one run stopped another")
		default:
		}
		cancel2()
		assert.ErrorIs(t, runnerReceive(t, done2), context.Canceled)
		assert.Equal(t, session1.closeCount.Load(), int32(1))
		assert.Equal(t, session2.closeCount.Load(), int32(1))
	})
}

func TestRunnerCancellationDuringConnect(t *testing.T) {
	t.Parallel()

	for _, reconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "reconnect"}[reconnect], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				initial, eventual := newRunnerSession(), newRunnerSession()
				entered, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				connector := runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
					call := calls.Add(1)
					if reconnect && call == 1 {
						return initial, nil
					}
					assert.Equal(t, lifecycle.IsBackgroundReconnect(ctx), reconnect)
					close(entered)
					<-release // Deliberately ignore cancellation.
					return eventual, nil
				})
				states := make(chan lifecycle.StateInfo, 10)
				done := make(chan error, 1)
				go func() {
					done <- lifecycle.New("test", lifecycle.Policy{Backoff: runnerBackoff}).Run(ctx, connector,
						lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { states <- info }})
				}()
				if reconnect {
					runnerReceive(t, initial.waiting)
					initial.ended <- lifecycle.ErrTransport
				}
				runnerReceive(t, entered)
				cancel()
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("Run abandoned a connector that ignores cancellation")
				default:
				}
				close(release)
				assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
				assert.Equal(t, eventual.closeCount.Load(), int32(1))
				runnerReceive(t, eventual.waitExited)
				close(states)
				readyCount := 0
				for info := range states {
					if info.State == lifecycle.StateReady {
						readyCount++
					}
				}
				if reconnect {
					assert.Equal(t, readyCount, 1)
				} else {
					assert.Equal(t, readyCount, 0)
				}
			})
		})
	}
}

func TestRunnerCancellationDuringBackoff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newRunnerSession()
		var calls atomic.Int32
		connector := runnerConnector(func(context.Context) (lifecycle.Session, error) {
			calls.Add(1)
			return session, nil
		})
		states := make(chan lifecycle.StateInfo, 10)
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{
				Backoff: lifecycle.Backoff{Initial: time.Hour, Max: time.Hour},
			}).Run(ctx, connector, lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { states <- info }})
		}()
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateStarting)
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateReady)
		session.ended <- lifecycle.ErrTransport
		assert.Equal(t, runnerReceive(t, states).State, lifecycle.StateRestarting)
		synctest.Wait()
		cancel()
		assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
		assert.Equal(t, calls.Load(), int32(1))
		assert.Equal(t, session.closeCount.Load(), int32(1))
	})
}

func TestRunnerPoliciesAndCallbackOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		policy  lifecycle.Restart
		waitErr error
		retry   bool
	}{
		{name: "never", policy: lifecycle.RestartNever, waitErr: lifecycle.ErrTransport},
		{name: "clean on failure", policy: lifecycle.RestartOnFailure},
		{name: "failure", policy: lifecycle.RestartOnFailure, waitErr: lifecycle.ErrTransport, retry: true},
		{name: "always clean", policy: lifecycle.RestartAlways, retry: true},
		{name: "always permanent", policy: lifecycle.RestartAlways, waitErr: lifecycle.ErrAuthRequired, retry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				initial, second := newRunnerSession(), newRunnerSession()
				var events []string
				var calls int
				var last lifecycle.StateInfo
				connector := runnerConnector(func(ctx context.Context) (lifecycle.Session, error) {
					calls++
					if calls == 1 {
						assert.Assert(t, !lifecycle.IsBackgroundReconnect(ctx))
						return initial, nil
					}
					assert.Assert(t, lifecycle.IsBackgroundReconnect(ctx))
					assert.Equal(t, initial.closeCount.Load(), int32(1))
					return second, nil
				})
				done := make(chan error, 1)
				go func() {
					done <- lifecycle.New("test", lifecycle.Policy{
						Restart: tc.policy,
						Backoff: runnerBackoff,
						OnDisconnect: func(err error) {
							assert.Equal(t, last.State, lifecycle.StateRestarting)
							assert.ErrorIs(t, err, tc.waitErr)
							events = append(events, "disconnect")
						},
						OnRestart: func(context.Context) {
							assert.Equal(t, last.State, lifecycle.StateReady)
							events = append(events, "restart")
							cancel()
						},
						OnFailed: func(err error) {
							assert.Equal(t, last.State, lifecycle.StateFailed)
							assert.ErrorIs(t, err, tc.waitErr)
							events = append(events, "failed")
						},
					}).Run(ctx, connector, lifecycle.RunHooks{
						OnState: func(info lifecycle.StateInfo) {
							last = info
							events = append(events, info.State.String())
						},
						OnReady: func(context.Context) error {
							events = append(events, "adopt")
							return nil
						},
					})
				}()
				runnerReceive(t, initial.waiting)
				initial.ended <- tc.waitErr
				err := runnerReceive(t, done)
				want := []string{"starting", "adopt", "ready", "restarting", "disconnect"}
				if tc.retry {
					assert.ErrorIs(t, err, context.Canceled)
					assert.Equal(t, calls, 2)
					want = append(want, "ready", "restart", "stopped")
				} else {
					assert.ErrorIs(t, err, tc.waitErr)
					assert.Equal(t, calls, 1)
					want = append(want, "failed", "failed")
				}
				assert.DeepEqual(t, events, want)
				assert.Equal(t, initial.closeCount.Load(), int32(1))
			})
		})
	}
}

func TestRunnerReconnectErrors(t *testing.T) {
	t.Parallel()

	for _, permanent := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget exhausted", true: "permanent"}[permanent], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				session := newRunnerSession()
				want := errors.New("reconnect failed")
				if permanent {
					want = lifecycle.ErrAuthRequired
				}
				var calls, failures int
				var last lifecycle.StateInfo
				connector := runnerConnector(func(context.Context) (lifecycle.Session, error) {
					calls++
					if calls == 1 {
						return session, nil
					}
					return nil, want
				})
				done := make(chan error, 1)
				go func() {
					done <- lifecycle.New("test", lifecycle.Policy{
						MaxAttempts: 2,
						Backoff:     runnerBackoff,
						OnFailed: func(err error) {
							assert.ErrorIs(t, err, want)
							failures++
						},
					}).Run(t.Context(), connector, lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { last = info }})
				}()
				runnerReceive(t, session.waiting)
				session.ended <- lifecycle.ErrTransport
				assert.ErrorIs(t, runnerReceive(t, done), want)
				assert.Equal(t, failures, 1)
				assert.Equal(t, last.State, lifecycle.StateFailed)
				if permanent {
					assert.Equal(t, calls, 2)
					assert.Equal(t, last.RestartCount, 0)
				} else {
					assert.Equal(t, calls, 3)
					assert.Equal(t, last.RestartCount, 2)
				}
			})
		})
	}
}

func TestRunnerSuccessfulReconnectResetsBudget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sessions := []*runnerSession{newRunnerSession(), newRunnerSession(), newRunnerSession()}
		var calls int
		connector := runnerConnector(func(context.Context) (lifecycle.Session, error) {
			calls++
			switch calls {
			case 1:
				return sessions[0], nil
			case 3:
				return sessions[1], nil
			case 5:
				return sessions[2], nil
			default:
				return nil, lifecycle.ErrTransport
			}
		})
		ready := make(chan lifecycle.StateInfo, 3)
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{
				Restart: lifecycle.RestartAlways, MaxAttempts: 2, Backoff: runnerBackoff,
			}).Run(ctx, connector, lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) {
				if info.State == lifecycle.StateReady {
					ready <- info
				}
			}})
		}()
		assert.Equal(t, runnerReceive(t, ready).RestartCount, 0)
		for i := range 2 {
			sessions[i].ended <- nil
			assert.Equal(t, runnerReceive(t, ready).RestartCount, 0)
		}
		cancel()
		assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
		assert.Equal(t, calls, 5)
	})
}

func TestRunnerExplicitRestart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		initial, second := newRunnerSession(), newRunnerSession()
		initial.closeWaitErr = lifecycle.ErrServerCrashed
		requests := make(chan struct{})
		restarted := make(chan struct{})
		var calls int
		connector := runnerConnector(func(context.Context) (lifecycle.Session, error) {
			calls++
			if calls == 1 {
				return initial, nil
			}
			return second, nil
		})
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{
				Restart:   lifecycle.RestartNever,
				Backoff:   runnerBackoff,
				CrashLoop: lifecycle.CrashLoop{Threshold: 1},
				OnRestart: func(context.Context) { close(restarted) },
			}).Run(ctx, connector, lifecycle.RunHooks{Restart: requests})
		}()
		runnerReceive(t, initial.waiting)
		requests <- struct{}{}
		runnerReceive(t, restarted)
		assert.Equal(t, initial.closeCount.Load(), int32(1))
		close(requests)
		synctest.Wait()
		assert.Equal(t, calls, 2)
		cancel()
		assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
		assert.Equal(t, second.closeCount.Load(), int32(1))
	})
}

func TestRunnerCrashLoopWindow(t *testing.T) {
	t.Parallel()

	for _, spaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "threshold", true: "pruning"}[spaced], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				sessions := []*runnerSession{newRunnerSession(), newRunnerSession(), newRunnerSession(), newRunnerSession()}
				var calls int
				connector := runnerConnector(func(context.Context) (lifecycle.Session, error) {
					session := sessions[calls]
					calls++
					return session, nil
				})
				ready := make(chan struct{}, 4)
				done := make(chan error, 1)
				go func() {
					done <- lifecycle.New("test", lifecycle.Policy{
						Backoff: runnerBackoff, CrashLoop: lifecycle.CrashLoop{Threshold: 2, Window: time.Second},
					}).Run(ctx, connector, lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) {
						if info.State == lifecycle.StateReady {
							ready <- struct{}{}
						}
					}})
				}()
				runnerReceive(t, ready)
				sessions[0].ended <- lifecycle.ErrServerCrashed
				runnerReceive(t, ready)
				if spaced {
					time.Sleep(2 * time.Second) //nolint:forbidigo // Advance virtual time beyond the crash window.
				}
				sessions[1].ended <- lifecycle.ErrServerCrashed
				if spaced {
					runnerReceive(t, ready)
					sessions[2].ended <- lifecycle.ErrTransport
					runnerReceive(t, ready)
					cancel()
					assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
					assert.Equal(t, calls, 4)
				} else {
					err := runnerReceive(t, done)
					assert.ErrorIs(t, err, lifecycle.ErrCrashLooping)
					assert.ErrorIs(t, err, lifecycle.ErrServerCrashed)
					assert.Equal(t, calls, 2)
				}
			})
		})
	}
}

func TestRunnerCancelledContextSkipsConnect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var states []lifecycle.State
	err := lifecycle.New("test", lifecycle.Policy{}).Run(ctx, runnerConnector(
		func(context.Context) (lifecycle.Session, error) {
			t.Error("cancelled run must not connect")
			return nil, nil
		},
	), lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { states = append(states, info.State) }})
	assert.ErrorIs(t, err, context.Canceled)
	assert.DeepEqual(t, states, []lifecycle.State{lifecycle.StateStarting, lifecycle.StateStopped})
}

func TestRunnerConnectObservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- lifecycle.New("test", lifecycle.Policy{}).Run(ctx, runnerConnector(
			func(ctx context.Context) (lifecycle.Session, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		), lifecycle.RunHooks{})
	}()
	runnerReceive(t, entered)
	cancel()
	assert.ErrorIs(t, runnerReceive(t, done), context.Canceled)
}

func TestRunnerCancellationDuringDisconnectWins(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		session := newRunnerSession()
		session.closeErr = errors.New("lost session cleanup failed")
		var last lifecycle.StateInfo
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{
				CrashLoop:    lifecycle.CrashLoop{Threshold: 1},
				OnDisconnect: func(error) { cancel() },
				OnFailed:     func(error) { t.Error("cancellation must win over the crash verdict") },
			}).Run(ctx, runnerConnector(func(context.Context) (lifecycle.Session, error) {
				return session, nil
			}), lifecycle.RunHooks{OnState: func(info lifecycle.StateInfo) { last = info }})
		}()
		runnerReceive(t, session.waiting)
		session.ended <- lifecycle.ErrServerCrashed
		err := runnerReceive(t, done)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, session.closeErr)
		assert.Equal(t, last.State, lifecycle.StateStopped)
		assert.Equal(t, session.closeCount.Load(), int32(1))
	})
}

func TestRunnerRetainsCleanupErrorsAcrossReconnect(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		initial, second := newRunnerSession(), newRunnerSession()
		initial.closeErr = errors.New("lost session cleanup failed")
		second.closeErr = errors.New("active session cleanup failed")
		var calls int
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.New("test", lifecycle.Policy{
				Backoff:   runnerBackoff,
				OnRestart: func(context.Context) { cancel() },
			}).Run(ctx, runnerConnector(func(context.Context) (lifecycle.Session, error) {
				calls++
				if calls == 1 {
					return initial, nil
				}
				return second, nil
			}), lifecycle.RunHooks{})
		}()
		runnerReceive(t, initial.waiting)
		initial.ended <- lifecycle.ErrTransport
		err := runnerReceive(t, done)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, initial.closeErr)
		assert.ErrorIs(t, err, second.closeErr)
		assert.Equal(t, initial.closeCount.Load(), int32(1))
		assert.Equal(t, second.closeCount.Load(), int32(1))
	})
}
