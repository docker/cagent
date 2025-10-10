package workflow

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

// Executor manages the execution of sequential workflows
type Executor struct {
	config *v2.Config
	team   *team.Team
}

// New creates a new workflow executor
func New(config *v2.Config, agents *team.Team) *Executor {
	return &Executor{
		config: config,
		team:   agents,
	}
}

// Execute runs the workflow sequentially, passing output from one agent to the next
func (e *Executor) Execute(ctx context.Context, events chan<- runtime.Event) error {
	if len(e.config.Workflow) == 0 {
		return fmt.Errorf("no workflow steps defined")
	}

	slog.Info("Starting workflow execution", "steps", len(e.config.Workflow))

	var previousOutput string

	for i, step := range e.config.Workflow {
		if step.Type != "agent" {
			return fmt.Errorf("step %d: unsupported workflow step type '%s'", i, step.Type)
		}

		agent := e.team.Agent(step.Name)
		if agent == nil {
			return fmt.Errorf("step %d: agent '%s' not found", i, step.Name)
		}

		slog.Info("Executing workflow step", "step", i+1, "agent", step.Name)
		events <- runtime.WorkflowStepStarted(i, step.Name)

		// Create a new session for this agent
		var sess *session.Session
		if i == 0 {
			// First agent - use its instruction as system message
			sess = session.New(
				session.WithSystemMessage(agent.Instruction()),
				session.WithImplicitUserMessage("", "Generate the initial data as specified in your instructions."),
			)
		} else {
			// Subsequent agents - pass previous output as user message
			userPrompt := fmt.Sprintf("Process the following input according to your instructions:\n\n%s", previousOutput)
			sess = session.New(
				session.WithSystemMessage(agent.Instruction()),
				session.WithImplicitUserMessage("", userPrompt),
			)
		}
		sess.SendUserMessage = false

		// Create runtime for this agent
		rt, err := runtime.New(e.team,
			runtime.WithCurrentAgent(step.Name),
			runtime.WithSessionCompaction(false),
		)
		if err != nil {
			events <- runtime.WorkflowStepFailed(i, step.Name, err.Error())
			return fmt.Errorf("step %d: failed to create runtime: %w", i, err)
		}

		// Run the agent and collect events
		for event := range rt.RunStream(ctx, sess) {
			// Forward events to the caller
			events <- event

			// Check for errors
			if errEvent, ok := event.(*runtime.ErrorEvent); ok {
				events <- runtime.WorkflowStepFailed(i, step.Name, errEvent.Error)
				return fmt.Errorf("step %d: agent '%s' failed: %s", i, step.Name, errEvent.Error)
			}
		}

		// Get the output from the last assistant message
		output := sess.GetLastAssistantMessageContent()
		if output == "" {
			err := fmt.Errorf("step %d: agent '%s' produced no output", i, step.Name)
			events <- runtime.WorkflowStepFailed(i, step.Name, err.Error())
			return err
		}

		// Pass output to next step
		previousOutput = output
		events <- runtime.WorkflowStepCompleted(i, step.Name, output)
		slog.Info("Workflow step completed", "step", i+1, "agent", step.Name)
	}

	events <- runtime.WorkflowCompleted(previousOutput)
	slog.Info("Workflow execution completed successfully")
	return nil
}
