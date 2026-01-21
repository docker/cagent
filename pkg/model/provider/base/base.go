package base

import (
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// Config is a common base configuration shared by all provider clients.
// It can be embedded in provider-specific Client structs to avoid code duplication.
type Config struct {
	ModelConfig  latest.ModelConfig
	ModelOptions options.ModelOptions
	Env          environment.Provider
	// Models stores the full models map for providers that need it (e.g., routers).
	// This enables proper cloning of providers that reference other models.
	Models map[string]latest.ModelConfig
}

// ID returns the provider and model ID in the format "provider/model"
func (c *Config) ID() string {
	return c.ModelConfig.Provider + "/" + c.ModelConfig.Model
}

func (c *Config) BaseConfig() Config {
	return *c
}

// EmbeddingResult contains the embedding and usage information
type EmbeddingResult struct {
	Embedding   []float64
	InputTokens int64
	TotalTokens int64
	Cost        float64
}

// BatchEmbeddingResult contains multiple embeddings and usage information
type BatchEmbeddingResult struct {
	Embeddings  [][]float64
	InputTokens int64
	TotalTokens int64
	Cost        float64
}
