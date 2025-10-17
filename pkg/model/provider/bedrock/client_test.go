package bedrock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	latest "github.com/docker/cagent/pkg/config/v2"
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
	ctx := context.Background()
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
	ctx := context.Background()
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

func TestNewClient_DefaultRegion(t *testing.T) {
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
	env := newTestMapProvider(map[string]string{})

	_, err := NewClient(ctx, nil, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model configuration is required")
}

func TestGetModelFamily(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected string
	}{
		{
			name:     "Claude model",
			modelID:  "anthropic.claude-3-5-sonnet-20241022-v2:0",
			expected: "anthropic",
		},
		{
			name:     "Titan model",
			modelID:  "amazon.titan-text-express-v1",
			expected: "titan",
		},
		{
			name:     "Llama model",
			modelID:  "meta.llama3-70b-instruct-v1:0",
			expected: "llama",
		},
		{
			name:     "Mistral model",
			modelID:  "mistral.mistral-7b-instruct-v0:2",
			expected: "mistral",
		},
		{
			name:     "Unknown model",
			modelID:  "unknown.model-v1",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family := getModelFamily(tt.modelID)
			assert.Equal(t, tt.expected, family)
		})
	}
}

func TestClientID(t *testing.T) {
	ctx := context.Background()
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
