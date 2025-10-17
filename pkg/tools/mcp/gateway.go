package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"

	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/tools"
)

type GatewayToolset struct {
	mcpServerName string
	config        any
	toolFilter    []string
	envProvider   environment.Provider

	once           sync.Once
	initErr        error
	cmdToolset     *Toolset
	cleanUpConfig  func() error
	cleanUpSecrets func() error
}

var _ tools.ToolSet = (*GatewayToolset)(nil)

func NewGatewayToolset(mcpServerName string, config any, toolFilter []string, envProvider environment.Provider) *GatewayToolset {
	slog.Debug("Creating MCP Gateway toolset", "name", mcpServerName, "toolFilter", toolFilter)

	return &GatewayToolset{
		mcpServerName: mcpServerName,
		config:        config,
		toolFilter:    toolFilter,
		envProvider:   envProvider,

		cleanUpConfig:  func() error { return nil },
		cleanUpSecrets: func() error { return nil },
	}
}

func (t *GatewayToolset) Instructions() string {
	return t.cmdToolset.Instructions()
}

func (t *GatewayToolset) configureOnce(ctx context.Context) error {
	// Check which secrets (env vars) are required by the MCP server.
	secrets, err := gateway.RequiredEnvVars(ctx, t.mcpServerName)
	if err != nil {
		return fmt.Errorf("reading which secrets the MCP server needs: %w", err)
	}

	// Make sure all the required secrets are available in the environment.
	// TODO(dga): Ideally, the MCP gateway would use the same provider that we have.
	fileSecrets, err := writeSecretsToFile(ctx, t.mcpServerName, secrets, t.envProvider)
	if err != nil {
		return fmt.Errorf("writing secrets to file: %w", err)
	}
	t.cleanUpSecrets = func() error { return os.Remove(fileSecrets) }

	fileConfig, err := writeConfigToFile(ctx, t.mcpServerName, t.config)
	if err != nil {
		return fmt.Errorf("writing config to file: %w", err)
	}
	t.cleanUpConfig = func() error { return os.Remove(fileConfig) }

	// Isolate ourselves from the MCP Toolkit config by always using the Docker MCP catalog and custom config and secrets.
	// This improves shareability of agents.
	args := []string{
		"mcp", "gateway", "run",
		"--servers", t.mcpServerName,
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

func (t *GatewayToolset) Stop(ctx context.Context) error {
	stopErr := t.cmdToolset.Stop(ctx)
	cleanUpSecretsErr := t.cleanUpSecrets()
	cleanUpConfigErr := t.cleanUpConfig()

	return errors.Join(stopErr, cleanUpSecretsErr, cleanUpConfigErr)
}

func (t *GatewayToolset) SetElicitationHandler(tools.ElicitationHandler) {
	// TODO: implement elicitations for the gateway
}

func (t *GatewayToolset) SetOAuthSuccessHandler(func()) {
	// No-op, as the gateway does not support OAuth
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
