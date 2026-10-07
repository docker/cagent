package desktop

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/docker/secrets-engine/client/dockerhub"
)

// Vars, not consts, so tests can shorten them.
var (
	// secretsEngineBudget bounds a single session lookup.
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
}

// fetchSecretsEngineToken returns the default Docker Hub account's access
// token from the secrets engine, or "" when nobody is signed in.
func fetchSecretsEngineToken(ctx context.Context) (string, error) {
	secretsEngineState.Lock()
	coolingDown := time.Now().Before(secretsEngineState.nextAttempt)
	secretsEngineState.Unlock()
	if coolingDown {
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
		return session.AccessToken, nil
	case errors.Is(err, dockerhub.ErrNoSession):
		return "", nil // nobody signed in
	case ctx.Err() != nil:
		// The caller gave up; that's not the engine's fault.
		return "", ctx.Err()
	default:
		secretsEngineState.Lock()
		secretsEngineState.nextAttempt = time.Now().Add(secretsEngineCooldown)
		secretsEngineState.Unlock()
		return "", err
	}
}
