package environment

func NewDefaultProvider() Provider {
	var providers []Provider

	providers = append(providers, NewOsEnvProvider(), NewRunSecretsProvider())

	// Append pass provider at the end if available
	if passProvider, err := NewPassProvider(); err == nil {
		providers = append(providers, passProvider)
	}

	// Append keychain provider if available
	if keychainProvider, err := NewKeychainProvider(); err == nil {
		providers = append(providers, keychainProvider)
	}

	// Append Docker Desktop provider last
	providers = append(providers, NewDockerDesktopProvider())

	return NewMultiProvider(providers...)
}
