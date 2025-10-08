package root

import (
	"github.com/docker/cagent/pkg/config"
	"github.com/spf13/cobra"
)

var (
	listenAddr string
	sessionDb  string
	runConfig  config.RuntimeConfig
)

func addRuntimeConfigFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().BoolVar(&runConfig.GlobalCodeMode, "code-mode-tools", false, "Provide a single tool to call other tools via Javascript")
}
