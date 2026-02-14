package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/telemetry"
)

func newExecCmd() *cobra.Command {
	var flags runExecFlags

	cmd := &cobra.Command{
		Use:   "exec <agent-file>|<registry-ref> <message>...",
		Short: "Execute an agent",
		Long:  "Execute an agent with one or more user messages (multi-turn, No TUI)",
		Example: `  cagent exec ./agent.yaml "What is Go?"
  cagent exec ./team.yaml --agent root "First question" "Follow-up question"
  echo "INSTRUCTIONS" | cagent exec ./echo.yaml -
  cagent exec ./agent.yaml "question" --record  # Records to auto-generated file`,
		GroupID:           "core",
		ValidArgsFunction: completeRunExec,
		Args:              cobra.MinimumNArgs(2),
		RunE:              flags.runExecCommand,
	}

	addRunOrExecFlags(cmd, &flags)
	addRuntimeConfigFlags(cmd, &flags.runConfig)
	cmd.PersistentFlags().BoolVar(&flags.hideToolCalls, "hide-tool-calls", false, "Hide the tool calls in the output")
	cmd.PersistentFlags().BoolVar(&flags.outputJSON, "json", false, "Output results in JSON format")

	return cmd
}

func (f *runExecFlags) runExecCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("exec", args)

	out := cli.NewPrinter(cmd.OutOrStdout())

	return f.runOrExec(cmd.Context(), out, args, false)
}
