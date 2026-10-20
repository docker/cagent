package lifecycle

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// Supervisor is an immutable connection supervisor. Concurrent Run calls are
// independent; callers must supply independently owned connectors and hooks.
type Supervisor struct {
	name   string
	policy Policy
}

// New returns a supervisor with no connection or execution state.
func New(name string, policy Policy) *Supervisor {
	return &Supervisor{name: name, policy: policy}
}

// RunHooks observes one Run. Callbacks run synchronously; callbacks accepting a
// context must respect cancellation and must not wait for Run to return.
type RunHooks struct {
	OnState func(StateInfo)
	// OnReady runs once after the initial connection, before publishing Ready.
	OnReady func(context.Context) error
	// Restart requests a reconnect regardless of policy. Closing it disables it.
	Restart <-chan struct{}
}

// Run owns the connection until cancellation or a terminal failure. Cancellation
// returns ctx.Err(); connection, readiness, and cleanup errors are also returned.
// Connect runs synchronously: if it ignores cancellation, Run waits for it and
// closes any eventual session. StartupTimeout is left to the caller's adapter.
func (r *Supervisor) Run(ctx context.Context, connector Connector, hooks RunHooks) (runErr error) {
	tracker := NewTracker()
	publish := func(state State, err error) {
		tracker.Fail(state, err)
		if hooks.OnState != nil {
			hooks.OnState(tracker.Snapshot())
		}
	}
	var cleanupErr error
	fail := func(err error) error {
		if cleanupErr != nil {
			err = errors.Join(err, &cleanupError{cleanupErr})
			cleanupErr = nil
		}
		publish(StateFailed, err)
		if r.policy.OnFailed != nil {
			r.policy.OnFailed(err)
		}
		return err
	}

	var session Session
	var waitDone <-chan error
	connect := func(connectCtx context.Context) error {
		var err error
		session, err = connector.Connect(connectCtx)
		if err == nil && session == nil {
			return errors.New("connector returned no session")
		}
		if err == nil {
			done := make(chan error, 1)
			waitDone = done
			sess := session
			go func() { done <- sess.Wait() }()
		}
		return err
	}
	closeSession := func() error {
		if session == nil {
			return nil
		}
		cleanupErr = errors.Join(cleanupErr, session.Close(context.WithoutCancel(ctx)))
		session = nil
		var err error
		if waitDone != nil {
			err = <-waitDone
			waitDone = nil
		}
		return err
	}
	defer func() {
		_ = closeSession()
		if cleanupErr != nil {
			runErr = errors.Join(runErr, &cleanupError{cleanupErr})
		}
		if tracker.State() != StateFailed {
			publish(StateStopped, runErr)
		}
	}()

	publish(StateStarting, nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := connect(ctx); err != nil {
		return errors.Join(ctx.Err(), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if hooks.OnReady != nil {
		if err := hooks.OnReady(ctx); err != nil {
			return errors.Join(ctx.Err(), err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	publish(StateReady, nil)

	var crashTimes []time.Time
	restart := hooks.Restart
	log := r.policy.logger()
	for {
		var waitErr error
		var forced bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case waitErr = <-waitDone:
			waitDone = nil
		case _, ok := <-restart:
			if !ok {
				restart = nil
				continue
			}
			forced = true
			publish(StateRestarting, nil)
			waitErr = closeSession()
		}
		if !forced {
			// Owner stops accepting new force requests before this drain.
			publish(StateRestarting, waitErr)
			select {
			case _, ok := <-restart:
				forced = ok
				if !ok {
					restart = nil
				}
			default:
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		crashLooping := false
		if !forced && errors.Is(waitErr, ErrServerCrashed) {
			now := time.Now()
			cutoff := now.Add(-r.policy.CrashLoop.window())
			kept := crashTimes[:0]
			for _, crash := range crashTimes {
				if crash.After(cutoff) {
					kept = append(kept, crash)
				}
			}
			crashTimes = append(kept, now)
			crashLooping = len(crashTimes) >= r.policy.CrashLoop.threshold()
		}

		log.Warn("supervisor: session lost", "name", r.name, "error", waitErr, "forced", forced)
		if r.policy.OnDisconnect != nil {
			r.policy.OnDisconnect(waitErr)
		}
		// LSP needs teardown after process exit; MCP wraps a shared client, so
		// closing an old session after reconnect could close the new one.
		_ = closeSession()
		if err := ctx.Err(); err != nil {
			return err
		}
		if crashLooping {
			return fail(wrap(ErrCrashLooping, waitErr))
		}
		if !forced && (r.policy.Restart == RestartNever ||
			(r.policy.Restart != RestartAlways && (waitErr == nil || IsPermanent(waitErr)))) {
			return fail(waitErr)
		}

		maxAttempts := r.policy.maxAttempts()
		for attempt := 0; ; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if maxAttempts > 0 && attempt >= maxAttempts {
				return fail(tracker.LastError())
			}
			delay := r.policy.Backoff.delay(attempt, rand.Float64)
			log.Debug("supervisor: restart attempt", "name", r.name, "attempt", attempt+1, "backoff", delay)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			err := connect(withBackgroundReconnect(ctx))
			if ctx.Err() != nil {
				return errors.Join(ctx.Err(), err)
			}
			if err != nil {
				_ = closeSession()
				if ctx.Err() != nil {
					return errors.Join(ctx.Err(), err)
				}
				if IsPermanent(err) {
					return fail(err)
				}
				tracker.IncRestarts()
				publish(StateRestarting, err)
				continue
			}
			tracker.ResetRestarts()
			publish(StateReady, nil)
			log.Info("supervisor: restarted", "name", r.name, "attempt", attempt+1)
			if r.policy.OnRestart != nil {
				r.policy.OnRestart(ctx)
			}
			break
		}
	}
}

// cleanupError lets the legacy Stop report teardown errors without re-reporting
// a connection failure already surfaced through readiness or state.
type cleanupError struct{ error }

func (e *cleanupError) Unwrap() error { return e.error }
