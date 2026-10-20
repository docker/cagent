package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/gateway"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

const (
	secretsFilePrefix = "mcp-secrets-"
	configFilePrefix  = "mcp-config-"
	staleTempFileAge  = 24 * time.Hour
)

type GatewayToolset struct {
	*Toolset

	mcpServerName string
	secrets       []gateway.Secret
	config        []byte
	envProvider   environment.Provider
}

var _ tools.ToolSet = (*GatewayToolset)(nil)

// NewGatewayToolset creates an inert MCP gateway toolset. Secret and config
// files are materialized only when Start or Restart acquires the subprocess.
func NewGatewayToolset(_ context.Context, name, mcpServerName string, secrets []gateway.Secret, config any, envProvider environment.Provider, cwd string, policy ...lifecycle.Policy) (*GatewayToolset, error) {
	configData, err := yaml.Marshal(map[string]any{mcpServerName: config})
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	inner := NewToolsetCommand(name, "docker", nil, nil, cwd, policy...)
	inner.description = "mcp(ref=" + mcpServerName + ")"
	t := &GatewayToolset{
		Toolset:       inner,
		mcpServerName: mcpServerName,
		secrets:       secrets,
		config:        configData,
		envProvider:   envProvider,
	}
	inner.supervisor = newSupervisorWithConnector(inner, firstOrZero(policy), &gatewayConnector{gateway: t})
	return t, nil
}

// gatewayConnector gives each connection attempt its own temporary files.
// Returning from a caller's Start wait does not end their lifetime.
type gatewayConnector struct{ gateway *GatewayToolset }

func (c *gatewayConnector) Connect(ctx context.Context) (lifecycle.Session, error) {
	t := c.gateway
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runStartupSweep(ctx)
	secretsFile, err := writeSecretsToFile(ctx, t.mcpServerName, t.secrets, t.envProvider)
	if err != nil {
		return nil, fmt.Errorf("writing secrets to file: %w", err)
	}
	configFile, err := writeTempFile(tempFilePattern(configFilePrefix), t.config)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("writing config to file: %w", err), removeIfExists(secretsFile))
	}
	cleanup := func() error { return errors.Join(removeIfExists(secretsFile), removeIfExists(configFile)) }
	client, ok := t.mcpClient.(*stdioMCPClient)
	if !ok {
		return nil, errors.Join(errors.New("gateway toolset requires stdio MCP client"), cleanup())
	}
	client.setArgs([]string{
		"mcp", "gateway", "run",
		"--servers", t.mcpServerName,
		"--catalog", gateway.DockerCatalogURL,
		"--secrets", secretsFile,
		"--config", configFile,
	})
	session, err := (&clientConnector{ts: t.Toolset}).Connect(ctx)
	if err != nil {
		return nil, errors.Join(err, cleanup())
	}
	return &gatewaySession{Session: session, cleanup: cleanup}, nil
}

type gatewaySession struct {
	lifecycle.Session

	cleanup func() error
	once    sync.Once
	err     error
}

func (s *gatewaySession) Close(ctx context.Context) error {
	s.once.Do(func() {
		s.err = errors.Join(s.Session.Close(ctx), s.cleanup())
	})
	return s.err
}

func removeIfExists(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func writeSecretsToFile(ctx context.Context, mcpServerName string, secrets []gateway.Secret, envProvider environment.Provider) (string, error) {
	var secretValues []string
	for _, secret := range secrets {
		v, found := envProvider.Get(ctx, secret.Env)
		if !found || v == "" {
			return "", errors.New("missing environment variable " + secret.Env + " required by MCP server " + mcpServerName)
		}
		if strings.ContainsAny(v, "\n\r") {
			return "", fmt.Errorf("secret %s contains newline characters", secret.Env)
		}
		secretValues = append(secretValues, fmt.Sprintf("%s=%s", secret.Name, v))
	}
	return writeTempFile(tempFilePattern(secretsFilePrefix), []byte(strings.Join(secretValues, "\n")))
}

func tempFilePattern(prefix string) string {
	return prefix + strconv.Itoa(os.Getpid()) + "-*"
}

func parseGatewayTempName(name string) (pidPart string, ok bool) {
	rest, found := strings.CutPrefix(name, secretsFilePrefix)
	if !found {
		rest, found = strings.CutPrefix(name, configFilePrefix)
	}
	if !found {
		return "", false
	}
	pidPart, random, hasPID := strings.Cut(rest, "-")
	if !hasPID {
		return "", isDigits(rest)
	}
	if !isDigits(pidPart) || !isDigits(random) {
		return "", false
	}
	return pidPart, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func sweepStaleGatewayTempFiles(dir string, currentPID int, now time.Time, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scanning temp dir: %w", err)
	}

	ownPIDPart := strconv.Itoa(currentPID)
	cutoff := now.Add(-maxAge)
	var firstErr error
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		pidPart, ours := parseGatewayTempName(e.Name())
		if !ours || pidPart == ownPIDPart {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

var startupSweepOnce sync.Once

func runStartupSweep(ctx context.Context) {
	startupSweepOnce.Do(func() {
		tempDir := os.TempDir()
		if err := sweepStaleGatewayTempFiles(tempDir, os.Getpid(), time.Now(), staleTempFileAge); err != nil {
			slog.WarnContext(ctx, "Failed to sweep stale MCP Gateway temp files", "error", err, "dir", tempDir)
		}
	})
}

func writeTempFile(nameTemplate string, content []byte) (string, error) {
	f, err := os.CreateTemp("", nameTemplate)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
