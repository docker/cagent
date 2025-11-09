package base

import (
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// Config is a common base configuration shared by all provider clients.
// It can be embedded in provider-specific Client structs to avoid code duplication.
type Config struct {
	ModelConfig  latest.ModelConfig
	ModelOptions options.ModelOptions
	Env          environment.Provider
}

// ID returns the provider and model ID in the format "provider/model"
func (c *Config) ID() string {
	return c.ModelConfig.Provider + "/" + c.ModelConfig.Model
}

func (c *Config) BaseConfig() Config {
	return *c
}
