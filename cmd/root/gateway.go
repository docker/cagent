package root

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	flagGateway       = "gateway"
	flagModelsGateway = "models-gateway"
	envGateway        = "CAGENT_GATEWAY"
	envModelsGateway  = "CAGENT_MODELS_GATEWAY"
)

type gatewayConfig struct {
	mainGateway string
}

var gwConfig gatewayConfig

func canonize(endpoint string) string {
	return strings.TrimSpace(strings.TrimSuffix(endpoint, "/"))
}

func logEnvvarShadowing(flagValue, varName, flagName string) {
	if flagValue != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Environment variable %s set, using it instead of CLI flag --%s\n", varName, flagName)
	}
}

func addGatewayFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&gwConfig.mainGateway, flagGateway, "", "Set the gateway address to use for models and tool calls")
	cmd.PersistentFlags().StringVar(&runConfig.ModelsGateway, flagModelsGateway, "", "Set the models gateway address")

	// Don't allow gateway to be specified if a qualified gateway flag is provided
	cmd.MarkFlagsMutuallyExclusive(flagGateway, flagModelsGateway)

	persistentPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// verify mutual exclusion for environment variables
		if os.Getenv(envGateway) != "" && os.Getenv(envModelsGateway) != "" {
			return fmt.Errorf("environment variables %s and %s cannot be set at the same time", envGateway, envModelsGateway)
		}

		// Get gateway value from the environment.
		// This behavior sets both the models and tools gateway
		mainGateway := os.Getenv(envGateway)
		if mainGateway != "" {
			logEnvvarShadowing(gwConfig.mainGateway, envGateway, flagGateway)
			gwConfig.mainGateway = mainGateway
			runConfig.ModelsGateway = mainGateway
		}

		if gateway := os.Getenv(envModelsGateway); gateway != "" {
			logEnvvarShadowing(runConfig.ModelsGateway, envModelsGateway, flagModelsGateway)
			runConfig.ModelsGateway = gateway
		}

		// Set the qualified gateways to the main gateway if they haven't been set explicitly
		if runConfig.ModelsGateway == "" {
			runConfig.ModelsGateway = gwConfig.mainGateway
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
