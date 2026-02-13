package config

import (
	"log/slog"
	"sync"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
)

type RuntimeConfig struct {
	Config

	EnvProviderForTests environment.Provider
	envProvider         environment.Provider
	envProviderLock     sync.Mutex
}

type Config struct {
	EnvFiles       []string
	ModelsGateway  string
	DefaultModel   *latest.ModelConfig
	GlobalCodeMode bool
	WorkingDir     string
}

func (runConfig *RuntimeConfig) Clone() *RuntimeConfig {
	return &RuntimeConfig{
		Config: runConfig.Config,
	}
}

func (runConfig *RuntimeConfig) EnvProvider() environment.Provider {
	if runConfig.EnvProviderForTests != nil {
		return runConfig.EnvProviderForTests
	}

	runConfig.envProviderLock.Lock()
	defer runConfig.envProviderLock.Unlock()

	env := runConfig.computedEnvProvider()
	runConfig.envProvider = env
	return env
}

// SetEnvProvider sets a custom environment provider for this runtime config.
// This is useful for injecting session-specific environment variables that
// take precedence over the default computed provider.
func (runConfig *RuntimeConfig) SetEnvProvider(provider environment.Provider) {
	runConfig.envProviderLock.Lock()
	defer runConfig.envProviderLock.Unlock()
	runConfig.envProvider = provider
}

func (runConfig *RuntimeConfig) computedEnvProvider() environment.Provider {
	defaultEnv := environment.NewDefaultProvider()

	// Make env file paths absolute relative to the working directory.
	var err error
	runConfig.EnvFiles, err = environment.AbsolutePaths(runConfig.WorkingDir, runConfig.EnvFiles)
	if err != nil {
		slog.Error("Failed to make env file paths absolute", "error", err)
		return defaultEnv
	}

	envFilesProviders, err := environment.NewEnvFilesProvider(runConfig.EnvFiles)
	if err != nil {
		slog.Error("Failed to read env files", "error", err)
		return defaultEnv
	}

	// Update the env provider to include env files
	return environment.NewMultiProvider(envFilesProviders, defaultEnv)
}
