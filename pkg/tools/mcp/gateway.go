package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/tools"
	"gopkg.in/yaml.v3"
)

const DOCKER_MCP_GATEWAY_URL_ENV = "DOCKER_MCP_GATEWAY_URL"

type GatewayToolset struct {
	ref         string
	config      any
	toolFilter  []string
	envProvider environment.Provider

	once           sync.Once
	initErr        error
	cmdToolset     *Toolset
	cleanUpConfig  func() error
	cleanUpSecrets func() error
}

var _ tools.ToolSet = (*GatewayToolset)(nil)

func NewGatewayToolset(ref string, config any, toolFilter []string, envProvider environment.Provider) *GatewayToolset {
	slog.Debug("Creating MCP Gateway toolset", "ref", ref, "toolFilter", toolFilter)

	return &GatewayToolset{
		ref:            ref,
		config:         config,
		toolFilter:     toolFilter,
		envProvider:    envProvider,
		cleanUpConfig:  func() error { return nil },
		cleanUpSecrets: func() error { return nil },
	}
}

func (t *GatewayToolset) Instructions() string {
	return ""
}

func (t *GatewayToolset) configureOnce(ctx context.Context) error {
	if mcpGatewayURL := os.Getenv(DOCKER_MCP_GATEWAY_URL_ENV); mcpGatewayURL != "" {
		var err error

		t.cmdToolset, err = NewToolsetRemote(mcpGatewayURL, "streaming", nil, t.toolFilter, "")
		if err != nil {
			return fmt.Errorf("connecting to remote MCP Gateway: %w", err)
		}

		return nil
	}

	mcpServerName := gateway.ParseServerRef(t.ref)

	// Check which secrets (env vars) are required by the MCP server.
	secrets, err := gateway.RequiredEnvVars(ctx, mcpServerName, gateway.DockerCatalogURL)
	if err != nil {
		return fmt.Errorf("reading which secrets the MCP server needs: %w", err)
	}

	// Make sure all the required secrets are available in the environment.
	// TODO(dga): Ideally, the MCP gateway would use the same provider that we have.
	fileSecrets, err := writeSecretsToFile(ctx, mcpServerName, secrets, t.envProvider)
	if err != nil {
		return fmt.Errorf("writing secrets to file: %w", err)
	}
	t.cleanUpSecrets = func() error { return os.Remove(fileSecrets) }

	fileConfig, err := writeConfigToFile(ctx, mcpServerName, t.config)
	if err != nil {
		return fmt.Errorf("writing config to file: %w", err)
	}
	t.cleanUpConfig = func() error { return os.Remove(fileConfig) }

	// Isolate ourselves from the MCP Toolkit config by always using the Docker MCP catalog and custom config and secrets.
	// This improves shareability of agents.
	args := []string{
		"mcp", "gateway", "run",
		"--servers", mcpServerName,
		"--catalog", gateway.DockerCatalogURL,
		"--secrets", fileSecrets,
		"--config", fileConfig,
	}
	t.cmdToolset = NewToolsetCommand("docker", args, nil, t.toolFilter)

	return nil
}

func (t *GatewayToolset) ensureConfigured(ctx context.Context) error {
	t.once.Do(func() {
		t.initErr = t.configureOnce(ctx)
	})
	return t.initErr
}

func (t *GatewayToolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	if err := t.ensureConfigured(ctx); err != nil {
		return nil, err
	}
	return t.cmdToolset.Tools(ctx)
}

func (t *GatewayToolset) Start(ctx context.Context) error {
	if err := t.ensureConfigured(ctx); err != nil {
		return err
	}
	return t.cmdToolset.Start(ctx)
}

func (t *GatewayToolset) Stop() error {
	stopErr := t.cmdToolset.Stop()
	cleanUpSecretsErr := t.cleanUpSecrets()
	cleanUpConfigErr := t.cleanUpConfig()

	return errors.Join(stopErr, cleanUpSecretsErr, cleanUpConfigErr)
}

func writeSecretsToFile(ctx context.Context, mcpServerName string, secrets []gateway.Secret, envProvider environment.Provider) (string, error) {
	var secretValues []string
	for _, secret := range secrets {
		v := envProvider.Get(ctx, secret.Env)
		if v == "" {
			return "", errors.New("missing environment variable " + secret.Env + " required by MCP server " + mcpServerName)
		}

		secretValues = append(secretValues, fmt.Sprintf("%s=%s", secret.Name, v))
	}

	// We have all the secrets, let's create a file with all of them for the MCP Gateway
	return writeTempFile("mcp-secrets-*", []byte(strings.Join(secretValues, "\n")))
}

func writeConfigToFile(_ context.Context, mcpServerName string, config any) (string, error) {
	buf, err := yaml.Marshal(map[string]any{
		mcpServerName: config,
	})
	if err != nil {
		return "", err
	}

	return writeTempFile("mcp-config-*", buf)
}

func writeTempFile(nameTemplate string, content []byte) (string, error) {
	f, err := os.CreateTemp("", nameTemplate)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(content); err != nil {
		return "", err
	}

	return f.Name(), nil
}
