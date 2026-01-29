package evaluation

import (
	"fmt"
	"time"

	"github.com/docker/cagent/pkg/session"
)

// EvalCriteria contains the evaluation criteria for a test case.
type EvalCriteria struct {
	Relevance  []string `json:"relevance,omitempty"`   // Statements that should be true about the response
	WorkingDir string   `json:"working_dir,omitempty"` // Subdirectory under evals/working_dirs/
	Size       string   `json:"size,omitempty"`        // Expected response size: S, M, L, XL
}

// EvalSession extends session.Session with evaluation criteria.
type EvalSession struct {
	session.Session
	Evals      EvalCriteria `json:"evals"`
	SourcePath string       `json:"-"` // Path to the source eval file (not serialized)
}

// Result contains the evaluation results for a single test case.
type Result struct {
	InputPath         string           `json:"input_path"`
	Title             string           `json:"title"`
	Question          string           `json:"question"`
	Response          string           `json:"response"`
	Cost              float64          `json:"cost"`
	OutputTokens      int64            `json:"output_tokens"`
	Size              string           `json:"size"`
	SizeExpected      string           `json:"size_expected"`
	ToolCallsScore    float64          `json:"tool_calls_score"`
	ToolCallsExpected float64          `json:"tool_calls_score_expected"`
	HandoffsMatch     bool             `json:"handoffs"`
	RelevancePassed   float64          `json:"relevance"`
	RelevanceExpected float64          `json:"relevance_expected"`
	FailedRelevance   []string         `json:"failed_relevance,omitempty"`
	Error             string           `json:"error,omitempty"`
	RawOutput         []map[string]any `json:"raw_output,omitempty"`
}

// checkResults returns successes and failures for this result.
func (r *Result) checkResults() (successes, failures []string) {
	if r.Error != "" {
		return nil, []string{r.Error}
	}

	// Check size
	if r.SizeExpected != "" {
		if r.SizeExpected == r.Size {
			successes = append(successes, fmt.Sprintf("size %s", r.Size))
		} else {
			failures = append(failures, fmt.Sprintf("size expected %s, got %s", r.SizeExpected, r.Size))
		}
	}

	// Check tool calls
	if r.ToolCallsExpected > 0 {
		if r.ToolCallsScore >= 1.0 {
			successes = append(successes, "tool calls")
		} else {
			failures = append(failures, fmt.Sprintf("tool calls score %.2f", r.ToolCallsScore))
		}
	}

	// Check handoffs
	if r.HandoffsMatch {
		successes = append(successes, "handoffs")
	} else {
		failures = append(failures, "handoffs mismatch")
	}

	// Check relevance
	if r.RelevanceExpected > 0 {
		if r.RelevancePassed >= r.RelevanceExpected {
			successes = append(successes, fmt.Sprintf("relevance %.0f/%.0f", r.RelevancePassed, r.RelevanceExpected))
		} else {
			for _, criterion := range r.FailedRelevance {
				failures = append(failures, fmt.Sprintf("relevance: %s", criterion))
			}
		}
	}

	return successes, failures
}

// Summary contains aggregate statistics across all evaluations.
type Summary struct {
	TotalEvals      int     `json:"total_evals"`
	FailedEvals     int     `json:"failed_evals"`
	TotalCost       float64 `json:"total_cost"`
	SizesPassed     int     `json:"sizes_passed"`
	SizesTotal      int     `json:"sizes_total"`
	ToolsPassed     float64 `json:"tools_passed"`
	ToolsTotal      float64 `json:"tools_total"`
	HandoffsPassed  int     `json:"handoffs_passed"`
	HandoffsTotal   int     `json:"handoffs_total"`
	RelevancePassed float64 `json:"relevance_passed"`
	RelevanceTotal  float64 `json:"relevance_total"`
}

// EvalRun contains the results and metadata for an evaluation run.
type EvalRun struct {
	Name      string        `json:"name"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
	Results   []Result      `json:"results"`
	Summary   Summary       `json:"summary"`
}

// Config holds configuration for evaluation runs.
type Config struct {
	AgentFilename  string   // Path to the agent configuration file
	EvalsDir       string   // Directory containing evaluation files
	JudgeModel     string   // Model for relevance checking (format: provider/model, optional)
	Concurrency    int      // Number of concurrent runs (0 = number of CPUs)
	TTYFd          int      // File descriptor for terminal size queries (e.g., int(os.Stdout.Fd()))
	Only           []string // Only run evaluations matching these patterns
	BaseImage      string   // Custom base Docker image for running evaluations
	KeepContainers bool     // If true, don't remove containers after evaluation (skip --rm)
}

// Session helper functions

func getFirstUserMessage(sess *session.Session) string {
	for _, msg := range sess.GetAllMessages() {
		if msg.Message.Role == "user" {
			return msg.Message.Content
		}
	}
	return ""
}

func extractToolCalls(items []session.Item) []string {
	var names []string
	for _, item := range items {
		if item.Message != nil {
			for _, tc := range item.Message.Message.ToolCalls {
				names = append(names, tc.Function.Name)
			}
		}
		if item.SubSession != nil {
			names = append(names, extractToolCalls(item.SubSession.Messages)...)
		}
	}
	return names
}
