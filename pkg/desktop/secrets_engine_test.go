package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTokenFromSecretsEngine(t *testing.T) {
	t.Run("the engine session is preferred to Docker Desktop's backend", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromEngine := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: makeToken(t, time.Now().Add(time.Hour))})
			installFakeEngine(t, &fakeEngine{token: fromEngine})

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromEngine, token)
			assert.Equal(t, SourceSecretsEngine, source)
		})
	})

	t.Run("an engine token is served from memory", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromEngine := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{})
			engine := &fakeEngine{token: fromEngine}
			installFakeEngine(t, engine)

			require.Equal(t, fromEngine, GetToken(t.Context()))
			lookups := engine.lookupCount()

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromEngine, token)
			assert.Equal(t, SourceSecretsEngine, source)
			assert.Equal(t, lookups, engine.lookupCount(), "gateway clients call this per request")
		})
	})

	t.Run("nobody signed in falls back without cooling down", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromDesktop := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: fromDesktop})
			engine := &fakeEngine{}
			installFakeEngine(t, engine)

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromDesktop, token)
			assert.Equal(t, SourceDesktop, source)

			// The user signs in: the engine is asked again as soon as the
			// cached token is due for a re-check.
			fromEngine := makeToken(t, time.Now().Add(time.Hour))
			engine.setToken(fromEngine)
			expireCache()

			token, source = GetTokenWithSource(t.Context())
			assert.Equal(t, fromEngine, token)
			assert.Equal(t, SourceSecretsEngine, source)
		})
	})

	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "engine unavailable", err: errors.New("secrets engine is not available: dial unix engine.sock: connect: no such file or directory")},
		{name: "access denied", err: secrets.ErrAccessDenied},
		{name: "lookup failed", err: errors.New("internal error")},
	} {
		t.Run(tt.name+" falls back and cools down", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fromDesktop := makeToken(t, time.Now().Add(time.Hour))
				installFakeBackend(t, &fakeBackend{token: fromDesktop})
				engine := &fakeEngine{err: tt.err}
				installFakeEngine(t, engine)

				token, source := GetTokenWithSource(t.Context())
				assert.Equal(t, fromDesktop, token)
				assert.Equal(t, SourceDesktop, source)
				lookups := engine.lookupCount()
				require.Positive(t, lookups)

				expireCache()
				assert.Equal(t, fromDesktop, GetToken(t.Context()))
				assert.Equal(t, lookups, engine.lookupCount(), "a failing engine is left alone for a while")

				endSecretsEngineCooldown()
				expireCache()
				assert.Equal(t, fromDesktop, GetToken(t.Context()))
				assert.Greater(t, engine.lookupCount(), lookups, "the engine is asked again after the cooldown")
			})
		})
	}

	t.Run("an engine client that can't be created falls back", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromDesktop := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: fromDesktop})
			hubAuth = func() (dockerhub.ClientAuth, error) {
				return nil, errors.New("the secrets engine is not available on this platform")
			}

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromDesktop, token)
			assert.Equal(t, SourceDesktop, source)
		})
	})

	t.Run("an expiring engine token falls back to Docker Desktop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromDesktop := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: fromDesktop})
			installFakeEngine(t, &fakeEngine{token: makeToken(t, time.Now().Add(10*time.Second))})

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromDesktop, token)
			assert.Equal(t, SourceDesktop, source)
		})
	})

	t.Run("an expired engine token falls through to minting", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// The engine and Desktop's backend share Desktop's token source: a
			// stuck refresher leaves both stale.
			expired := makeToken(t, time.Now().Add(-time.Hour))
			minted := makeToken(t, time.Now().Add(time.Hour))
			backend := &fakeBackend{token: expired}
			installFakeBackend(t, backend)
			installFakeEngine(t, &fakeEngine{token: expired})
			mintToken = func(context.Context) (string, error) { return minted, nil }

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, minted, token)
			assert.Equal(t, SourceMinted, source)
			assert.Equal(t, 0, backend.refreshes())
		})
	})

	t.Run("a refused engine token is not served again", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromEngine := makeToken(t, time.Now().Add(time.Hour))
			fromDesktop := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: fromDesktop})
			installFakeEngine(t, &fakeEngine{token: fromEngine})
			require.Equal(t, fromEngine, GetToken(t.Context()))

			// The engine keeps serving the token Docker refused.
			InvalidateToken(fromEngine)

			token, source := GetTokenWithSource(t.Context())
			assert.Equal(t, fromDesktop, token)
			assert.Equal(t, SourceDesktop, source)
		})
	})

	t.Run("an unresponsive engine delays the lookup by its budget only", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			fromDesktop := makeToken(t, time.Now().Add(time.Hour))
			installFakeBackend(t, &fakeBackend{token: fromDesktop})
			engine := &fakeEngine{block: true}
			installFakeEngine(t, engine)

			start := time.Now()
			token, source := GetTokenWithSource(t.Context())
			elapsed := time.Since(start)

			// The engine's deadline must not cut the caller's lookup short.
			assert.Equal(t, fromDesktop, token)
			assert.Equal(t, SourceDesktop, source)
			assert.GreaterOrEqual(t, elapsed, secretsEngineBudget)
			assert.Less(t, elapsed, secretsEngineBudget+time.Second)

			lookups := engine.lookupCount()
			expireCache()
			start = time.Now()
			assert.Equal(t, fromDesktop, GetToken(t.Context()))
			assert.Less(t, time.Since(start), time.Second, "an unresponsive engine is not waited on again right away")
			assert.Equal(t, lookups, engine.lookupCount())
		})
	})
}

func TestFetchSecretsEngineTokenCanceledCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := &fakeEngine{block: true}
		installFakeEngine(t, engine)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := fetchSecretsEngineToken(ctx)
		require.ErrorIs(t, err, context.Canceled)

		// A caller giving up says nothing about the engine: it is not cooled
		// down, so the next caller asks it again.
		fromEngine := makeToken(t, time.Now().Add(time.Hour))
		engine.setBlock(false)
		engine.setToken(fromEngine)

		token, err := fetchSecretsEngineToken(t.Context())
		require.NoError(t, err)
		assert.Equal(t, fromEngine, token)
	})
}

// installFakeEngine points the secrets engine lookup at engine, through the
// real SDK accessor so the realms and payloads it decodes are exercised too.
func installFakeEngine(t *testing.T, engine *fakeEngine) {
	t.Helper()

	oldHubAuth := hubAuth
	hubAuth = func() (dockerhub.ClientAuth, error) { return dockerhub.New(engine), nil }
	t.Cleanup(func() { hubAuth = oldHubAuth })

	endSecretsEngineCooldown()
	t.Cleanup(endSecretsEngineCooldown)
}

func endSecretsEngineCooldown() {
	secretsEngineState.Lock()
	defer secretsEngineState.Unlock()
	secretsEngineState.nextAttempt = time.Time{}
}

// fakeEngine emulates the secrets engine Docker Desktop serves: the default
// account's profile and its session, as Desktop's sign-in stores them.
type fakeEngine struct {
	mu      sync.Mutex
	token   string // the default account's access token; "" when signed out
	err     error  // returned by every lookup when set
	block   bool   // lookups wait for their context to end
	lookups int
}

func (e *fakeEngine) setToken(token string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.token = token
}

func (e *fakeEngine) setBlock(block bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.block = block
}

func (e *fakeEngine) lookupCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lookups
}

func (e *fakeEngine) GetSecrets(ctx context.Context, pattern secrets.Pattern) ([]secrets.Envelope, error) {
	e.mu.Lock()
	e.lookups++
	token, err, block := e.token, e.err, e.block
	e.mu.Unlock()

	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, secrets.ErrNotFound
	}

	var value any
	switch pattern.String() {
	case "docker/auth/metadata/hub/default":
		value = dockerhub.Profile{UserID: "docker/auth/hub/testuser", Username: "testuser"}
	case "docker/auth/hub/testuser":
		value = dockerhub.UserSession{AccessToken: token}
	default:
		return nil, secrets.ErrNotFound
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return []secrets.Envelope{{ID: secrets.MustParseID(pattern.String()), Value: data}}, nil
}
