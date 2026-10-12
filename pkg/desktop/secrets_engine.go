package desktop

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/secrets-engine/client/dockerhub"

	"github.com/docker/docker-agent/pkg/hubauth"
)

const (
	// secretsEngineBudget bounds a session lookup: the SDK has no default timeout.
	secretsEngineBudget = 5 * time.Second

	// secretsEngineCooldown is how long a failing engine is skipped.
	secretsEngineCooldown = 30 * time.Second
)

var (
	errSecretsEngineCoolingDown = errors.New("secrets engine lookup failed recently, not retrying yet")
	errSecretsEngineSlow        = errors.New("secrets engine did not answer in time")
)

const secretsEngineFailureMsg = "Could not read the Docker Hub session from the secrets engine"

// defaultSecretsEngine is the engine [GetTokenWithSource] reads, created on first use.
var defaultSecretsEngine = sync.OnceValue(func() *secretsEngine {
	client, err := newSecretsEngineHubAuth(secretsEngineHubOptions(hubauth.Staging())...)
	return &secretsEngine{client: client, clientErr: err, logger: slog.Default()}
})

// secretsEngine reads Docker Hub sessions from the secrets engine, sharing lookups and cooling down after failures.
type secretsEngine struct {
	client    dockerhub.ClientAuth
	clientErr error
	logger    *slog.Logger

	mu          sync.Mutex
	nextAttempt time.Time            // earliest time the engine may be asked again
	reported    bool                 // the current run of failures was logged as a warning
	inflight    *secretsEngineLookup // the lookup callers share, nil when none runs
}

// secretsEngineLookup is one session lookup shared by concurrent callers.
type secretsEngineLookup struct {
	done  chan struct{} // closed once token and err are set
	token string
	err   error
}

// secretsEngineHubOptions reads the staging realms when Docker Hub staging is in use.
func secretsEngineHubOptions(staging bool) []dockerhub.Option {
	if staging {
		return []dockerhub.Option{dockerhub.Staging()}
	}
	return nil
}

// secretsEngineToken returns a usable token from the secrets engine and caches it.
func secretsEngineToken(ctx context.Context, engine *secretsEngine) (string, bool) {
	token, err := engine.fetch(ctx)
	switch {
	case err != nil:
		slog.DebugContext(ctx, "Skipping the secrets engine", "error", err)
		return "", false
	case token == "":
		slog.DebugContext(ctx, "No Docker Hub session in the secrets engine")
		return "", false
	case !usable(token):
		slog.DebugContext(ctx, "The secrets engine served a token that expired, is about to, or was refused",
			"fingerprint", tokenFingerprint(token),
			"expires_in", expiresIn(token))
		return "", false
	case !remember(token, SourceSecretsEngine):
		return "", false
	}
	return token, true
}

// fetch waits at most half of ctx's remaining time for the shared lookup.
func (e *secretsEngine) fetch(ctx context.Context) (string, error) {
	lookup, err := e.join(ctx)
	if err != nil {
		return "", err
	}

	wait, cancel := context.WithTimeout(ctx, secretsEngineWait(ctx))
	defer cancel()

	select {
	case <-lookup.done:
		return lookup.token, lookup.err
	case <-wait.Done():
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", errSecretsEngineSlow
	}
}

// secretsEngineWait is how long ctx's caller may wait for the engine.
func secretsEngineWait(ctx context.Context) time.Duration {
	wait := secretsEngineBudget
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline)/2)
	}
	return wait
}

// join returns the running lookup, or starts one.
func (e *secretsEngine) join(ctx context.Context) (*secretsEngineLookup, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if lookup := e.inflight; lookup != nil {
		return lookup, nil
	}
	if time.Now().Before(e.nextAttempt) {
		return nil, errSecretsEngineCoolingDown
	}

	lookup := &secretsEngineLookup{done: make(chan struct{})}
	e.inflight = lookup
	go e.run(context.WithoutCancel(ctx), lookup)
	return lookup, nil
}

// run reads the session; a failure is logged and starts the cooldown.
func (e *secretsEngine) run(ctx context.Context, lookup *secretsEngineLookup) {
	token, err := e.session(ctx)
	if err != nil {
		e.logError(ctx, err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.nextAttempt = time.Now().Add(secretsEngineCooldown)
	} else {
		e.reported = false
	}
	e.inflight = nil
	lookup.token, lookup.err = token, err
	close(lookup.done)
}

// session returns the default account's access token, or "" when signed out.
func (e *secretsEngine) session(ctx context.Context) (string, error) {
	if e.clientErr != nil {
		return "", e.clientErr
	}

	ctx, cancel := context.WithTimeout(ctx, secretsEngineBudget)
	defer cancel()

	session, err := e.client.GetDefaultSession(ctx)
	switch {
	case err == nil:
		return session.AccessToken, nil
	case errors.Is(err, dockerhub.ErrNoSession):
		return "", nil
	default:
		return "", err
	}
}

// logError warns once per run of failures, except for a missing engine.
func (e *secretsEngine) logError(ctx context.Context, err error) {
	level := slog.LevelWarn
	if errors.Is(err, errNoSecretsEngine) || e.failureReported() {
		level = slog.LevelDebug
	}
	args := []any{"error", err}
	if hint := secretsEngineHint(err); hint != "" {
		args = append(args, "hint", hint)
	}
	e.logger.Log(ctx, level, secretsEngineFailureMsg, args...)
}

// failureReported reports whether this run of failures was already warned about.
func (e *secretsEngine) failureReported() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	reported := e.reported
	e.reported = true
	return reported
}
