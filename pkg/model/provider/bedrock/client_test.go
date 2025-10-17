package bedrock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
)

// testMapProvider is a simple map-based environment provider for testing
type testMapProvider struct {
	values map[string]string
}

func newTestMapProvider(values map[string]string) *testMapProvider {
	return &testMapProvider{values: values}
}

func (p *testMapProvider) Get(_ context.Context, name string) string {
	return p.values[name]
}

var _ environment.Provider = (*testMapProvider)(nil)

func TestNewClient_ValidConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
		"AWS_REGION":        "us-west-2",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "us-west-2", client.region)
	assert.Equal(t, "amazon-bedrock/anthropic.claude-3-5-sonnet-20241022-v2:0", client.ID())
}

func TestNewClient_CustomRegionFromConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "amazon.titan-text-express-v1",
		ProviderOpts: map[string]any{
			"region": "eu-west-1",
		},
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "eu-west-1", client.region)
}

func TestNewClient_ProviderOptsRegionTakesPrecedenceOverEnv(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	// Set both AWS_REGION env var and ProviderOpts region
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
		"AWS_REGION":        "us-west-2",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		ProviderOpts: map[string]any{
			"region": "eu-central-1", // Explicit config should take precedence
		},
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)
	require.NotNil(t, client)
	// ProviderOpts region should take precedence over AWS_REGION env var
	assert.Equal(t, "eu-central-1", client.region)
}

func TestNewClient_DefaultRegion(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "us-east-1", client.region) // Default region
}

func TestNewClient_CustomEndpoint(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		BaseURL:  "https://custom-bedrock-endpoint.example.com",
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestNewClient_WrongProviderType(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
	})

	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4",
	}

	_, err := NewClient(ctx, cfg, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model type must be 'amazon-bedrock'")
}

func TestNewClient_NilConfig(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{})

	_, err := NewClient(ctx, nil, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model configuration is required")
}

func TestClientID(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := newTestMapProvider(map[string]string{
		"AWS_BEDROCK_TOKEN": "test-token",
	})

	cfg := &latest.ModelConfig{
		Provider: "amazon-bedrock",
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
	}

	client, err := NewClient(ctx, cfg, env)
	require.NoError(t, err)

	expectedID := "amazon-bedrock/anthropic.claude-3-5-sonnet-20241022-v2:0"
	assert.Equal(t, expectedID, client.ID())
}
