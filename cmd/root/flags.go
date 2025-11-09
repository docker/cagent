package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
)

func addRuntimeConfigFlags(cmd *cobra.Command, runConfig *config.RuntimeConfig) {
	addGatewayFlags(cmd, runConfig)
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().StringVar(&runConfig.RedirectURI, "redirect-uri", "http://localhost:8083/oauth-callback", "Set the redirect URI for OAuth2 flows")
	cmd.PersistentFlags().BoolVar(&runConfig.GlobalCodeMode, "code-mode-tools", false, "Provide a single tool to call other tools via Javascript")
}
