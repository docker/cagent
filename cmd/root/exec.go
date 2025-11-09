package root

import (
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/telemetry"
)

func newExecCmd() *cobra.Command {
	var flags runExecFlags

	cmd := &cobra.Command{
		Use:   "exec <agent-name>",
		Short: "Execute an agent",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  flags.runExecCommand,
	}

	cmd.PersistentFlags().StringVarP(&flags.agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().StringVar(&flags.workingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
	cmd.PersistentFlags().BoolVar(&flags.autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().StringVar(&flags.attachmentPath, "attach", "", "Attach an image file to the message")
	cmd.PersistentFlags().StringArrayVar(&flags.modelOverrides, "model", nil, "Override agent model: [agent=]provider/model (repeatable)")
	cmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Initialize the agent without executing anything")
	_ = cmd.PersistentFlags().MarkHidden("dry-run")

	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *runExecFlags) runExecCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("exec", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())

	return f.runOrExec(ctx, out, args, true)
}
