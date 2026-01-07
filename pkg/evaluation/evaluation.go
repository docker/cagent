package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
)

type Score struct {
	ToolTrajectoryScore float64
	Rouge1Score         float64
}

type Result struct {
	EvalFile     string
	FirstMessage string
	Score        Score
	Cost         float64
	OutputTokens int64
}

type Printer interface {
	Printf(format string, a ...any)
}

func Evaluate(ctx context.Context, out Printer, agentFilename, evalsDir string, runConfig *config.RuntimeConfig) error {
	agentSource, err := config.Resolve(agentFilename)
	if err != nil {
		return err
	}

	agents, err := teamloader.Load(ctx, agentSource, runConfig)
	if err != nil {
		return err
	}

	evals, err := loadEvalSessions(ctx, evalsDir)
	if err != nil {
		return err
	}

	runEvals := make([]func() (Result, error), len(evals))
	for i := range evals {
		runEvals[i] = sync.OnceValues(func() (Result, error) {
			return runSingleEvaluation(ctx, agents, evals[i])
		})
	}

	var index atomic.Int32
	for range 4 {
		go func() {
			for {
				i := index.Add(1) - 1
				if i >= int32(len(evals)) {
					break
				}
				_, _ = runEvals[i]()
			}
		}()
	}

	for i := range evals {
		result, err := runEvals[i]()
		if err != nil {
			return err
		}

		out.Printf("--- %d\n", i)
		out.Printf("First message: %s\n", result.FirstMessage)
		out.Printf("Eval file: %s\n", result.EvalFile)
		out.Printf("Tool trajectory score: %f\n", result.Score.ToolTrajectoryScore)
		out.Printf("Rouge-1 score: %f\n", result.Score.Rouge1Score)
		out.Printf("Cost: %.2f\n", result.Cost)
		out.Printf("Output tokens: %d\n", result.OutputTokens)
		out.Printf("\n")
	}

	return nil
}

// loadEvalSessions reads all evaluation session files from the given directory.
func loadEvalSessions(ctx context.Context, evalsDir string) ([]*session.Session, error) {
	evalFiles, err := os.ReadDir(evalsDir)
	if err != nil {
		return nil, err
	}

	var evals []*session.Session
	for _, evalFile := range evalFiles {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		data, err := os.ReadFile(filepath.Join(evalsDir, evalFile.Name()))
		if err != nil {
			return nil, err
		}

		var sess session.Session
		if err := json.Unmarshal(data, &sess); err != nil {
			return nil, err
		}

		evals = append(evals, &sess)
	}

	return evals, nil
}

// runSingleEvaluation runs a single evaluation and returns the result.
func runSingleEvaluation(ctx context.Context, t *team.Team, eval *session.Session) (Result, error) {
	rt, err := runtime.New(t)
	if err != nil {
		return Result{}, err
	}

	evalMessages := eval.GetAllMessages()

	sess, err := runLoop(ctx, rt, eval)
	if err != nil {
		return Result{}, err
	}
	actualMessages := sess.GetAllMessages()

	return Result{
		EvalFile:     eval.ID,
		FirstMessage: evalMessages[0].Message.Content,
		Score:        score(evalMessages, actualMessages),
		Cost:         sess.Cost,
		OutputTokens: sess.OutputTokens,
	}, nil
}

func runLoop(ctx context.Context, rt *runtime.LocalRuntime, eval *session.Session) (*session.Session, error) {
	var userMessages []session.Message
	allMessages := eval.GetAllMessages()
	for i := range allMessages {
		if allMessages[i].Message.Role == chat.MessageRoleUser {
			userMessages = append(userMessages, allMessages[i])
		}
	}

	sess := session.New(
		session.WithToolsApproved(true),
		session.WithMaxIterations(rt.CurrentAgent().MaxIterations()),
	)

	for i := range userMessages {
		sess.AddMessage(&userMessages[i])

		_, err := rt.Run(ctx, sess)
		if err != nil {
			return nil, err
		}
	}

	return sess, nil
}
