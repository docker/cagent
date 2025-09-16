package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

type Score struct {
	ToolTrajectoryScore float64
	Rouge1Score         float64
}
type Result struct {
	Score    Score
	EvalFile string
}

func Evaluate(ctx context.Context, t *team.Team, evalsDir string) ([]Result, error) {
	evalFiles, err := os.ReadDir(evalsDir)
	if err != nil {
		return nil, err
	}

	var evals []session.Session
	for _, evalFile := range evalFiles {
		evalFile, err := os.ReadFile(filepath.Join(evalsDir, evalFile.Name()))
		if err != nil {
			return nil, err
		}
		var sess session.Session
		if err := json.Unmarshal(evalFile, &sess); err != nil {
			return nil, err
		}
		evals = append(evals, sess)
	}

	var results []Result
	for _, eval := range evals {
		rt, err := runtime.New(t)
		if err != nil {
			return nil, err
		}

		actualMessages, err := runLoop(ctx, rt, &eval)
		if err != nil {
			return nil, err
		}

		score := evaluate(eval.GetAllMessages(), actualMessages)

		results = append(results, Result{
			Score:    score,
			EvalFile: eval.ID,
		})
	}

	return results, nil
}

func runLoop(ctx context.Context, rt runtime.Runtime, eval *session.Session) ([]session.Message, error) {
	var userMessages []session.Message
	allMessages := eval.GetAllMessages()
	for i := range allMessages {
		if allMessages[i].Message.Role == chat.MessageRoleUser {
			userMessages = append(userMessages, allMessages[i])
		}
	}

	sess := session.New()
	for i := range userMessages {
		sess.AddMessage(&userMessages[i])
		_, err := rt.Run(ctx, sess)
		if err != nil {
			return nil, err
		}

		// Note: rt.Run now returns all messages, but we use sess.GetAllMessages() instead
	}

	return sess.GetAllMessages(), nil
}

func evaluate(expectedMessages, actualMessages []session.Message) Score {
	var expectedToolMessages []session.Message
	for i := range expectedMessages {
		if len(expectedMessages[i].Message.ToolCalls) != 0 {
			expectedToolMessages = append(expectedToolMessages, expectedMessages[i])
		}
	}

	var actualToolMessages []session.Message
	for i := range actualMessages {
		if len(actualMessages[i].Message.ToolCalls) != 0 {
			actualToolMessages = append(actualToolMessages, actualMessages[i])
		}
	}

	toolTrajectoryScore := toolTrajectoryScore(expectedToolMessages, actualToolMessages)
	rouge1Score := rouge1(expectedMessages[len(expectedMessages)-1].Message.Content, actualMessages[len(actualMessages)-1].Message.Content)

	return Score{
		ToolTrajectoryScore: toolTrajectoryScore,
		Rouge1Score:         rouge1Score,
	}
}

// https://medium.com/nlplanet/two-minutes-nlp-learn-the-rouge-metric-by-examples-f179cc285499
func rouge1(expected, actual string) float64 {
	expectedWords := strings.Fields(strings.ToLower(expected))
	actualWords := strings.Fields(strings.ToLower(actual))

	expectedSet := make(map[string]int)
	for _, word := range expectedWords {
		expectedSet[word]++
	}

	actualSet := make(map[string]int)
	for _, word := range actualWords {
		actualSet[word]++
	}

	overlap := 0
	for word, expectedCount := range expectedSet {
		if actualCount, exists := actualSet[word]; exists {
			if actualCount < expectedCount {
				overlap += actualCount
			} else {
				overlap += expectedCount
			}
		}
	}

	precision := float64(overlap) / float64(len(actualWords))
	recall := float64(overlap) / float64(len(expectedWords))

	if precision+recall == 0 {
		return 0.0
	}

	return 2 * (precision * recall) / (precision + recall)
}

func toolTrajectoryScore(expectedToolMessages, actualToolMessages []session.Message) float64 {
	score := 0.0

	for i := range expectedToolMessages {
		expected := expectedToolMessages[i]
		actual := actualToolMessages[i]

		for j := range actual.Message.ToolCalls {
			if actual.Message.ToolCalls[j].Function.Name == expected.Message.ToolCalls[j].Function.Name {
				score += 1.0
			}
		}
	}

	return score / float64(len(expectedToolMessages))
}

func Save(sess *session.Session) error {
	if err := os.MkdirAll("evals", 0o755); err != nil {
		return err
	}

	fileName := sess.ID + ".json"
	if _, err := os.Stat("evals/" + fileName); err == nil {
		number := 1
		for {
			fileName = fmt.Sprintf("%s_%d.json", sess.ID, number)
			if _, err := os.Stat("evals/" + fileName); err != nil {
				break
			}
			number++
		}
	}

	file, err := os.Create(filepath.Join("evals", fileName))
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(sess)
}
