package environment

import "context"

// KeychainProvider is a provider that retrieves secrets using the macOS keychain
// via the `security` command-line tool.
type KeychainProvider struct{}

type KeychainNotAvailableError struct{}

func (KeychainNotAvailableError) Error() string {
	return "security command is not available (macOS keychain access)"
}

// NewKeychainProvider creates a new KeychainProvider instance.
// It verifies that the `security` command is available on the system.
func NewKeychainProvider() (*KeychainProvider, error) {
	if err := lookupBinary("security", KeychainNotAvailableError{}); err != nil {
		return nil, err
	}
	return &KeychainProvider{}, nil
}

// Get retrieves the value of a secret by its service name from the macOS keychain.
// It uses the `security find-generic-password -w -s <name>` command to fetch the password.
func (p *KeychainProvider) Get(ctx context.Context, name string) (string, bool) {
	return runCommand(ctx, "keychain", "security", "find-generic-password", "-w", "-s", name)
}
