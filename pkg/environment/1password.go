package environment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/1password/onepassword-sdk-go"
)

type OnePasswordProvider struct {
	secrets onepassword.SecretsAPI
}

func NewOnePasswordProvider(ctx context.Context) (*OnePasswordProvider, error) {
	opToken := os.Getenv("OP_SERVICE_ACCOUNT_TOKEN")
	if opToken == "" {
		return nil, errors.New("OP_SERVICE_ACCOUNT_TOKEN environment variable is required for 1Password integration")
	}

	client, err := onepassword.NewClient(ctx,
		onepassword.WithServiceAccountToken(opToken),
		onepassword.WithIntegrationInfo("cagent 1Password Integration", "v1.0.0"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to 1Password: %w", err)
	}

	return &OnePasswordProvider{
		secrets: client.Secrets(),
	}, nil
}

func (p *OnePasswordProvider) Get(ctx context.Context, name string) (string, error) {
	path := "op://cagent/" + name + "/password"
	slog.Debug("Looking for environment variable in 1Password", "path", path)

	secret, err := p.secrets.Resolve(ctx, "op://cagent/"+name+"/password")
	if err != nil {
		return "", fmt.Errorf("failed to find environment variable (%s) in 1Password: %w", name, err)
	}

	return secret, nil
}
