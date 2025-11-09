package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/telemetry"
)

func newFeedbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "feedback",
		Short: "Send feedback about cagent",
		Long:  `Submit feedback or report issues with cagent`,
		Args:  cobra.NoArgs,
		RunE:  runFeedbackCommand,
	}
}

func runFeedbackCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("feedback", args)

	fmt.Fprintln(cmd.OutOrStdout(), "Feel free to give feedback:\n", feedback.FeedbackLink)
	return nil
}
