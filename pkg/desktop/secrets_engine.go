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
	// secretsEngineBudget bounds a whole session lookup. The engine may wait
	// on the user (an access prompt) and the token is wanted by a request
	// already in flight: past this, the next source is tried instead.
	secretsEngineBudget = 5 * time.Second

	// secretsEngineCooldown keeps an engine that is unreachable, unresponsive
	// or refusing access from adding latency to every lookup. A missing
	// session is a clean miss and is not cooled down: the user may sign in at
	// any time.
	secretsEngineCooldown = 30 * time.Second
)

// hubAuth reads Docker Hub sessions from the secrets engine. The connection
// is set up once; a var so tests never reach a real engine.
var hubAuth = sync.OnceValues(newSecretsEngineHubAuth)

// errSecretsEngineCoolingDown means a recent lookup failed and the engine is
// left alone until [secretsEngineCooldown] has passed.
var errSecretsEngineCoolingDown = errors.New("secrets engine lookup failed recently, not retrying yet")

var secretsEngineState struct {
	sync.Mutex

	nextAttempt time.Time // earliest time the engine may be asked again
}

// fetchSecretsEngineToken returns the access token of the default Docker Hub
// account, as stored in the secrets engine by Docker Desktop's sign-in. It
// returns ("", nil) when no account is signed in there.
//
// The engine only ever adds a source: every failure is reported to the caller,
// which moves on to the next one, so machines without it (CI runners, Linux
// without Docker Desktop, older Desktop releases) behave as before.
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

	// Bound the lookup on its own context: the caller's stays intact for the
	// sources tried after this one.
	lookupCtx, cancel := context.WithTimeout(ctx, secretsEngineBudget)
	defer cancel()

	session, err := hub.GetDefaultSession(lookupCtx)
	switch {
	case err == nil:
		return session.AccessToken, nil
	case errors.Is(err, dockerhub.ErrNoSession):
		// Also covers dockerhub.ErrNoDefaultProfile: nobody is signed in.
		return "", nil
	case ctx.Err() != nil:
		// The caller gave up, which says nothing about the engine.
		return "", ctx.Err()
	default:
		secretsEngineState.Lock()
		secretsEngineState.nextAttempt = time.Now().Add(secretsEngineCooldown)
		secretsEngineState.Unlock()
		return "", err
	}
}
