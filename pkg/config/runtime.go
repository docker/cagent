package config

import "github.com/docker/cagent/pkg/environment"

type RuntimeConfig struct {
	DefaultEnvProvider environment.Provider
	EnvFiles           []string
	ModelsGateway      string
	RedirectURI        string
	GlobalCodeMode     bool
	WorkingDir         string
}
