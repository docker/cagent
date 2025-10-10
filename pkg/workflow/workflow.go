package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

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

// Execute runs the workflow, supporting both sequential and parallel execution
func (e *Executor) Execute(ctx context.Context, events chan<- runtime.Event) error {
	if len(e.config.Workflow) == 0 {
		return fmt.Errorf("no workflow steps defined")
	}

	slog.Info("Starting workflow execution", "steps", len(e.config.Workflow))

	var previousOutput string

	for i, step := range e.config.Workflow {
		switch step.Type {
		case "agent":
			output, err := e.executeAgent(ctx, events, i, step.Name, previousOutput, i == 0)
			if err != nil {
				return err
			}
			previousOutput = output

		case "parallel":
			output, err := e.executeParallel(ctx, events, i, step.Steps, previousOutput)
			if err != nil {
				return err
			}
			previousOutput = output

		default:
			return fmt.Errorf("step %d: unsupported workflow step type '%s'", i, step.Type)
		}
	}

	events <- runtime.WorkflowCompleted(previousOutput)
	slog.Info("Workflow execution completed successfully")
	return nil
}

// executeAgent runs a single agent
func (e *Executor) executeAgent(ctx context.Context, events chan<- runtime.Event, stepIndex int, agentName, input string, isFirst bool) (string, error) {
	agent := e.team.Agent(agentName)
	if agent == nil {
		return "", fmt.Errorf("step %d: agent '%s' not found", stepIndex, agentName)
	}

	slog.Info("Executing workflow step", "step", stepIndex+1, "agent", agentName)
	events <- runtime.WorkflowStepStarted(stepIndex, agentName)

	// Create a new session for this agent
	var sess *session.Session
	if isFirst {
		// First agent - use its instruction as system message
		sess = session.New(
			session.WithSystemMessage(agent.Instruction()),
			session.WithImplicitUserMessage("", "Generate the initial data as specified in your instructions."),
		)
	} else {
		// Subsequent agents - pass previous output as user message
		userPrompt := fmt.Sprintf("Process the following input according to your instructions:\n\n%s", input)
		sess = session.New(
			session.WithSystemMessage(agent.Instruction()),
			session.WithImplicitUserMessage("", userPrompt),
		)
	}
	sess.SendUserMessage = false

	// Create runtime for this agent
	rt, err := runtime.New(e.team,
		runtime.WithCurrentAgent(agentName),
		runtime.WithSessionCompaction(false),
	)
	if err != nil {
		events <- runtime.WorkflowStepFailed(stepIndex, agentName, err.Error())
		return "", fmt.Errorf("step %d: failed to create runtime: %w", stepIndex, err)
	}

	// Run the agent and collect events
	for event := range rt.RunStream(ctx, sess) {
		// Forward events to the caller
		events <- event

		// Check for errors
		if errEvent, ok := event.(*runtime.ErrorEvent); ok {
			events <- runtime.WorkflowStepFailed(stepIndex, agentName, errEvent.Error)
			return "", fmt.Errorf("step %d: agent '%s' failed: %s", stepIndex, agentName, errEvent.Error)
		}
	}

	// Get the output from the last assistant message
	output := sess.GetLastAssistantMessageContent()
	if output == "" {
		err := fmt.Errorf("step %d: agent '%s' produced no output", stepIndex, agentName)
		events <- runtime.WorkflowStepFailed(stepIndex, agentName, err.Error())
		return "", err
	}

	events <- runtime.WorkflowStepCompleted(stepIndex, agentName, output)
	slog.Info("Workflow step completed", "step", stepIndex+1, "agent", agentName)
	return output, nil
}

// executeParallel runs multiple agents in parallel and combines their outputs
func (e *Executor) executeParallel(ctx context.Context, events chan<- runtime.Event, stepIndex int, agentNames []string, input string) (string, error) {
	if len(agentNames) == 0 {
		return "", fmt.Errorf("step %d: no agents specified for parallel execution", stepIndex)
	}

	slog.Info("Executing parallel workflow step", "step", stepIndex+1, "agents", agentNames)
	events <- runtime.WorkflowParallelStarted(stepIndex, agentNames)

	// Create channels for collecting results
	type result struct {
		agentName string
		output    string
		err       error
	}
	results := make(chan result, len(agentNames))

	// Create a wait group to track all parallel executions
	var wg sync.WaitGroup

	// Launch all agents in parallel
	for _, agentName := range agentNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			agent := e.team.Agent(name)
			if agent == nil {
				results <- result{agentName: name, err: fmt.Errorf("agent '%s' not found", name)}
				return
			}

			slog.Info("Starting parallel agent", "agent", name)

			// Create a new session for this agent
			userPrompt := fmt.Sprintf("Process the following input according to your instructions:\n\n%s", input)
			sess := session.New(
				session.WithSystemMessage(agent.Instruction()),
				session.WithImplicitUserMessage("", userPrompt),
			)
			sess.SendUserMessage = false

			// Create runtime for this agent
			rt, err := runtime.New(e.team,
				runtime.WithCurrentAgent(name),
				runtime.WithSessionCompaction(false),
			)
			if err != nil {
				results <- result{agentName: name, err: fmt.Errorf("failed to create runtime: %w", err)}
				return
			}

			// Run the agent and collect events
			for event := range rt.RunStream(ctx, sess) {
				// Forward events to the caller
				events <- event

				// Check for errors
				if errEvent, ok := event.(*runtime.ErrorEvent); ok {
					results <- result{agentName: name, err: fmt.Errorf("agent failed: %s", errEvent.Error)}
					return
				}
			}

			// Get the output from the last assistant message
			output := sess.GetLastAssistantMessageContent()
			if output == "" {
				results <- result{agentName: name, err: fmt.Errorf("agent produced no output")}
				return
			}

			slog.Info("Parallel agent completed", "agent", name)
			results <- result{agentName: name, output: output}
		}(agentName)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results maintaining order
	outputs := make(map[string]string)
	var errors []string

	for res := range results {
		if res.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", res.agentName, res.err))
			events <- runtime.WorkflowStepFailed(stepIndex, res.agentName, res.err.Error())
		} else {
			outputs[res.agentName] = res.output
			events <- runtime.WorkflowStepCompleted(stepIndex, res.agentName, res.output)
		}
	}

	// Check if any agents failed
	if len(errors) > 0 {
		return "", fmt.Errorf("step %d: parallel execution failed: %v", stepIndex, errors)
	}

	// Combine outputs in the order they were specified
	var combinedOutput string
	for _, agentName := range agentNames {
		if output, ok := outputs[agentName]; ok {
			combinedOutput += output + "\n\n"
		}
	}

	events <- runtime.WorkflowParallelCompleted(stepIndex, agentNames, combinedOutput)
	slog.Info("Parallel workflow step completed", "step", stepIndex+1)
	return combinedOutput, nil
}
