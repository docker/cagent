package config

import (
	"net/http"

	"github.com/docker/cagent/pkg/environment"
)

type RuntimeConfig struct {
	DefaultEnvProvider environment.Provider
	EnvFiles           []string
	ModelsGateway      string
	RedirectURI        string
	GlobalCodeMode     bool
	WorkingDir         string
	HTTPClient         http.RoundTripper
}
