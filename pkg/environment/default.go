package environment

import "github.com/docker/cagent/pkg/userconfig"

// NewDefaultProvider creates a provider chain with OS env, run secrets,
// credential helper (if configured), Docker Desktop, pass, and keychain providers.
func NewDefaultProvider() Provider {
	providers := []Provider{
		NewOsEnvProvider(),
		NewRunSecretsProvider(),
	}

	// Add credential helper provider if configured
	if cfg, err := userconfig.Load(); err == nil && cfg.CredentialHelper != nil && cfg.CredentialHelper.Command != "" {
		providers = append(providers, NewCredentialHelperProvider(cfg.CredentialHelper.Command, cfg.CredentialHelper.Args...))
	}

	// Docker Desktop provider comes after credential helper
	providers = append(providers, NewDockerDesktopProvider())

	// Append pass provider at the end if available
	if passProvider, err := NewPassProvider(); err == nil {
		providers = append(providers, passProvider)
	}

	// Append keychain provider if available
	if keychainProvider, err := NewKeychainProvider(); err == nil {
		providers = append(providers, keychainProvider)
	}

	return NewMultiProvider(providers...)
}
