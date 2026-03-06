package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCatalogProviders(t *testing.T) {
	t.Parallel()

	providers := CatalogProviders()

	// Should include all core providers
	for _, core := range CoreProviders {
		assert.Contains(t, providers, core, "should include core provider %s", core)
	}

	// Should include aliases with BaseURL
	for name, alias := range Aliases {
		if alias.BaseURL != "" {
			assert.Contains(t, providers, name, "should include alias %s with BaseURL", name)
		} else {
			assert.NotContains(t, providers, name, "should NOT include alias %s without BaseURL", name)
		}
	}
}

func TestIsCatalogProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		// Core providers
		{"openai is core", "openai", true},
		{"anthropic is core", "anthropic", true},
		{"google is core", "google", true},
		{"dmr is core", "dmr", true},
		{"amazon-bedrock is core", "amazon-bedrock", true},

		// Aliases with BaseURL (should be included)
		{"mistral has BaseURL", "mistral", true},
		{"xai has BaseURL", "xai", true},
		{"nebius has BaseURL", "nebius", true},
		{"requesty has BaseURL", "requesty", true},
		{"ollama has BaseURL", "ollama", true},

		// Aliases without BaseURL (should be excluded)
		{"azure has no BaseURL", "azure", false},

		// Unknown providers
		{"unknown provider", "unknown", false},
		{"cohere not supported", "cohere", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsCatalogProvider(tt.provider)
			assert.Equal(t, tt.want, got)
		})
	}
}
