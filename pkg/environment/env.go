package environment

import (
	"context"
	"os"
)

// OsEnvProvider provides access to the operating system's environment variables.
type OsEnvProvider struct{}

func NewOsEnvProvider() *OsEnvProvider {
	return &OsEnvProvider{}
}

func (p *OsEnvProvider) Get(_ context.Context, name string) (string, error) {
	return os.Getenv(name), nil
}

type EnvFilesProviders struct {
	absEnvFiles []string
}

func NewEnvFilesProvider(absEnvFiles []string) *EnvFilesProviders {
	return &EnvFilesProviders{
		absEnvFiles: absEnvFiles,
	}
}

func (p *EnvFilesProviders) Get(_ context.Context, name string) (string, error) {
	values, err := ReadEnvFiles(p.absEnvFiles)
	if err != nil {
		return "", err
	}

	for _, kv := range values {
		if kv.Key == name {
			return kv.Value, nil
		}
	}

	return "", nil
}
