package root

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <agent-name>",
		Short: "Execute an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommand(cmd, args, true)
		},
	}

	cmd.PersistentFlags().StringVarP(&agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().StringVar(&workingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
	cmd.PersistentFlags().BoolVar(&autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().StringVar(&attachmentPath, "attach", "", "Attach an image file to the message")
	allOptions := GetAllHideOutputOptions()
	helpText := fmt.Sprintf("Hide output for specific tools (comma-separated). Available: %s", strings.Join(allOptions, ","))
	cmd.PersistentFlags().StringVar(&hideOutputFor, "hide-output-for", "", helpText)
	cmd.PersistentFlags().BoolVar(&showTokensEveryStep, "show-tokens-every-step", false, "Show token usage after every AI API call")
	addGatewayFlags(cmd)

	return cmd
}
