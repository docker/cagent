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

// hubAuth reads Docker Hub sessions from the secrets engine; a var for tests.
var hubAuth = sync.OnceValues(newSecretsEngineHubAuth)

// hubStaging reports whether Docker Hub staging is in use; a var for tests.
var hubStaging = hubauth.Staging

// secretsEngineHubOptions reads the staging realms when Docker Hub staging is in use.
func secretsEngineHubOptions() []dockerhub.Option {
	if hubStaging() {
		return []dockerhub.Option{dockerhub.Staging()}
	}
	return nil
}

var (
	errSecretsEngineCoolingDown = errors.New("secrets engine lookup failed recently, not retrying yet")
	errSecretsEngineSlow        = errors.New("secrets engine did not answer in time")
)

var secretsEngineState struct {
	sync.Mutex

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

// secretsEngineToken returns a usable token from the secrets engine and caches it.
func secretsEngineToken(ctx context.Context) (string, bool) {
	token, err := fetchSecretsEngineToken(ctx)
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

// fetchSecretsEngineToken waits at most half of ctx's remaining time for the shared lookup.
func fetchSecretsEngineToken(ctx context.Context) (string, error) {
	lookup, err := joinSecretsEngineLookup(ctx)
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

// joinSecretsEngineLookup returns the running lookup, or starts one.
func joinSecretsEngineLookup(ctx context.Context) (*secretsEngineLookup, error) {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()

	if lookup := secretsEngineState.inflight; lookup != nil {
		return lookup, nil
	}
	if time.Now().Before(secretsEngineState.nextAttempt) {
		return nil, errSecretsEngineCoolingDown
	}

	lookup := &secretsEngineLookup{done: make(chan struct{})}
	secretsEngineState.inflight = lookup
	go runSecretsEngineLookup(context.WithoutCancel(ctx), lookup)
	return lookup, nil
}

// runSecretsEngineLookup reads the session; a failure is logged and starts the cooldown.
func runSecretsEngineLookup(ctx context.Context, lookup *secretsEngineLookup) {
	token, err := lookUpSecretsEngineSession(ctx)
	if err != nil {
		logSecretsEngineError(ctx, err)
	}

	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	if err != nil {
		secretsEngineState.nextAttempt = time.Now().Add(secretsEngineCooldown)
	} else {
		secretsEngineState.reported = false
	}
	secretsEngineState.inflight = nil
	lookup.token, lookup.err = token, err
	close(lookup.done)
}

// lookUpSecretsEngineSession returns the default account's access token, or "" when signed out.
func lookUpSecretsEngineSession(ctx context.Context) (string, error) {
	hub, err := hubAuth()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, secretsEngineBudget)
	defer cancel()

	session, err := hub.GetDefaultSession(ctx)
	switch {
	case err == nil:
		return session.AccessToken, nil
	case errors.Is(err, dockerhub.ErrNoSession):
		return "", nil
	default:
		return "", err
	}
}

// logSecretsEngineError warns once per run of failures, except for a missing engine.
func logSecretsEngineError(ctx context.Context, err error) {
	level := slog.LevelWarn
	if errors.Is(err, errNoSecretsEngine) || secretsEngineFailureReported() {
		level = slog.LevelDebug
	}
	args := []any{"error", err}
	if hint := secretsEngineHint(err); hint != "" {
		args = append(args, "hint", hint)
	}
	slog.Log(ctx, level, secretsEngineFailureMsg, args...)
}

const secretsEngineFailureMsg = "Could not read the Docker Hub session from the secrets engine"

// secretsEngineFailureReported reports whether this run of failures was already warned about.
func secretsEngineFailureReported() bool {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	reported := secretsEngineState.reported
	secretsEngineState.reported = true
	return reported
}
