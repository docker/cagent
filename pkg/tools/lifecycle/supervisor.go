package lifecycle

import (
	"context"
	"log/slog"
	"math"
	"time"
)

// backgroundReconnectKey is a context key that the supervisor attaches to
// connector.Connect calls made during background watcher reconnect attempts,
// distinguishing them from the initial interactive Start. Connector
// implementations (e.g. the MCP clientConnector) use this to apply
// non-interactive constraints on background reconnects so a 401 defers
// cleanly rather than blocking on a dead elicitation bridge.
type backgroundReconnectKey struct{}

// withBackgroundReconnect returns a copy of ctx marked as a background
// reconnect attempt. It is set by tryRestart before calling
// connector.Connect so the connector can distinguish watcher reconnects
// from the initial interactive Start.
func withBackgroundReconnect(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundReconnectKey{}, true)
}

// IsBackgroundReconnect reports whether ctx was created by the supervisor
// for a background reconnect attempt. Connector.Connect implementations can
// use this to disable interactive operations (e.g. OAuth prompts) that
// should not run in the background.
func IsBackgroundReconnect(ctx context.Context) bool {
	v, _ := ctx.Value(backgroundReconnectKey{}).(bool)
	return v
}

// Connector creates new sessions for a Supervisor. Implementations are
// transport-specific: stdio MCP, remote MCP, LSP stdio.
type Connector interface {
	// Connect establishes a new underlying connection (e.g. spawns a
	// process, dials HTTP, runs the initialize handshake). The returned
	// Session is owned by the supervisor; the supervisor calls Close on
	// it. Errors should be classified via Classify so the supervisor can
	// apply policy via errors.Is.
	Connect(ctx context.Context) (Session, error)
}

// Session is the supervisor's view of an active connection. Wait blocks
// until the session ends; Close terminates it. Close must be idempotent
// and safe to call concurrently with an in-flight Wait.
type Session interface {
	Wait() error
	Close(ctx context.Context) error
}

// Restart controls how the Supervisor reacts to an unexpected disconnect.
type Restart int

const (
	// RestartOnFailure reconnects after a non-nil Wait result or a forced
	// reconnect via RestartAndWait. Default; matches historical mcp.Toolset.
	RestartOnFailure Restart = iota
	// RestartNever transitions to Failed when the session ends.
	RestartNever
	// RestartAlways reconnects even after a clean (nil) Wait result.
	RestartAlways
)

// CrashLoop configures the supervisor's crash-loop detector: Threshold
// disconnects classified as ErrServerCrashed within Window stop the
// supervisor (state -> Failed, wrapped in ErrCrashLooping) instead of
// restarting again right away, so the caller's own backoff gate (e.g.
// StartableToolSet) paces the next attempt. Zero values default to
// Threshold=3, Window=1 minute.
//
// A single crash, or crashes spread out wider than Window apart, are
// ordinary restarts handled entirely by Restart/Backoff above and never
// trip this detector.
type CrashLoop struct {
	Threshold int
	Window    time.Duration
}

func (c CrashLoop) threshold() int {
	if c.Threshold <= 0 {
		return 3
	}
	return c.Threshold
}

func (c CrashLoop) window() time.Duration {
	if c.Window <= 0 {
		return time.Minute
	}
	return c.Window
}

// Backoff parameters for restart attempts. Zero values default to
// 1s..32s exponential (matching historical MCP behaviour).
type Backoff struct {
	Initial    time.Duration // first wait (default 1s)
	Max        time.Duration // cap (default 32s)
	Multiplier float64       // (default 2.0)
	Jitter     float64       // 0..1 fraction; 0 disables (default)
}

// delay returns the wait time before attempt n (0-based).
func (b Backoff) delay(attempt int, randFloat func() float64) time.Duration {
	initial := b.Initial
	if initial <= 0 {
		initial = time.Second
	}
	mul := b.Multiplier
	if mul <= 0 {
		mul = 2
	}
	maxDelay := b.Max
	if maxDelay <= 0 {
		maxDelay = 32 * time.Second
	}
	d := min(time.Duration(float64(initial)*math.Pow(mul, float64(attempt))), maxDelay)
	if b.Jitter > 0 {
		j := min(b.Jitter, 1.0)
		offset := (randFloat()*2 - 1) * j * float64(d)
		d = max(time.Duration(float64(d)+offset), 0)
	}
	return d
}

// Policy controls how a Supervisor manages a connection over time. The
// zero value gives the historical mcp.Toolset behaviour: RestartOnFailure,
// 5 attempts, 1s..32s backoff, no jitter, no callbacks.
type Policy struct {
	Restart     Restart // see Restart constants; default RestartOnFailure
	MaxAttempts int     // 0 = default (5); negative = unlimited
	Backoff     Backoff // zero fields use Backoff defaults

	// StartupTimeout bounds Owner.Start's wait, not the underlying handshake.
	// On expiry the same Run remains available for a later Start to adopt.
	// Zero means no timeout; direct Supervisor.Run calls use their context.
	StartupTimeout time.Duration

	// CallTimeout is carried through to the toolset for enforcement on
	// individual tool calls; the Supervisor itself does not use it.
	CallTimeout time.Duration

	// CrashLoop tunes the crash-loop detector (see CrashLoop's doc). Zero
	// value uses the CrashLoop defaults.
	CrashLoop CrashLoop

	// OnDisconnect is called when the session ends, with Wait()'s result.
	// Useful for cache invalidation.
	OnDisconnect func(err error)
	// OnRestart is called after each successful reconnect. Useful for
	// re-fetching server-side state (tools, prompts).
	OnRestart func(ctx context.Context)
	// OnFailed is called once when the supervisor enters StateFailed.
	OnFailed func(err error)

	// Logger is used for lifecycle logs. Defaults to slog.Default().
	Logger *slog.Logger
}

// maxAttempts resolves MaxAttempts to an effective value: 0 → default 5,
// negative → unlimited (returned as -1), positive → as configured.
func (p Policy) maxAttempts() int {
	if p.MaxAttempts == 0 {
		return 5
	}
	return p.MaxAttempts
}

// logger returns p.Logger or slog.Default if nil.
func (p Policy) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}
