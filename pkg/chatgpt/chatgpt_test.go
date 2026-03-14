package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overrideTokenPath redirects the token store to a temp directory for the
// duration of the test. Because it mutates a package-level variable, tests
// that use this helper must NOT be marked parallel.
func overrideTokenPath(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	orig := tokenFilePathFunc
	tokenFilePathFunc = func() string { return filepath.Join(dir, "chatgpt_token.json") }
	t.Cleanup(func() { tokenFilePathFunc = orig })
}

// overrideTokenEndpoint redirects token HTTP calls to the given test server
// for the duration of the test.
func overrideTokenEndpoint(t *testing.T, url string) {
	t.Helper()

	orig := tokenEndpointURL
	tokenEndpointURL = url
	t.Cleanup(func() { tokenEndpointURL = orig })
}

func TestToken_IsExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		token    Token
		expected bool
	}{
		{
			name:     "zero expiry is never expired",
			token:    Token{AccessToken: "test"},
			expected: false,
		},
		{
			name: "future expiry is not expired",
			token: Token{
				AccessToken: "test",
				ExpiresAt:   time.Now().Add(10 * time.Minute),
			},
			expected: false,
		},
		{
			name: "past expiry is expired",
			token: Token{
				AccessToken: "test",
				ExpiresAt:   time.Now().Add(-10 * time.Minute),
			},
			expected: true,
		},
		{
			name: "expiry within 60 seconds is considered expired",
			token: Token{
				AccessToken: "test",
				ExpiresAt:   time.Now().Add(30 * time.Second),
			},
			expected: true,
		},
		{
			name: "expiry beyond 60 seconds is not expired",
			token: Token{
				AccessToken: "test",
				ExpiresAt:   time.Now().Add(90 * time.Second),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.token.IsExpired())
		})
	}
}

// TestTokenStore_SaveLoadRemove tests the token store using a helper that
// overrides the package-level tokenFilePathFunc. Because mutating a package
// global is not parallel-safe, this test is NOT marked parallel.
func TestTokenStore_SaveLoadRemove(t *testing.T) {
	overrideTokenPath(t)

	// Initially no token
	token, err := LoadToken()
	require.NoError(t, err)
	assert.Nil(t, token)

	// Save a token
	testToken := &Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Truncate(time.Second),
	}
	require.NoError(t, SaveToken(testToken))

	// Load it back
	loaded, err := LoadToken()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, testToken.AccessToken, loaded.AccessToken)
	assert.Equal(t, testToken.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, testToken.TokenType, loaded.TokenType)

	// Remove it
	require.NoError(t, RemoveToken())

	// Gone
	token, err = LoadToken()
	require.NoError(t, err)
	assert.Nil(t, token)
}

func TestTokenStore_RemoveNoFile(t *testing.T) {
	overrideTokenPath(t)

	require.NoError(t, RemoveToken())
}

func TestTokenStore_InvalidJSON(t *testing.T) {
	overrideTokenPath(t)

	require.NoError(t, os.WriteFile(tokenFilePath(), []byte("not json"), 0o600))

	_, err := LoadToken()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse token file")
}

func TestRefreshAccessToken(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First call: token refresh (JSON body)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id_token":      "new-id-token",
				"access_token":  "new-access-token",
				"refresh_token": "", // no new refresh token
			})
		} else {
			// Second call: API key exchange (form body)
			assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "new-api-key",
			})
		}
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	token, err := RefreshAccessToken(t.Context(), "test-refresh")
	require.NoError(t, err)
	assert.Equal(t, "new-api-key", token.AccessToken)
	assert.Equal(t, "test-refresh", token.RefreshToken) // preserved when empty in response
	assert.Equal(t, "new-id-token", token.IDToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.False(t, token.ExpiresAt.IsZero())
}

func TestRefreshAccessToken_NewRefreshToken(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id_token":      "new-id-token",
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "new-api-key",
			})
		}
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	token, err := RefreshAccessToken(t.Context(), "old-refresh")
	require.NoError(t, err)
	assert.Equal(t, "new-refresh-token", token.RefreshToken)
}

func TestRefreshAccessToken_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	_, err := RefreshAccessToken(t.Context(), "test-refresh")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token refresh failed")
}

func TestProvider_Get_NonMatchingVar(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	val, ok := p.Get(t.Context(), "OTHER_VAR")
	assert.Empty(t, val)
	assert.False(t, ok)
}

func TestProvider_Get_NoToken(t *testing.T) {
	overrideTokenPath(t)

	p := NewProvider()
	val, ok := p.Get(t.Context(), TokenEnvVar)
	assert.Empty(t, val)
	assert.False(t, ok)
}

func TestProvider_Get_ValidToken(t *testing.T) {
	overrideTokenPath(t)

	testToken := &Token{
		AccessToken:  "valid-token",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	require.NoError(t, SaveToken(testToken))

	p := NewProvider()
	val, ok := p.Get(t.Context(), TokenEnvVar)
	assert.True(t, ok)
	assert.Equal(t, "valid-token", val)
}

func TestProvider_Get_ExpiredTokenRefreshes(t *testing.T) {
	overrideTokenPath(t)

	// Set up a mock token endpoint that handles refresh + API key exchange
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id_token":     "refreshed-id",
				"access_token": "refreshed-access",
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "refreshed-api-key",
			})
		}
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	// Save an expired token with a refresh token
	expiredToken := &Token{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, SaveToken(expiredToken))

	p := NewProvider()
	val, ok := p.Get(t.Context(), TokenEnvVar)
	assert.True(t, ok)
	assert.Equal(t, "refreshed-api-key", val)
}

func TestProvider_Get_ExpiredTokenNoRefresh(t *testing.T) {
	overrideTokenPath(t)

	// Save an expired token without a refresh token
	expiredToken := &Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, SaveToken(expiredToken))

	p := NewProvider()
	val, ok := p.Get(t.Context(), TokenEnvVar)
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestProvider_GetAccessToken_NotLoggedIn(t *testing.T) {
	overrideTokenPath(t)

	p := NewProvider()
	_, err := p.GetAccessToken(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not logged in")
}

func TestProvider_GetAccessToken_ValidToken(t *testing.T) {
	overrideTokenPath(t)

	testToken := &Token{
		AccessToken: "my-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	require.NoError(t, SaveToken(testToken))

	p := NewProvider()
	val, err := p.GetAccessToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "my-access-token", val)
}

func TestBuildAuthURL(t *testing.T) {
	t.Parallel()

	authURL := buildAuthURL("http://localhost:1455/auth/callback", "test-state", "test-challenge")
	assert.Contains(t, authURL, authorizationEndpoint)
	assert.Contains(t, authURL, "response_type=code")
	assert.Contains(t, authURL, "client_id="+clientID)
	assert.Contains(t, authURL, "redirect_uri=http")
	assert.Contains(t, authURL, "state=test-state")
	assert.Contains(t, authURL, "code_challenge=test-challenge")
	assert.Contains(t, authURL, "code_challenge_method=S256")
}

func TestGenerateState(t *testing.T) {
	t.Parallel()

	s1, err := generateState()
	require.NoError(t, err)
	assert.Len(t, s1, 32) // 16 bytes hex-encoded

	s2, err := generateState()
	require.NoError(t, err)
	assert.NotEqual(t, s1, s2) // should be random
}

func TestExchangeCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		assert.Equal(t, "test-code", r.Form.Get("code"))
		assert.Equal(t, "test-verifier", r.Form.Get("code_verifier"))
		assert.Equal(t, clientID, r.Form.Get("client_id"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id_token":      "exchanged-id-token",
			"access_token":  "exchanged-access",
			"refresh_token": "exchanged-refresh",
		})
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	token, err := exchangeCode(t.Context(), "test-code", "test-verifier", "http://localhost:1455/auth/callback")
	require.NoError(t, err)
	assert.Equal(t, "exchanged-access", token.AccessToken)
	assert.Equal(t, "exchanged-id-token", token.IDToken)
	assert.Equal(t, "exchanged-refresh", token.RefreshToken)
	assert.False(t, token.ExpiresAt.IsZero())
}

func TestExchangeForAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:token-exchange", r.Form.Get("grant_type"))
		assert.Equal(t, clientID, r.Form.Get("client_id"))
		assert.Equal(t, "openai-api-key", r.Form.Get("requested_token"))
		assert.Equal(t, "my-id-token", r.Form.Get("subject_token"))
		assert.Equal(t, "urn:ietf:params:oauth:token-type:id_token", r.Form.Get("subject_token_type"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sk-api-key-12345",
		})
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	apiKey, err := exchangeForAPIKey(t.Context(), "my-id-token")
	require.NoError(t, err)
	assert.Equal(t, "sk-api-key-12345", apiKey)
}

func TestExchangeForAPIKey_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	overrideTokenEndpoint(t, server.URL)

	_, err := exchangeForAPIKey(t.Context(), "bad-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key exchange failed")
}

func TestProvider_ContextCancellation(t *testing.T) {
	t.Parallel()

	p := NewProvider()

	// Non-matching key should return quickly regardless
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	val, ok := p.Get(ctx, "OTHER_VAR")
	assert.Empty(t, val)
	assert.False(t, ok)
}
