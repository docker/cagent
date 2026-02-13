package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/session"
)

func TestSessionManager_CreateSession_WithToolHeaderOverrides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := session.NewInMemorySessionStore()
	runConfig := &config.RuntimeConfig{}

	sm := &SessionManager{
		sessionStore: store,
		runConfig:    runConfig,
	}

	// Test creating session with tool header overrides
	overrides := map[string]map[string]string{
		"github-mcp": {
			"Authorization": "Bearer session-token-123",
			"X-API-Version": "v1",
		},
		"slack-mcp": {
			"Authorization": "Bearer slack-token-456",
		},
	}

	sessionTemplate := &session.Session{
		MaxIterations:       10,
		ToolsApproved:       true,
		ToolHeaderOverrides: overrides,
	}

	sess, err := sm.CreateSession(ctx, sessionTemplate)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Verify session was created with overrides
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, 10, sess.MaxIterations)
	assert.True(t, sess.ToolsApproved)
	assert.Equal(t, overrides, sess.ToolHeaderOverrides)

	// Verify session was stored
	stored, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, overrides, stored.ToolHeaderOverrides)
}

func TestSessionManager_CreateSession_WithoutOverrides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := session.NewInMemorySessionStore()
	runConfig := &config.RuntimeConfig{}

	sm := &SessionManager{
		sessionStore: store,
		runConfig:    runConfig,
	}

	// Test creating session without overrides (backward compatibility)
	sessionTemplate := &session.Session{
		MaxIterations: 5,
		ToolsApproved: false,
	}

	sess, err := sm.CreateSession(ctx, sessionTemplate)
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, 5, sess.MaxIterations)
	assert.False(t, sess.ToolsApproved)
	assert.Nil(t, sess.ToolHeaderOverrides)
}

func TestSessionManager_ApplyToolHeaderOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		overrides         map[string]map[string]string
		expectedEnvVars   map[string]string
		expectedEnvVarLen int
	}{
		{
			name: "single toolset single header",
			overrides: map[string]map[string]string{
				"github-mcp": {
					"Authorization": "Bearer token123",
				},
			},
			expectedEnvVars: map[string]string{
				"CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION": "Bearer token123",
			},
			expectedEnvVarLen: 1,
		},
		{
			name: "single toolset multiple headers",
			overrides: map[string]map[string]string{
				"github-mcp": {
					"Authorization": "Bearer token123",
					"X-API-Version": "v1",
					"X-Custom-Header": "custom-value",
				},
			},
			expectedEnvVars: map[string]string{
				"CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION":   "Bearer token123",
				"CAGENT_TOOLSET_GITHUB_MCP_X_API_VERSION":   "v1",
				"CAGENT_TOOLSET_GITHUB_MCP_X_CUSTOM_HEADER": "custom-value",
			},
			expectedEnvVarLen: 3,
		},
		{
			name: "multiple toolsets",
			overrides: map[string]map[string]string{
				"github-mcp": {
					"Authorization": "Bearer github-token",
				},
				"slack-mcp": {
					"Authorization": "Bearer slack-token",
					"X-Slack-User": "user123",
				},
			},
			expectedEnvVars: map[string]string{
				"CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION": "Bearer github-token",
				"CAGENT_TOOLSET_SLACK_MCP_AUTHORIZATION":  "Bearer slack-token",
				"CAGENT_TOOLSET_SLACK_MCP_X_SLACK_USER":   "user123",
			},
			expectedEnvVarLen: 3,
		},
		{
			name: "header names with hyphens",
			overrides: map[string]map[string]string{
				"my-custom-toolset": {
					"X-My-Custom-Header": "value",
				},
			},
			expectedEnvVars: map[string]string{
				"CAGENT_TOOLSET_MY_CUSTOM_TOOLSET_X_MY_CUSTOM_HEADER": "value",
			},
			expectedEnvVarLen: 1,
		},
		{
			name:              "empty overrides",
			overrides:         map[string]map[string]string{},
			expectedEnvVars:   map[string]string{},
			expectedEnvVarLen: 0,
		},
		{
			name:              "nil overrides",
			overrides:         nil,
			expectedEnvVars:   map[string]string{},
			expectedEnvVarLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			runConfig := &config.RuntimeConfig{}

			// Create a base provider for testing
			baseProvider := config.NewMapProviderForTest(map[string]string{
				"BASE_VAR": "base-value",
			})
			runConfig.EnvProviderForTests = baseProvider

			sm := &SessionManager{}
			sess := &session.Session{
				ID:                  "test-session",
				ToolHeaderOverrides: tt.overrides,
			}

			// Apply the overrides
			augmentedConfig := sm.applyToolHeaderOverrides(ctx, sess, runConfig)

			// Verify the augmented provider contains expected environment variables
			provider := augmentedConfig.EnvProvider()

			// Check each expected env var
			for envKey, expectedValue := range tt.expectedEnvVars {
				value, found := provider.Get(ctx, envKey)
				assert.True(t, found, "Expected env var %s not found", envKey)
				assert.Equal(t, expectedValue, value, "Env var %s has wrong value", envKey)
			}

			// Verify base provider still works (fallback)
			baseValue, found := provider.Get(ctx, "BASE_VAR")
			assert.True(t, found)
			assert.Equal(t, "base-value", baseValue)

			// Verify we don't have extra env vars
			// (We can't easily count them without exposing internal state, so we just verify expected ones exist)
		})
	}
}

func TestSessionManager_ApplyToolHeaderOverrides_Priority(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runConfig := &config.RuntimeConfig{}

	// Base provider has a default value
	baseProvider := config.NewMapProviderForTest(map[string]string{
		"CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION": "Bearer default-token",
	})
	runConfig.EnvProviderForTests = baseProvider

	sm := &SessionManager{}
	sess := &session.Session{
		ID: "test-session",
		ToolHeaderOverrides: map[string]map[string]string{
			"github-mcp": {
				"Authorization": "Bearer session-override-token",
			},
		},
	}

	// Apply the overrides
	augmentedConfig := sm.applyToolHeaderOverrides(ctx, sess, runConfig)
	provider := augmentedConfig.EnvProvider()

	// Session override should take precedence over base provider
	value, found := provider.Get(ctx, "CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION")
	assert.True(t, found)
	assert.Equal(t, "Bearer session-override-token", value, "Session override should take precedence")
}

func TestSessionManager_ApplyToolHeaderOverrides_Clone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runConfig := &config.RuntimeConfig{}

	baseProvider := config.NewMapProviderForTest(map[string]string{
		"BASE_VAR": "original",
	})
	runConfig.EnvProviderForTests = baseProvider

	sm := &SessionManager{}
	sess := &session.Session{
		ID: "test-session",
		ToolHeaderOverrides: map[string]map[string]string{
			"github-mcp": {
				"Authorization": "Bearer token",
			},
		},
	}

	// Apply overrides - should return a new config
	augmentedConfig := sm.applyToolHeaderOverrides(ctx, sess, runConfig)

	// Verify original config is unchanged
	originalProvider := runConfig.EnvProvider()
	_, found := originalProvider.Get(ctx, "CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION")
	assert.False(t, found, "Original config should not have session-specific env vars")

	// Verify augmented config has the overrides
	augmentedProvider := augmentedConfig.EnvProvider()
	value, found := augmentedProvider.Get(ctx, "CAGENT_TOOLSET_GITHUB_MCP_AUTHORIZATION")
	assert.True(t, found)
	assert.Equal(t, "Bearer token", value)

	// Both should still have access to base vars
	baseValue, found := augmentedProvider.Get(ctx, "BASE_VAR")
	assert.True(t, found)
	assert.Equal(t, "original", baseValue)
}
