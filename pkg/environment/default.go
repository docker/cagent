package environment

import "context"

func NewDefaultProvider(ctx context.Context) Provider {
	var providers []Provider

	providers = append(providers, NewOsEnvProvider())

	// Append 1Password provider at the end if available
	if onePasswordProvider, err := NewOnePasswordProvider(ctx); err == nil {
		providers = append(providers, NewNoFailProvider(onePasswordProvider))
	}

	// Append pass provider at the end if available
	if passProvider, err := NewPassProvider(); err == nil {
		providers = append(providers, NewNoFailProvider(passProvider))
	}

	// Append keychain provider if available
	if keychainProvider, err := NewKeychainProvider(); err == nil {
		providers = append(providers, NewNoFailProvider(keychainProvider))
	}

	return NewMultiProvider(providers...)
}
