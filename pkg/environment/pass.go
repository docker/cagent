package environment

import "context"

// PassProvider is a provider that retrieves secrets using the `pass` password
// manager.
type PassProvider struct{}

type PassNotAvailableError struct{}

func (PassNotAvailableError) Error() string {
	return "pass is not installed"
}

// NewPassProvider creates a new PassProvider instance.
func NewPassProvider() (*PassProvider, error) {
	if err := lookupBinary("pass", PassNotAvailableError{}); err != nil {
		return nil, err
	}
	return &PassProvider{}, nil
}

// Get retrieves the value of a secret by its name using the `pass` CLI.
// The name corresponds to the path in the `pass` store.
func (p *PassProvider) Get(ctx context.Context, name string) (string, bool) {
	return runCommand(ctx, "pass", "pass", "show", name)
}
