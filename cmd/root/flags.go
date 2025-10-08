package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
)

var (
	listenAddr string
	sessionDb  string
	runConfig  config.RuntimeConfig
)

func addRuntimeConfigFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().StringVar(&runConfig.RedirectURI, "redirect-uri", "", "Set the redirect URI for OAuth2 flows")
	cmd.PersistentFlags().BoolVar(&runConfig.GlobalCodeMode, "code-mode-tools", false, "Provide a single tool to call other tools via Javascript")
}
