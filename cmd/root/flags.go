package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/userconfig"
)

const (
	flagModelsGateway = "models-gateway"
	envModelsGateway  = "CAGENT_MODELS_GATEWAY"
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
		// Precedence: CLI flag > environment variable > user config
		if runConfig.ModelsGateway == "" {
			if gateway := os.Getenv(envModelsGateway); gateway != "" {
				runConfig.ModelsGateway = gateway
			} else if userCfg, err := loadUserConfig(); err == nil && userCfg.ModelsGateway != "" {
				runConfig.ModelsGateway = userCfg.ModelsGateway
			}
		}

		runConfig.ModelsGateway = canonize(runConfig.ModelsGateway)

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
