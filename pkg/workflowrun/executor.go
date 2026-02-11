package workflowrun

import (
	"context"
	"fmt"
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
	Run(ctx context.Context, cfg *workflow.Config, sess *session.Session, events chan Event) error
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
}

// NewLocalExecutor returns an executor that uses the given Runner.
func NewLocalExecutor(r Runner) *LocalExecutor {
	return &LocalExecutor{Runner: r}
}

// Run executes the workflow. Sequential steps run in order; conditional steps
// evaluate and run true/false branches; parallel steps run concurrently and
// all must succeed before the next sequential step.
func (e *LocalExecutor) Run(ctx context.Context, cfg *workflow.Config, sess *session.Session, events chan Event) error {
	if cfg == nil || len(cfg.Steps) == 0 {
		return fmt.Errorf("workflow: no steps configured")
	}
	maxLoop := cfg.MaxLoopIterations
	if maxLoop <= 0 {
		maxLoop = workflow.DefaultMaxLoopIterations
	}
	ctx = workflow.NewLoopCounter(ctx, maxLoop)
	stepCtx := workflow.NewStepContext()
	return e.runSteps(ctx, cfg.Steps, &stepCtx, sess, events)
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
	if err := e.Runner.SetCurrentAgent(step.Name); err != nil {
		return fmt.Errorf("workflow: set agent %q: %w", step.Name, err)
	}
	runSess := e.buildSessionForStep(step, stepCtx, sess)
	for ev := range e.Runner.RunStream(ctx, runSess) {
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
	var hasUserMessage bool
	if initial != nil && initial.Messages != nil {
		for _, item := range initial.Messages {
			if item.IsMessage() && item.Message.Message.Role == "user" {
				opts = append(opts, session.WithUserMessage(item.Message.Message.Content))
				hasUserMessage = true
				break
			}
		}
	}
	if !hasUserMessage {
		opts = append(opts, session.WithUserMessage("Please proceed with the workflow step."))
	}
	return session.New(opts...)
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
			subEvents := make(chan Event, 128)
			err := e.runStep(ctx, s, stepCtx, sess, subEvents)
			if err != nil {
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
			close(subEvents)
			for ev := range subEvents {
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
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
