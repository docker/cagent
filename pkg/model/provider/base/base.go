package base

import (
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// Config is a common base configuration shared by all provider clients.
// It can be embedded in provider-specific Client structs to avoid code duplication.
type Config struct {
	ModelConfig  *latest.ModelConfig
	ModelOptions options.ModelOptions
}

// ID returns the provider and model ID in the format "provider/model"
func (c *Config) ID() string {
	return c.ModelConfig.Provider + "/" + c.ModelConfig.Model
}

// MaxTokens returns the maximum tokens configured for this provider's model
func (c *Config) MaxTokens() int {
	if c.ModelConfig == nil {
		return 0
	}
	return c.ModelConfig.MaxTokens
}

// Options returns the effective model options used by this provider's model
func (c *Config) Options() options.ModelOptions {
	return c.ModelOptions
}
