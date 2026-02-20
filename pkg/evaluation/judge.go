package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// relevancePrompt is the prompt template for the judge model to evaluate responses.
const relevancePrompt = `You are an evaluation judge. Check if the response matches the given relevance criteria.

Response to evaluate:
<response>
%s
</response>

Criteria to check:
<criteria>
%s
</criteria>

Evaluate whether the response satisfies the criteria and respond with your judgment.`

// judgeResponseSchema defines the JSON schema for structured output from the judge model.
var judgeResponseSchema = &latest.StructuredOutput{
	Name:        "judge_response",
	Description: "Evaluation result for a relevance criterion",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"result": map[string]any{
				"type":        "string",
				"enum":        []string{"pass", "fail"},
				"description": "Whether the response satisfies the criterion",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Brief explanation of why the criterion passed or failed",
			},
		},
		"required":             []string{"result", "reason"},
		"additionalProperties": false,
	},
	Strict: true,
}

// Judge runs LLM-as-a-judge relevance checks concurrently.
type Judge struct {
	model       provider.Provider
	runConfig   *config.RuntimeConfig
	concurrency int
}

// NewJudge creates a new Judge that runs relevance checks with the given concurrency.
// Concurrency defaults to 1 if n < 1.
func NewJudge(model provider.Provider, runConfig *config.RuntimeConfig, concurrency int) *Judge {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Judge{
		model:       model,
		runConfig:   runConfig,
		concurrency: concurrency,
	}
}

// RelevanceResult contains the result of a single relevance check.
type RelevanceResult struct {
	Criterion string `json:"criterion"`
	Reason    string `json:"reason"`
}

// CheckRelevance runs all relevance checks concurrently with the configured concurrency.
// It returns the number of passed checks, a slice of failed results with reasons, and any errors encountered.
func (j *Judge) CheckRelevance(ctx context.Context, response string, criteria []string) (passed int, failed []RelevanceResult, errs []string) {
	if len(criteria) == 0 {
		return 0, nil, nil
	}

	// Create work channel
	type workItem struct {
		index     int
		criterion string
	}
	work := make(chan workItem, len(criteria))
	for i, c := range criteria {
		work <- workItem{index: i, criterion: c}
	}
	close(work)

	// Results slice preserves order
	type result struct {
		passed bool
		reason string
		err    error
	}
	results := make([]result, len(criteria))

	var wg sync.WaitGroup
	for range j.concurrency {
		wg.Go(func() {
			for item := range work {
				if ctx.Err() != nil {
					results[item.index] = result{err: fmt.Errorf("context cancelled: %w", ctx.Err())}
					continue
				}
				pass, reason, err := j.checkSingle(ctx, response, item.criterion)
				results[item.index] = result{passed: pass, reason: reason, err: err}
			}
		})
	}
	wg.Wait()

	// Aggregate results
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("error checking %q: %v", criteria[i], r.err))
			continue
		}
		if r.passed {
			passed++
		} else {
			failed = append(failed, RelevanceResult{
				Criterion: criteria[i],
				Reason:    r.reason,
			})
		}
	}

	return passed, failed, errs
}

// checkSingle checks a single relevance criterion against the response.
// It returns whether the check passed, the reason provided by the judge, and any error.
func (j *Judge) checkSingle(ctx context.Context, response, criterion string) (passed bool, reason string, err error) {
	modelCfg := j.model.BaseConfig().ModelConfig
	judgeWithSchema, err := provider.New(
		ctx,
		&modelCfg,
		j.runConfig.EnvProvider(),
		options.WithStructuredOutput(judgeResponseSchema),
	)
	if err != nil {
		return false, "", fmt.Errorf("creating judge provider with structured output: %w", err)
	}

	prompt := fmt.Sprintf(relevancePrompt, response, criterion)
	messages := []chat.Message{{Role: chat.MessageRoleUser, Content: prompt}}

	stream, err := judgeWithSchema.CreateChatCompletionStream(ctx, messages, nil)
	if err != nil {
		return false, "", fmt.Errorf("creating chat completion: %w", err)
	}
	defer stream.Close()

	var fullResponse strings.Builder
	var streamErr error
	for {
		resp, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				streamErr = err
			}
			break
		}
		for _, choice := range resp.Choices {
			fullResponse.WriteString(choice.Delta.Content)
		}
	}

	if streamErr != nil {
		return false, "", fmt.Errorf("streaming judge response: %w", streamErr)
	}

	raw := fullResponse.String()
	passed, reason, err = parseJudgeResponse(raw)
	if err != nil {
		slog.Warn("Failed to parse judge response",
			"criterion", criterion,
			"raw_response", raw,
			"error", err,
		)
		return false, "", fmt.Errorf("parsing judge response (length=%d): %w", len(raw), err)
	}

	slog.Debug("Judge response parsed successfully",
		"criterion", criterion,
		"passed", passed,
		"reason", reason,
	)

	return passed, reason, nil
}

// judgeResponse represents the structured response from the judge model.
type judgeResponse struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

// parseJudgeResponse parses a JSON judge response and returns whether the check
// passed, the reason, and any parse error.
func parseJudgeResponse(text string) (passed bool, reason string, err error) {
	text = strings.TrimSpace(text)

	var resp judgeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return false, "", fmt.Errorf("invalid JSON: %w", err)
	}

	if resp.Result == "" {
		slog.Warn("Judge response has empty result field",
			"raw_response", text,
			"reason_field", resp.Reason,
		)
	}

	return strings.EqualFold(resp.Result, "pass"), resp.Reason, nil
}
