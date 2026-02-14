package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/modelsdev"
)

// mockEnvProvider is a simple environment provider for testing
type mockEnvProvider struct {
	vars map[string]string
}

func (m *mockEnvProvider) Get(_ context.Context, name string) (string, bool) {
	v, ok := m.vars[name]
	return v, ok
}

// mockCatalogStore implements CatalogStore for testing
type mockCatalogStore struct {
	db *modelsdev.Database
}

func (m *mockCatalogStore) GetModel(_ string) (*modelsdev.Model, error) {
	return nil, nil
}

func (m *mockCatalogStore) GetDatabase() (*modelsdev.Database, error) {
	return m.db, nil
}

func TestIsInlineAlloySpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		modelRef string
		want     bool
	}{
		{
			name:     "single inline model",
			modelRef: "openai/gpt-4o",
			want:     false,
		},
		{
			name:     "two inline models",
			modelRef: "openai/gpt-4o,anthropic/claude-sonnet-4-0",
			want:     true,
		},
		{
			name:     "three inline models",
			modelRef: "openai/gpt-4o,anthropic/claude-sonnet-4-0,google/gemini-2.0-flash",
			want:     true,
		},
		{
			name:     "with spaces",
			modelRef: "openai/gpt-4o, anthropic/claude-sonnet-4-0",
			want:     true,
		},
		{
			name:     "named model (no slash)",
			modelRef: "my_fast_model",
			want:     false,
		},
		{
			name:     "comma separated named models (not inline alloy)",
			modelRef: "fast_model,smart_model",
			want:     false,
		},
		{
			name:     "mixed named and inline",
			modelRef: "fast_model,openai/gpt-4o",
			want:     false, // "fast_model" doesn't contain "/" so it's not an inline alloy
		},
		{
			name:     "empty string",
			modelRef: "",
			want:     false,
		},
		{
			name:     "just commas",
			modelRef: ",,",
			want:     false, // No valid parts after trimming
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isInlineAlloySpec(tt.modelRef)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsEmbeddingModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		family string
		model  string
		want   bool
	}{
		// Family-based detection
		{"family text-embedding", "text-embedding", "Text Embedding 3 Large", true},
		{"family cohere-embed", "cohere-embed", "Embed v4.0", true},
		{"family mistral-embed", "mistral-embed", "Mistral Embed", true},
		{"family gemini-embedding", "gemini-embedding", "Gemini Embedding", true},
		// Name-based fallback (empty family)
		{"name fallback embed", "", "Text Embedding 3 Large", true},
		{"name fallback mistral", "", "Mistral Embed", true},
		// Non-embedding models
		{"gpt family", "gpt", "GPT-4o", false},
		{"claude family", "claude-sonnet", "Claude Sonnet 4", false},
		{"llama family", "llama", "Llama 3.1 70B", false},
		{"empty both", "", "GPT-4o", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isEmbeddingModel(tt.family, tt.model)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapModelsDevProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		providerID   string
		wantProvider string
		wantSupport  bool
	}{
		{"openai", "openai", true},
		{"anthropic", "anthropic", true},
		{"google", "google", true},
		{"mistral", "mistral", true},
		{"xai", "xai", true},
		{"amazon-bedrock", "amazon-bedrock", true},
		{"unsupported-provider", "", false},
		{"cohere", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.providerID, func(t *testing.T) {
			t.Parallel()
			gotProvider, gotSupport := mapModelsDevProvider(tt.providerID)
			assert.Equal(t, tt.wantProvider, gotProvider)
			assert.Equal(t, tt.wantSupport, gotSupport)
		})
	}
}

func TestGetAvailableProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		envVars       map[string]string
		modelsGateway string
		wantProviders []string
	}{
		{
			name: "only openai key",
			envVars: map[string]string{
				"OPENAI_API_KEY": "sk-test",
			},
			wantProviders: []string{"openai", "dmr", "ollama"},
		},
		{
			name: "openai and anthropic keys",
			envVars: map[string]string{
				"OPENAI_API_KEY":    "sk-test",
				"ANTHROPIC_API_KEY": "sk-ant-test",
			},
			wantProviders: []string{"openai", "anthropic", "dmr", "ollama"},
		},
		{
			name: "with gateway - uses docker token",
			envVars: map[string]string{
				"DOCKER_TOKEN": "test-token",
			},
			modelsGateway: "https://gateway.example.com",
			wantProviders: []string{"openai", "anthropic", "google", "mistral", "xai"},
		},
		{
			name: "with gateway but no token",
			envVars: map[string]string{
				"OPENAI_API_KEY": "sk-test", // This is ignored when gateway is set
			},
			modelsGateway: "https://gateway.example.com",
			wantProviders: []string{}, // No token means no providers via gateway
		},
		{
			name: "aws access key for bedrock",
			envVars: map[string]string{
				"AWS_ACCESS_KEY_ID": "AKIA...",
			},
			wantProviders: []string{"amazon-bedrock", "dmr", "ollama"},
		},
		{
			name: "aws profile for bedrock",
			envVars: map[string]string{
				"AWS_PROFILE": "my-profile",
			},
			wantProviders: []string{"amazon-bedrock", "dmr", "ollama"},
		},
		{
			name: "aws web identity for bedrock (EKS/IRSA)",
			envVars: map[string]string{
				"AWS_WEB_IDENTITY_TOKEN_FILE": "/var/run/secrets/token",
			},
			wantProviders: []string{"amazon-bedrock", "dmr", "ollama"},
		},
		{
			name: "aws container credentials for bedrock (ECS)",
			envVars: map[string]string{
				"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI": "/v2/credentials/...",
			},
			wantProviders: []string{"amazon-bedrock", "dmr", "ollama"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &LocalRuntime{
				modelSwitcherCfg: &ModelSwitcherConfig{
					EnvProvider:   &mockEnvProvider{vars: tt.envVars},
					ModelsGateway: tt.modelsGateway,
				},
			}

			got := r.getAvailableProviders(t.Context())

			for _, want := range tt.wantProviders {
				assert.True(t, got[want], "expected provider %s to be available", want)
			}
		})
	}
}

func TestBuildCatalogChoices(t *testing.T) {
	t.Parallel()

	// Create a mock database with some models
	db := &modelsdev.Database{
		Providers: map[string]modelsdev.Provider{
			"openai": {
				ID:   "openai",
				Name: "OpenAI",
				Models: map[string]modelsdev.Model{
					"gpt-4o": {
						ID:   "gpt-4o",
						Name: "GPT-4o",
						Modalities: modelsdev.Modalities{
							Output: []string{"text"},
						},
					},
					"dall-e-3": {
						ID:   "dall-e-3",
						Name: "DALL-E 3",
						Modalities: modelsdev.Modalities{
							Output: []string{"image"}, // Not a text model
						},
					},
					"text-embedding-3-large": {
						ID:     "text-embedding-3-large",
						Name:   "Text Embedding 3 Large",
						Family: "text-embedding",
						Modalities: modelsdev.Modalities{
							Output: []string{"text"}, // Embedding model - identified by family field
						},
					},
				},
			},
			"anthropic": {
				ID:   "anthropic",
				Name: "Anthropic",
				Models: map[string]modelsdev.Model{
					"claude-sonnet-4-0": {
						ID:   "claude-sonnet-4-0",
						Name: "Claude Sonnet 4",
						Modalities: modelsdev.Modalities{
							Output: []string{"text"},
						},
					},
				},
			},
			"unsupported": {
				ID:   "unsupported",
				Name: "Unsupported Provider",
				Models: map[string]modelsdev.Model{
					"some-model": {
						ID:   "some-model",
						Name: "Some Model",
						Modalities: modelsdev.Modalities{
							Output: []string{"text"},
						},
					},
				},
			},
		},
	}

	r := &LocalRuntime{
		modelsStore: &mockCatalogStore{db: db},
		modelSwitcherCfg: &ModelSwitcherConfig{
			EnvProvider: &mockEnvProvider{vars: map[string]string{
				"OPENAI_API_KEY":    "sk-test",
				"ANTHROPIC_API_KEY": "sk-ant-test",
			}},
			Models: map[string]latest.ModelConfig{
				"my_model": {Provider: "openai", Model: "gpt-4o"}, // This should be excluded from catalog (duplicate)
			},
		},
	}

	choices := r.buildCatalogChoices(t.Context())

	// Should include Claude Sonnet (not a duplicate)
	var foundClaude bool
	for _, c := range choices {
		if c.Ref == "anthropic/claude-sonnet-4-0" {
			foundClaude = true
			assert.True(t, c.IsCatalog)
			assert.Equal(t, "Claude Sonnet 4", c.Name)
		}
	}
	require.True(t, foundClaude, "should include Claude Sonnet from catalog")

	// Should NOT include DALL-E 3 (not a text model)
	for _, c := range choices {
		assert.NotEqual(t, "openai/dall-e-3", c.Ref, "should not include non-text models")
	}

	// Should NOT include embedding models
	for _, c := range choices {
		assert.NotEqual(t, "openai/text-embedding-3-large", c.Ref, "should not include embedding models")
	}

	// Should NOT include unsupported provider
	for _, c := range choices {
		assert.NotEqual(t, "unsupported", c.Provider, "should not include unsupported providers")
	}
}

func TestBuildCatalogChoicesWithDuplicates(t *testing.T) {
	t.Parallel()

	db := &modelsdev.Database{
		Providers: map[string]modelsdev.Provider{
			"openai": {
				ID:   "openai",
				Name: "OpenAI",
				Models: map[string]modelsdev.Model{
					"gpt-4o": {
						ID:   "gpt-4o",
						Name: "GPT-4o",
						Modalities: modelsdev.Modalities{
							Output: []string{"text"},
						},
					},
				},
			},
		},
	}

	r := &LocalRuntime{
		modelsStore: &mockCatalogStore{db: db},
		modelSwitcherCfg: &ModelSwitcherConfig{
			EnvProvider: &mockEnvProvider{vars: map[string]string{
				"OPENAI_API_KEY": "sk-test",
			}},
			Models: map[string]latest.ModelConfig{
				// This model has the same provider/model as the catalog entry
				"my_gpt4o": {Provider: "openai", Model: "gpt-4o"},
			},
		},
	}

	choices := r.buildCatalogChoices(t.Context())

	// Should NOT include gpt-4o since it's already in config
	for _, c := range choices {
		assert.NotEqual(t, "openai/gpt-4o", c.Ref, "should not include duplicates from config")
	}
}
