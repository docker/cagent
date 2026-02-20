package workflowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/workflow"
)

// Event is the type of events emitted during workflow execution.
// The executor sends runtime.Event values on the channel.
type Event = any

// Executor runs a workflow: sequential, conditional, and parallel steps.
// It drives the runtime (RunStream per agent step), maintains step outputs,
// evaluates conditions, and enforces max loop iterations.
type Executor interface {
	// Run executes the workflow with the given session (initial user message) and sends events to the channel.
	Run(ctx context.Context, cfg *workflow.Config, sess *session.Session, events chan Event) (*workflow.StepContext, error)
}

// Runner is the minimal runtime interface needed to run agent steps.
// Callers pass a runtime.Runtime (or adapter) that implements Runner.
type Runner interface {
	CurrentAgentName() string
	SetCurrentAgent(agentName string) error
	RunStream(ctx context.Context, sess *session.Session) <-chan runtime.Event
}

// LocalExecutor executes workflows using a LocalRuntime (or any Runner).
type LocalExecutor struct {
	Runner Runner
	// runnerMu serializes SetCurrentAgent + RunStream calls so the Runner's
	// internal goroutine captures the correct agent name before the next
	// parallel step changes it.
	runnerMu sync.Mutex
}

// NewLocalExecutor returns an executor that uses the given Runner.
func NewLocalExecutor(r Runner) *LocalExecutor {
	return &LocalExecutor{Runner: r}
}

// Run executes the workflow. Sequential steps run in order; conditional steps
// evaluate and run true/false branches; parallel steps run concurrently and
// all must succeed before the next sequential step.
func (e *LocalExecutor) Run(ctx context.Context, cfg *workflow.Config, sess *session.Session, events chan Event) (*workflow.StepContext, error) {
	if cfg == nil || len(cfg.Steps) == 0 {
		return nil, fmt.Errorf("workflow: no steps configured")
	}
	maxLoop := cfg.MaxLoopIterations
	if maxLoop <= 0 {
		maxLoop = workflow.DefaultMaxLoopIterations
	}
	ctx = workflow.NewLoopCounter(ctx, maxLoop)
	stepCtx := workflow.NewStepContext()
	err := e.runSteps(ctx, cfg.Steps, &stepCtx, sess, events)

	// Print step context for debugging.
	if b, jerr := json.MarshalIndent(stepCtx.Snapshot(), "", "  "); jerr == nil {
		fmt.Fprintf(os.Stderr, "\n--- Step Context ---\n%s\n", string(b))
	}

	return &stepCtx, err
}

func (e *LocalExecutor) runSteps(ctx context.Context, steps []workflow.Step, stepCtx *workflow.StepContext, sess *session.Session, events chan Event) error {
	for i := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := e.runStep(ctx, &steps[i], stepCtx, sess, events); err != nil {
			return err
		}
	}
	return nil
}

func (e *LocalExecutor) runStep(ctx context.Context, step *workflow.Step, stepCtx *workflow.StepContext, sess *session.Session, events chan Event) error {
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step_%s", step.Type)
	}
	switch step.Type {
	case workflow.StepTypeAgent:
		return e.runAgentStep(ctx, stepID, step, stepCtx, sess, events)
	case workflow.StepTypeCondition:
		return e.runConditionStep(ctx, stepID, step, stepCtx, sess, events)
	case workflow.StepTypeParallel:
		return e.runParallelStep(ctx, stepID, step, stepCtx, sess, events)
	default:
		return fmt.Errorf("workflow: unknown step type %q", step.Type)
	}
}

func (e *LocalExecutor) runAgentStep(ctx context.Context, stepID string, step *workflow.Step, stepCtx *workflow.StepContext, sess *session.Session, events chan Event) error {
	if err := workflow.IncLoopCounter(ctx, stepID); err != nil {
		return err
	}

	runSess := e.buildSessionForStep(step, stepCtx, sess)

	// Protect SetCurrentAgent + RunStream so the Runner's internal goroutine
	// captures the correct agent before another parallel step changes it.
	e.runnerMu.Lock()
	if err := e.Runner.SetCurrentAgent(step.Name); err != nil {
		e.runnerMu.Unlock()
		return fmt.Errorf("workflow: set agent %q: %w", step.Name, err)
	}
	eventsCh := e.Runner.RunStream(ctx, runSess)
	e.runnerMu.Unlock()

	for ev := range eventsCh {
		select {
		case events <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var lastOutput string
	if runSess != nil {
		lastOutput = runSess.GetLastAssistantMessageContent()
	}
	stepCtx.SetAgentOutput(stepID, lastOutput, step.Name)
	return nil
}

func (e *LocalExecutor) buildSessionForStep(step *workflow.Step, stepCtx *workflow.StepContext, initial *session.Session) *session.Session {
	opts := []session.Opt{
		session.WithMaxIterations(initial.MaxIterations),
		session.WithToolsApproved(initial.ToolsApproved),
		session.WithThinking(initial.Thinking),
		session.WithSendUserMessage(true),
	}

	// Build the user message: original user prompt + context from prior steps.
	var userMsg string
	if initial != nil && initial.Messages != nil {
		for _, item := range initial.Messages {
			if item.IsMessage() && item.Message.Message.Role == "user" {
				userMsg = item.Message.Message.Content
				break
			}
		}
	}
	if userMsg == "" {
		userMsg = "Please proceed with the workflow step."
	}

	// Inject prior step outputs as context for the current step.
	if prior := buildPriorContext(stepCtx); prior != "" {
		userMsg = prior + "\n\n" + userMsg
	}

	opts = append(opts, session.WithUserMessage(userMsg))
	return session.New(opts...)
}

// buildPriorContext formats all prior step outputs into a context block
// that is injected into the next step's user message.
func buildPriorContext(stepCtx *workflow.StepContext) string {
	snapshot := stepCtx.Snapshot()
	if len(snapshot) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("--- Prior Step Outputs ---")
	for id, v := range snapshot {
		switch out := v.(type) {
		case workflow.StepOutput:
			if out.Output != "" {
				sb.WriteString("\n\n[")
				sb.WriteString(id)
				sb.WriteString(" (agent: ")
				sb.WriteString(out.Agent)
				sb.WriteString(")]:\n")
				sb.WriteString(out.Output)
			}
		case *workflow.ParallelOutputs:
			if out != nil {
				for _, subID := range out.Order {
					so := out.Steps[subID]
					if so.Output != "" {
						sb.WriteString("\n\n[")
						sb.WriteString(id)
						sb.WriteString("/")
						sb.WriteString(subID)
						sb.WriteString(" (agent: ")
						sb.WriteString(so.Agent)
						sb.WriteString(")]:\n")
						sb.WriteString(so.Output)
					}
				}
			}
		}
	}
	sb.WriteString("\n\n--- End Prior Step Outputs ---")
	return sb.String()
}

func (e *LocalExecutor) runConditionStep(ctx context.Context, stepID string, step *workflow.Step, stepCtx *workflow.StepContext, sess *session.Session, events chan Event) error {
	ok, resolved := stepCtx.EvalCondition(step.Condition)
	if !resolved {
		return fmt.Errorf("workflow: condition did not resolve to boolean: %q", step.Condition)
	}
	if ok {
		return e.runSteps(ctx, step.TrueSteps, stepCtx, sess, events)
	}
	return e.runSteps(ctx, step.FalseSteps, stepCtx, sess, events)
}

func (e *LocalExecutor) runParallelStep(ctx context.Context, stepID string, step *workflow.Step, stepCtx *workflow.StepContext, sess *session.Session, events chan Event) error {
	if len(step.Steps) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	outputs := make(map[string]workflow.StepOutput)
	order := make([]string, 0, len(step.Steps))
	var mu sync.Mutex
	var firstErr error

	for i := range step.Steps {
		child := &step.Steps[i]
		childID := child.ID
		if childID == "" {
			childID = fmt.Sprintf("%s_%d", stepID, i)
		}
		order = append(order, childID)
		stepCopy := *child
		stepCopy.ID = childID
		wg.Add(1)
		go func(s *workflow.Step, id string) {
			defer wg.Done()

			// Each parallel goroutine gets its own sub-session.
			// PersistentRuntime skips all persistence for sub-sessions,
			// avoiding concurrent SQLite writes.
			subSess := e.buildSessionForStep(s, stepCtx, sess)
			subSess.ParentID = sess.ID

			if err := e.runAgentStep(ctx, id, s, stepCtx, subSess, events); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if so, ok := stepCtx.GetOutput(id); ok {
				mu.Lock()
				outputs[id] = so
				mu.Unlock()
			}
		}(&stepCopy, childID)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	stepCtx.SetParallelOutput(stepID, &workflow.ParallelOutputs{Steps: outputs, Order: order})
	return nil
}
