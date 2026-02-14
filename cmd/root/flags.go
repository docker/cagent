package root

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/server"
	"github.com/docker/cagent/pkg/userconfig"
)

const (
	flagModelsGateway = "models-gateway"
	envModelsGateway  = "CAGENT_MODELS_GATEWAY"
	envDefaultModel   = "CAGENT_DEFAULT_MODEL"
)

func addRuntimeConfigFlags(cmd *cobra.Command, runConfig *config.RuntimeConfig) {
	addGatewayFlags(cmd, runConfig)
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().BoolVar(&runConfig.GlobalCodeMode, "code-mode-tools", false, "Provide a single tool to call other tools via Javascript")
	cmd.PersistentFlags().StringVar(&runConfig.WorkingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
}

func setupWorkingDirectory(workingDir string) error {
	if workingDir == "" {
		return nil
	}

	absWd, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}

	info, err := os.Stat(absWd)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("working directory does not exist or is not a directory: %s", absWd)
	}

	if err := os.Chdir(absWd); err != nil {
		return fmt.Errorf("failed to change working directory: %w", err)
	}

	_ = os.Setenv("PWD", absWd)
	slog.Debug("Working directory set", "path", absWd)

	return nil
}

func canonize(endpoint string) string {
	return strings.TrimSuffix(strings.TrimSpace(endpoint), "/")
}

// loadUserConfig is the function used to load user configuration.
// It can be overridden in tests.
var loadUserConfig = userconfig.Load

func addGatewayFlags(cmd *cobra.Command, runConfig *config.RuntimeConfig) {
	cmd.PersistentFlags().StringVar(&runConfig.ModelsGateway, flagModelsGateway, "", "Set the models gateway address")

	persistentPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(_ *cobra.Command, args []string) error {
		userCfg, err := loadUserConfig()
		if err != nil {
			slog.Warn("Failed to load user config", "error", err)
			userCfg = &userconfig.Config{}
		}

		// Precedence: CLI flag > environment variable > user config
		if runConfig.ModelsGateway == "" {
			if gateway := os.Getenv(envModelsGateway); gateway != "" {
				runConfig.ModelsGateway = gateway
			} else if userCfg.ModelsGateway != "" {
				runConfig.ModelsGateway = userCfg.ModelsGateway
			}
		}
		runConfig.ModelsGateway = canonize(runConfig.ModelsGateway)

		// Precedence for default model: environment variable > user config
		if model := os.Getenv(envDefaultModel); model != "" {
			runConfig.DefaultModel = parseModelShorthand(model)
		} else if userCfg.DefaultModel != nil {
			runConfig.DefaultModel = &userCfg.DefaultModel.ModelConfig
		}

		if err := setupWorkingDirectory(runConfig.WorkingDir); err != nil {
			return err
		}

		if persistentPreRunE != nil {
			return persistentPreRunE(cmd, args)
		}
		if cmd.Parent() != nil && cmd.Parent().PersistentPreRunE != nil {
			return cmd.Parent().PersistentPreRunE(cmd, args)
		}

		return nil
	}
}

// parseModelShorthand parses "provider/model" into a ModelConfig
func parseModelShorthand(s string) *latest.ModelConfig {
	if idx := strings.Index(s, "/"); idx > 0 && idx < len(s)-1 {
		return &latest.ModelConfig{
			Provider: s[:idx],
			Model:    s[idx+1:],
		}
	}
	return nil
}

// listenAndCloseOnCancel starts a listener and spawns a goroutine
// that closes it when the context is cancelled.
func listenAndCloseOnCancel(ctx context.Context, addr string) (net.Listener, error) {
	ln, err := server.Listen(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	return ln, nil
}
