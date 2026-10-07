package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/secrets-engine/client/dockerhub"
)

const (
	// secretsEngineBudget bounds a single session lookup: the SDK sets no
	// request timeout of its own.
	secretsEngineBudget = 5 * time.Second

	// secretsEngineCooldown is how long a failing engine is skipped.
	secretsEngineCooldown = 30 * time.Second
)

// hubAuth reads Docker Hub sessions from the secrets engine. A var so tests
// can fake it.
var hubAuth = sync.OnceValues(newSecretsEngineHubAuth)

var errSecretsEngineCoolingDown = errors.New("secrets engine lookup failed recently, not retrying yet")

var secretsEngineState struct {
	sync.Mutex

	nextAttempt time.Time // earliest time the engine may be asked again
	reported    bool      // the current run of failures was logged as a warning
}

// secretsEngineToken returns a usable token from the secrets engine and caches
// it.
func secretsEngineToken(ctx context.Context) (string, bool) {
	token, err := fetchSecretsEngineToken(ctx)
	switch {
	case err != nil:
		logSecretsEngineError(ctx, err)
		return "", false
	case token == "":
		slog.DebugContext(ctx, "No Docker Hub session in the secrets engine")
		return "", false
	case !usable(token):
		// Fall through so minting or a forced refresh can replace it.
		slog.DebugContext(ctx, "The secrets engine served a token that expired, is about to, or was refused",
			"fingerprint", tokenFingerprint(token),
			"expires_in", expiresIn(token))
		return "", false
	case !remember(token, SourceSecretsEngine):
		// Refused while it was being looked up.
		return "", false
	}
	return token, true
}

// fetchSecretsEngineToken returns the default Docker Hub account's access
// token from the secrets engine, or "" when nobody is signed in.
func fetchSecretsEngineToken(ctx context.Context) (string, error) {
	if secretsEngineCoolingDown() {
		return "", errSecretsEngineCoolingDown
	}

	hub, err := hubAuth()
	if err != nil {
		return "", err
	}

	// A separate deadline, so the fallbacks keep the caller's context.
	lookupCtx, cancel := context.WithTimeout(ctx, secretsEngineBudget)
	defer cancel()

	session, err := hub.GetDefaultSession(lookupCtx)
	switch {
	case err == nil:
		secretsEngineAnswered()
		return session.AccessToken, nil
	case errors.Is(err, dockerhub.ErrNoSession):
		secretsEngineAnswered()
		return "", nil // nobody signed in
	case ctx.Err() != nil:
		// The caller gave up; that's not the engine's fault.
		if errors.Is(err, ctx.Err()) {
			return "", err
		}
		return "", fmt.Errorf("%w: %w", ctx.Err(), err)
	default:
		coolSecretsEngineDown()
		return "", err
	}
}

// logSecretsEngineError says why the engine couldn't be read. Most machines
// without Docker Desktop have no engine, so that's only logged at debug level.
// Other failures, such as access being denied, mean something is wrong. Only
// the first failure in a run is a warning, so a long-lived process doesn't
// repeat it after every cooldown.
func logSecretsEngineError(ctx context.Context, err error) {
	routine := errors.Is(err, errSecretsEngineCoolingDown) || ctx.Err() != nil || secretsEngineUnavailable(err)
	if routine || secretsEngineFailureReported() {
		slog.DebugContext(ctx, secretsEngineFailureMsg, "error", err)
		return
	}
	slog.WarnContext(ctx, secretsEngineFailureMsg, "error", err)
}

const secretsEngineFailureMsg = "Could not read the Docker Hub session from the secrets engine"

func secretsEngineCoolingDown() bool {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	return time.Now().Before(secretsEngineState.nextAttempt)
}

// coolSecretsEngineDown leaves the engine alone for secretsEngineCooldown.
func coolSecretsEngineDown() {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	secretsEngineState.nextAttempt = time.Now().Add(secretsEngineCooldown)
}

// secretsEngineAnswered ends a run of failures, so the next one is reported.
func secretsEngineAnswered() {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	secretsEngineState.reported = false
}

// secretsEngineFailureReported reports whether the current run of failures
// was already logged as a warning, and records that it now is.
func secretsEngineFailureReported() bool {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	reported := secretsEngineState.reported
	secretsEngineState.reported = true
	return reported
}
