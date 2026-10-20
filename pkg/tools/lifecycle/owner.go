package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Owner bridges request-scoped Start/Stop callers to a single connection Run.
// It never owns or closes sessions. A timed-out Start leaves the same Run
// available for adoption; Stop cancels it even if no Start succeeded.
type Owner struct {
	name      string
	connector Connector
	policy    Policy

	startGate  chan struct{}
	mu         sync.Mutex
	run        *ownedRun
	stopped    bool
	stopDone   chan struct{}
	state      StateInfo
	crashErr   error
	cleanupErr error
	changed    chan struct{}
	restarted  chan struct{}
}

type ownedRun struct {
	cancel     context.CancelFunc
	ready      chan struct{}
	adopt      chan struct{}
	done       chan struct{}
	restart    chan struct{}
	err        error        // Published by closing done.
	requestErr func() error // Nil for a detached, timeout-adoptable handshake.

	// Guarded by Owner.mu.
	adopted        bool
	active         bool
	restartPending bool
	values         context.Context //nolint:containedctx // Carries the adopting request's values, never its lifetime.
}

// NewOwner creates an inert legacy API adapter. Stop permanently closes it.
func NewOwner(name string, connector Connector, policy Policy) *Owner {
	return &Owner{
		name: name, connector: connector, policy: policy,
		state:   StateInfo{State: StateStopped, Since: time.Now()},
		changed: make(chan struct{}), restarted: make(chan struct{}),
		startGate: make(chan struct{}, 1), stopDone: make(chan struct{}),
	}
}

// State returns the last published connection state.
func (o *Owner) State() StateInfo {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// IsReady reports whether the connection can serve requests.
func (o *Owner) IsReady() bool { return o.State().State.IsUsable() }

// PendingCrashLoopError reads the crash-loop verdict without consuming it.
func (o *Owner) PendingCrashLoopError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.crashErr
}

// MarkReadyForTesting bypasses connection setup for transport tests.
func (o *Owner) MarkReadyForTesting() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state.State = StateReady
}

// Restarted returns a channel closed on the next successful reconnect.
func (o *Owner) Restarted() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.restarted
}

func (o *Owner) notifyLocked() {
	close(o.changed)
	o.changed = make(chan struct{})
}

// Start waits for readiness without launching a competing connection attempt.
// StartupTimeout bounds this wait, not the Run or its pending handshake.
func (o *Owner) Start(ctx context.Context) error {
	var timeout <-chan time.Time
	if o.policy.StartupTimeout > 0 {
		timer := time.NewTimer(o.policy.StartupTimeout)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case o.startGate <- struct{}{}:
		defer func() { <-o.startGate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout:
		return o.timeoutError()
	case <-o.stopDone:
		return ErrNotStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return ErrNotStarted
	}
	if o.crashErr != nil {
		err := o.crashErr
		o.crashErr = nil
		o.mu.Unlock()
		return err
	}
	if o.state.State.IsUsable() {
		o.mu.Unlock()
		return nil
	}
	if o.run != nil {
		previous := o.run
		canceledSetup := !previous.active && previous.requestErr != nil && previous.requestErr() != nil
		if (o.state.State.IsTerminal() && previous.active) || canceledSetup {
			if canceledSetup {
				previous.cancel()
			}
			o.mu.Unlock()
			select {
			case <-previous.done:
			case <-ctx.Done():
				return ctx.Err()
			case <-o.stopDone:
				return ErrNotStarted
			case <-timeout:
				return o.timeoutError()
			}
			o.mu.Lock()
			if o.stopped {
				o.mu.Unlock()
				return ErrNotStarted
			}
			o.retainCleanupLocked(previous)
			o.run = nil
		} else {
			select {
			case <-previous.done:
				o.retainCleanupLocked(previous)
				o.run = nil
			default:
			}
		}
	}
	if o.run == nil {
		o.launchLocked(ctx)
	}
	run := o.run
	o.mu.Unlock()
	values := context.WithoutCancel(ctx)

	for {
		o.mu.Lock()
		if o.stopped {
			o.mu.Unlock()
			return ErrNotStarted
		}
		if o.state.State.IsUsable() {
			o.mu.Unlock()
			return nil
		}
		ready := run.ready
		if run.adopted {
			ready = nil
		}
		changed := o.changed
		o.mu.Unlock()
		select {
		case <-ready:
			o.mu.Lock()
			if !run.adopted && !o.stopped {
				run.values = values //nolint:fatcontext // The adopting request replaces values once; contexts are not nested.
				run.adopted = true
				close(run.adopt)
			}
			o.mu.Unlock()
		case <-changed:
		case <-run.done:
			if run.err != nil {
				return run.err
			}
			return ErrNotStarted
		case <-ctx.Done():
			o.mu.Lock()
			if !run.active && run.requestErr != nil && run.requestErr() != nil {
				run.cancel()
			}
			o.abandonStartLocked(run, ctx.Err())
			o.mu.Unlock()
			return ctx.Err()
		case <-timeout:
			err := o.timeoutError()
			o.mu.Lock()
			o.abandonStartLocked(run, err)
			o.mu.Unlock()
			return err
		}
	}
}

// Until readiness is published, route public Restart through Start, not a reconnect wait.
func (o *Owner) abandonStartLocked(run *ownedRun, err error) {
	if !run.active && !o.stopped {
		o.state = StateInfo{State: StateStopped, Since: time.Now(), LastError: err}
		o.notifyLocked()
	}
}

func (o *Owner) retainCleanupLocked(run *ownedRun) {
	o.cleanupErr = errors.Join(o.cleanupErr, runCleanupError(run))
}

func runCleanupError(run *ownedRun) error {
	if cleanup, ok := errors.AsType[*cleanupError](run.err); ok {
		return cleanup.error
	}
	return nil
}

func (o *Owner) timeoutError() error {
	return wrap(ErrInitTimeout, fmt.Errorf("%q did not connect within %s", o.name, o.policy.StartupTimeout))
}

// ownerContext changes request values at adoption without changing Run's lifetime.
type ownerContext struct {
	context.Context //nolint:containedctx // Adapts request values to the separately owned lifetime.

	owner *Owner
	run   *ownedRun
}

func (c ownerContext) Value(key any) any {
	c.owner.mu.Lock()
	values := c.run.values
	c.owner.mu.Unlock()
	return values.Value(key)
}

type connectFunc func(context.Context) (Session, error)

func (f connectFunc) Connect(ctx context.Context) (Session, error) { return f(ctx) }

func (o *Owner) launchLocked(requestCtx context.Context) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(requestCtx))
	setupParent := requestCtx
	if o.policy.StartupTimeout > 0 {
		setupParent = context.WithoutCancel(requestCtx)
	}
	setupCtx, cancelSetup := context.WithCancel(setupParent)
	run := &ownedRun{
		cancel: func() { cancelSetup(); cancel() }, ready: make(chan struct{}), adopt: make(chan struct{}),
		done: make(chan struct{}), restart: make(chan struct{}, 1), values: context.WithoutCancel(requestCtx),
	}
	if o.policy.StartupTimeout == 0 {
		run.requestErr = requestCtx.Err
	}
	o.run = run
	o.state = StateInfo{State: StateStarting, Since: time.Now()}
	ctx = ownerContext{Context: ctx, owner: o, run: run}
	first := true
	connector := connectFunc(func(runCtx context.Context) (Session, error) {
		if !first {
			return o.connector.Connect(runCtx)
		}
		first = false
		return o.connector.Connect(setupCtx)
	})
	go func() {
		defer close(run.done)
		defer run.cancel()
		run.err = New(o.name, o.policy).Run(ctx, connector, RunHooks{
			Restart: run.restart,
			OnReady: func(ctx context.Context) error {
				close(run.ready)
				select {
				case <-run.adopt:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			OnState: func(state StateInfo) {
				o.mu.Lock()
				defer o.mu.Unlock()
				// Starting was published before launching Run. Do not overwrite
				// an abandoned wait if the goroutine was slow to start.
				if o.stopped || state.State == StateStarting {
					return
				}
				o.state = state
				if state.State.IsUsable() {
					run.restartPending = false
					if run.active {
						close(o.restarted)
						o.restarted = make(chan struct{})
					}
					run.active = true
				}
				if errors.Is(state.LastError, ErrCrashLooping) {
					o.crashErr = state.LastError
				}
				o.notifyLocked()
			},
		})
	}()
}

// Stop cancels and joins the owned Run, including an unadopted handshake.
// If ctx expires, the Run retains ownership until its cleanup completes.
func (o *Owner) Stop(ctx context.Context) error {
	o.mu.Lock()
	if !o.stopped {
		close(o.stopDone)
		o.stopped = true
	}
	o.crashErr = nil
	o.state = StateInfo{State: StateStopped, Since: time.Now()}
	run := o.run
	previousCleanup := o.cleanupErr
	if run != nil {
		run.values = context.WithoutCancel(ctx)
		run.cancel()
	}
	o.notifyLocked()
	o.mu.Unlock()
	if run == nil {
		return previousCleanup
	}
	select {
	case <-run.done:
		return errors.Join(previousCleanup, runCleanupError(run))
	default:
	}
	select {
	case <-run.done:
		return errors.Join(previousCleanup, runCleanupError(run))
	case <-ctx.Done():
		return errors.Join(previousCleanup, ctx.Err())
	}
}

// RestartAndWait requests a reconnect or joins the one already in progress.
func (o *Owner) RestartAndWait(ctx context.Context, timeout time.Duration) error {
	o.mu.Lock()
	if o.stopped || o.run == nil {
		o.mu.Unlock()
		return ErrNotStarted
	}
	run, restarted := o.run, o.restarted
	if o.state.State.IsUsable() && !run.restartPending {
		run.restartPending = true
		select {
		case run.restart <- struct{}{}:
		default:
		}
	}
	o.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		o.mu.Lock()
		if o.stopped || o.state.State.IsTerminal() {
			err := o.state.LastError
			o.mu.Unlock()
			if err != nil {
				return err
			}
			return ErrNotStarted
		}
		changed := o.changed
		o.mu.Unlock()
		select {
		case <-restarted:
			return nil
		case <-changed:
		case <-run.done:
			if run.err != nil {
				return run.err
			}
			return ErrNotStarted
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("timed out waiting for supervisor reconnect")
		}
	}
}
