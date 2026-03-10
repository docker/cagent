package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin"
	agenttool "github.com/docker/docker-agent/pkg/tools/builtin/agent"
)

// agentNames returns the names of the given agents.
func agentNames(agents []*agent.Agent) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name()
	}
	return names
}

// validateAgentInList checks that targetAgent appears in the given agent list.
// Returns a tool error result if not found, or nil if the target is valid.
// The action describes the attempted operation (e.g. "transfer task to"),
// and listDesc is a human-readable description of the list (e.g. "sub-agents list").
func validateAgentInList(currentAgent, targetAgent, action, listDesc string, agents []*agent.Agent) *tools.ToolCallResult {
	if slices.ContainsFunc(agents, func(a *agent.Agent) bool { return a.Name() == targetAgent }) {
		return nil
	}
	if names := agentNames(agents); len(names) > 0 {
		return tools.ResultError(fmt.Sprintf(
			"Agent %s cannot %s %s: target agent not in %s. Available agent IDs are: %s",
			currentAgent, action, targetAgent, listDesc, strings.Join(names, ", "),
		))
	}
	return tools.ResultError(fmt.Sprintf(
		"Agent %s cannot %s %s: target agent not in %s. No agents are configured in this list.",
		currentAgent, action, targetAgent, listDesc,
	))
}

// buildTaskSystemMessage constructs the system message for a delegated task.
func buildTaskSystemMessage(task, expectedOutput string) string {
	msg := "You are a member of a team of agents. Your goal is to complete the following task:"
	msg += fmt.Sprintf("\n\n<task>\n%s\n</task>", task)
	if expectedOutput != "" {
		msg += fmt.Sprintf("\n\n<expected_output>\n%s\n</expected_output>", expectedOutput)
	}
	return msg
}

// CurrentAgentSubAgentNames implements agenttool.Runner.
func (r *LocalRuntime) CurrentAgentSubAgentNames() []string {
	a := r.CurrentAgent()
	if a == nil {
		return nil
	}
	return agentNames(a.SubAgents())
}

// RunAgent implements agenttool.Runner. It starts a sub-agent synchronously and
// blocks until completion or cancellation.
func (r *LocalRuntime) RunAgent(ctx context.Context, params agenttool.RunParams) *agenttool.RunResult {
	child, err := r.team.Agent(params.AgentName)
	if err != nil {
		return &agenttool.RunResult{ErrMsg: fmt.Sprintf("agent %q not found: %s", params.AgentName, err)}
	}

	sess := params.ParentSession

	// Background tasks run with tools pre-approved because there is no user present
	// to respond to interactive approval prompts during async execution. This is a
	// deliberate design trade-off: the user implicitly authorises all tool calls made
	// by the sub-agent when they approve run_background_agent. Callers should be aware
	// that prompt injection in the sub-agent's context could exploit this gate-bypass.
	//
	// TODO: propagate the parent session's per-tool permission rules once the runtime
	// supports per-session permission scoping rather than a single shared ToolsApproved flag.
	s := session.New(
		session.WithSystemMessage(buildTaskSystemMessage(params.Task, params.ExpectedOutput)),
		session.WithImplicitUserMessage("Please proceed."),
		session.WithMaxIterations(child.MaxIterations()),
		session.WithTitle("Background agent task"),
		session.WithToolsApproved(true),
		session.WithThinking(sess.Thinking),
		session.WithSendUserMessage(false),
		session.WithParentID(sess.ID),
		session.WithAgentName(params.AgentName),
	)

	var errMsg string
	events := r.RunStream(ctx, s)
	for event := range events {
		if ctx.Err() != nil {
			break
		}
		if choice, ok := event.(*AgentChoiceEvent); ok && choice.Content != "" {
			if params.OnContent != nil {
				params.OnContent(choice.Content)
			}
		}
		if errEvt, ok := event.(*ErrorEvent); ok {
			errMsg = errEvt.Error
			break
		}
	}
	// Drain remaining events so the RunStream goroutine can complete
	// and close the channel without blocking on a full buffer.
	for range events {
	}

	if errMsg != "" {
		return &agenttool.RunResult{ErrMsg: errMsg}
	}

	result := s.GetLastAssistantMessageContent()
	sess.AddSubSession(s)
	return &agenttool.RunResult{Result: result}
}

func (r *LocalRuntime) handleTaskTransfer(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, evts chan Event) (*tools.ToolCallResult, error) {
	var params struct {
		Agent          string `json:"agent"`
		Task           string `json:"task"`
		ExpectedOutput string `json:"expected_output"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	a := r.CurrentAgent()

	// Validate that the target agent is in the current agent's sub-agents list
	if errResult := validateAgentInList(a.Name(), params.Agent, "transfer task to", "sub-agents list", a.SubAgents()); errResult != nil {
		return errResult, nil
	}

	ctx, span := r.startSpan(ctx, "runtime.task_transfer", trace.WithAttributes(
		attribute.String("from.agent", a.Name()),
		attribute.String("to.agent", params.Agent),
		attribute.String("session.id", sess.ID),
	))
	defer span.End()

	slog.Debug("Transferring task to agent", "from_agent", a.Name(), "to_agent", params.Agent, "task", params.Task)

	ca := r.CurrentAgentName()

	// Emit agent switching start event
	evts <- AgentSwitching(true, ca, params.Agent)

	r.setCurrentAgent(params.Agent)
	defer func() {
		r.setCurrentAgent(ca)

		// Emit agent switching end event
		evts <- AgentSwitching(false, params.Agent, ca)

		// Restore original agent info in sidebar
		if originalAgent, err := r.team.Agent(ca); err == nil {
			evts <- AgentInfo(originalAgent.Name(), r.getEffectiveModelID(originalAgent), originalAgent.Description(), originalAgent.WelcomeMessage())
		}
	}()

	// Emit agent info for the new agent
	if newAgent, err := r.team.Agent(params.Agent); err == nil {
		evts <- AgentInfo(newAgent.Name(), r.getEffectiveModelID(newAgent), newAgent.Description(), newAgent.WelcomeMessage())
	}

	slog.Debug("Creating new session with parent session", "parent_session_id", sess.ID, "tools_approved", sess.ToolsApproved, "thinking", sess.Thinking)

	child, err := r.team.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	s := session.New(
		session.WithSystemMessage(buildTaskSystemMessage(params.Task, params.ExpectedOutput)),
		session.WithImplicitUserMessage("Please proceed."),
		session.WithMaxIterations(child.MaxIterations()),
		session.WithTitle("Transferred task"),
		session.WithToolsApproved(sess.ToolsApproved),
		session.WithThinking(sess.Thinking),
		session.WithSendUserMessage(false),
		session.WithParentID(sess.ID),
	)

	return r.runSubSession(ctx, sess, s, span, evts, a.Name())
}

// runSubSession runs a child session within the parent, forwarding events and
// propagating state (tool approvals, thinking) back to the parent when done.
func (r *LocalRuntime) runSubSession(ctx context.Context, parent, child *session.Session, span trace.Span, evts chan Event, agentName string) (*tools.ToolCallResult, error) {
	for event := range r.RunStream(ctx, child) {
		evts <- event
		if errEvent, ok := event.(*ErrorEvent); ok {
			span.RecordError(fmt.Errorf("%s", errEvent.Error))
			span.SetStatus(codes.Error, "sub-session error")
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	parent.ToolsApproved = child.ToolsApproved
	parent.Thinking = child.Thinking

	parent.AddSubSession(child)
	evts <- SubSessionCompleted(parent.ID, child, agentName)

	span.SetStatus(codes.Ok, "sub-session completed")
	return tools.ResultSuccess(child.GetLastAssistantMessageContent()), nil
}

func (r *LocalRuntime) handleHandoff(_ context.Context, _ *session.Session, toolCall tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
	var params builtin.HandoffArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ca := r.CurrentAgentName()
	currentAgent, err := r.team.Agent(ca)
	if err != nil {
		return nil, fmt.Errorf("current agent not found: %w", err)
	}

	// Validate that the target agent is in the current agent's handoffs list
	if errResult := validateAgentInList(ca, params.Agent, "hand off to", "handoffs list", currentAgent.Handoffs()); errResult != nil {
		return errResult, nil
	}

	next, err := r.team.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	r.setCurrentAgent(next.Name())
	handoffMessage := "The agent " + ca + " handed off the conversation to you. " +
		"Your available handoff agents and tools are specified in the system messages that follow. " +
		"Only use those capabilities - do not attempt to use tools or hand off to agents that you see " +
		"in the conversation history from previous agents, as those were available to different agents " +
		"with different capabilities. Look at the conversation history for context, but only use the " +
		"handoff agents and tools that are listed in your system messages below. " +
		"Complete your part of the task and hand off to the next appropriate agent in your workflow " +
		"(if any are available to you), or respond directly to the user if you are the final agent."
	return tools.ResultSuccess(handoffMessage), nil
}
