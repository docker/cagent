package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

func NewEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "eval <agent-name> <eval-dir>",
		Args: cobra.ExactArgs(2),
		RunE: runEvalCommand,
	}

	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	addGatewayFlags(cmd)

	return cmd
}

func runEvalCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("eval", args)

	agents, err := teamloader.Load(cmd.Context(), args[0], runConfig)
	if err != nil {
		return err
	}

	evalResults, err := evaluation.Evaluate(cmd.Context(), agents, args[1])
	if err != nil {
		return err
	}

	for _, evalResult := range evalResults {
		fmt.Printf("Eval file: %s\n", evalResult.EvalFile)
		fmt.Printf("Tool trajectory score: %f\n", evalResult.Score.ToolTrajectoryScore)
		fmt.Printf("Rouge-1 score: %f\n", evalResult.Score.Rouge1Score)
	}

	return nil
}
