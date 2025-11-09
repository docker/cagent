package root

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
)

const (
	flagModelsGateway = "models-gateway"
	envModelsGateway  = "CAGENT_MODELS_GATEWAY"
)

func canonize(endpoint string) string {
	return strings.TrimSuffix(strings.TrimSpace(endpoint), "/")
}

func logEnvvarShadowing(flagValue, varName, flagName string) {
	if flagValue != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Environment variable %s set, using it instead of CLI flag --%s\n", varName, flagName)
	}
}

func addGatewayFlags(cmd *cobra.Command, runConfig *config.RuntimeConfig) {
	cmd.PersistentFlags().StringVar(&runConfig.ModelsGateway, flagModelsGateway, "", "Set the models gateway address")

	persistentPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(_ *cobra.Command, args []string) error {
		if gateway := os.Getenv(envModelsGateway); gateway != "" {
			logEnvvarShadowing(runConfig.ModelsGateway, envModelsGateway, flagModelsGateway)
			runConfig.ModelsGateway = gateway
		}

		// Ensure the gateway url is canonical.
		runConfig.ModelsGateway = canonize(runConfig.ModelsGateway)

		// First call the original persistentPreRunE if it exists (from this command)
		if persistentPreRunE != nil {
			return persistentPreRunE(cmd, args)
		}

		// If this command doesn't have its own persistentPreRunE, check if the parent has one
		// This ensures parent PersistentPreRunE is called for child commands
		if cmd.Parent() != nil && cmd.Parent().PersistentPreRunE != nil {
			return cmd.Parent().PersistentPreRunE(cmd, args)
		}

		return nil
	}
}
