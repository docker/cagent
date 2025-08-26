package root

import (
	"fmt"

	"github.com/docker/cagent/internal/telemetry"
	"github.com/spf13/cobra"
)

var FeedbackLink = "https://docker.qualtrics.com/jfe/form/SV_cNsCIg92nQemlfw"

// NewFeedbackCmd creates a new feedback command
func NewFeedbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "feedback",
		Short: "Send feedback about cagent",
		Long:  `Submit feedback or report issues with cagent`,
		Run: func(cmd *cobra.Command, args []string) {
			telemetry.TrackCommand("feedback", args)
			fmt.Println("Feel free to give feedback:\n", FeedbackLink)
		},
	}
}
