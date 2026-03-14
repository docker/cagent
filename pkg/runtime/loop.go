package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/compaction"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/modelerrors"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/telemetry"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin"
)

// registerDefaultTools wires up the built-in tool handlers (delegation,
// background agents, model switching) into the runtime's tool dispatch map.
func (r *LocalRuntime) registerDefaultTools() {
	r.toolMap[builtin.ToolNameTransferTask] = r.handleTaskTransfer
	r.toolMap[builtin.ToolNameHandoff] = r.handleHandoff
	r.toolMap[builtin.ToolNameChangeModel] = r.handleChangeModel
	r.toolMap[builtin.ToolNameRevertModel] = r.handleRevertModel

	r.bgAgents.RegisterHandlers(func(name string, fn func(context.Context, *session.Session, tools.ToolCall) (*tools.ToolCallResult, error)) {
		r.toolMap[name] = func(ctx context.Context, sess *session.Session, tc tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
			return fn(ctx, sess, tc)
		}
	})
}

// finalizeEventChannel performs cleanup at the end of a RunStream goroutine:
// restores the previous elicitation channel, emits the StreamStopped event,
// fires hooks, and closes the events channel.
func (r *LocalRuntime) finalizeEventChannel(ctx context.Context, sess *session.Session, prevElicitationCh, events chan Event) {
	// Swap back the parent's elicitation channel before closing this
	// stream's channel. This prevents a send-on-closed-channel panic
	// and restores elicitation for the parent session.
	r.swapElicitationEventsChannel(prevElicitationCh)

	defer close(events)

	events <- StreamStopped(sess.ID, r.resolveSessionAgent(sess).Name())

	r.executeOnUserInputHooks(ctx, sess.ID, "stream stopped")

	telemetry.RecordSessionEnd(ctx)
}

// RunStream starts the agent's interaction loop and returns a channel of events.
// The returned channel is closed when the loop terminates (success, error, or
// context cancellation). Each iteration: sends messages to the model, streams
// the response, executes any tool calls, and loops until the model signals stop
// or the iteration limit is reached.
func (r *LocalRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	slog.Debug("Starting runtime stream", "agent", r.CurrentAgentName(), "session_id", sess.ID)
	events := make(chan Event, 128)

	go func() {
		telemetry.RecordSessionStart(ctx, r.CurrentAgentName(), sess.ID)

		ctx, sessionSpan := r.startSpan(ctx, "runtime.session", trace.WithAttributes(
			attribute.String("agent", r.CurrentAgentName()),
			attribute.String("session.id", sess.ID),
		))
		defer sessionSpan.End()

		// Swap in this stream's events channel for elicitation and save the
		// previous one so it can be restored on teardown. This allows nested
		// RunStream calls to temporarily own elicitation without losing the
		// parent's channel.
		prevElicitationCh := r.swapElicitationEventsChannel(events)

		a := r.resolveSessionAgent(sess)

		// lastEmittedModelID tracks what the TUI currently displays.
		// emitModelInfo sends an AgentInfo only when the model actually changed,
		// so new features (routing, alloy, fallback, model picker, …) never need
		// to notify the TUI themselves — the loop handles it.
		lastEmittedModelID := r.getEffectiveModelID(a)
		emitModelInfo := func(a *agent.Agent, modelID string) {
			if modelID == lastEmittedModelID {
				return
			}
			lastEmittedModelID = modelID
			events <- AgentInfo(a.Name(), modelID, a.Description(), a.WelcomeMessage())
		}

		// Emit agent information for sidebar display
		events <- AgentInfo(a.Name(), lastEmittedModelID, a.Description(), a.WelcomeMessage())

		// Emit team information
		events <- TeamInfo(r.agentDetailsFromTeam(), a.Name())

		// Initialize RAG and forward events
		r.InitializeRAG(ctx, events)

		r.emitAgentWarnings(a, chanSend(events))
		r.configureToolsetHandlers(a, events)

		agentTools, err := r.getTools(ctx, a, sessionSpan, events)
		if err != nil {
			events <- Error(fmt.Sprintf("failed to get tools: %v", err))
			return
		}

		events <- ToolsetInfo(len(agentTools), false, a.Name())

		messages := sess.GetMessages(a)
		if sess.SendUserMessage {
			lastMsg := messages[len(messages)-1]
			events <- UserMessage(lastMsg.Content, sess.ID, lastMsg.MultiContent, len(sess.Messages)-1)
		}

		events <- StreamStarted(sess.ID, a.Name())

		defer r.finalizeEventChannel(ctx, sess, prevElicitationCh, events)

		r.registerDefaultTools()

		iteration := 0
		// Use a runtime copy of maxIterations so we don't modify the session's persistent config
		runtimeMaxIterations := sess.MaxIterations

		// toolModelOverride holds the per-toolset model from the most recent
		// tool calls. It applies for one LLM turn, then resets.
		var toolModelOverride string
		var prevAgentName string

		for {
			a = r.resolveSessionAgent(sess)

			// Clear per-tool model override on agent switch so it doesn't
			// leak from one agent's toolset into another agent's turn.
			if a.Name() != prevAgentName {
				toolModelOverride = ""
				prevAgentName = a.Name()
			}

			r.emitAgentWarnings(a, chanSend(events))
			r.configureToolsetHandlers(a, events)

			agentTools, err := r.getTools(ctx, a, sessionSpan, events)
			if err != nil {
				events <- Error(fmt.Sprintf("failed to get tools: %v", err))
				return
			}

			// Emit updated tool count. After a ToolListChanged MCP notification
			// the cache is invalidated, so getTools above re-fetches from the
			// server and may return a different count.
			events <- ToolsetInfo(len(agentTools), false, a.Name())

			// Check iteration limit
			if runtimeMaxIterations > 0 && iteration >= runtimeMaxIterations {
				slog.Debug(
					"Maximum iterations reached",
					"agent", a.Name(),
					"iterations", iteration,
					"max", runtimeMaxIterations,
				)

				events <- MaxIterationsReached(runtimeMaxIterations)

				// Wait for user decision (resume / reject)
				select {
				case req := <-r.resumeChan:
					if req.Type == ResumeTypeApprove {
						slog.Debug("User chose to continue after max iterations", "agent", a.Name())
						runtimeMaxIterations = iteration + 10
					} else {
						slog.Debug("User rejected continuation", "agent", a.Name())

						assistantMessage := chat.Message{
							Role: chat.MessageRoleAssistant,
							Content: fmt.Sprintf(
								"Execution stopped after reaching the configured max_iterations limit (%d).",
								runtimeMaxIterations,
							),
							CreatedAt: time.Now().Format(time.RFC3339),
						}

						addAgentMessage(sess, a, &assistantMessage, events)
						return
					}

				case <-ctx.Done():
					slog.Debug(
						"Context cancelled while waiting for resume confirmation",
						"agent", a.Name(),
						"session_id", sess.ID,
					)
					return
				}
			}

			iteration++

			// Exit immediately if the stream context has been cancelled (e.g., Ctrl+C)
			if err := ctx.Err(); err != nil {
				slog.Debug("Runtime stream context cancelled, stopping loop", "agent", a.Name(), "session_id", sess.ID)
				return
			}
			slog.Debug("Starting conversation loop iteration", "agent", a.Name())

			streamCtx, streamSpan := r.startSpan(ctx, "runtime.stream", trace.WithAttributes(
				attribute.String("agent", a.Name()),
				attribute.String("session.id", sess.ID),
			))

			model := a.Model()
			defaultModelID := r.getEffectiveModelID(a)

			// Per-tool model routing: use a cheaper model for this turn
			// if the previous tool calls specified one, then reset.
			if toolModelOverride != "" {
				if overrideModel, err := r.resolveModelRef(ctx, toolModelOverride); err != nil {
					slog.Warn("Failed to resolve per-tool model override; using agent default",
						"model_override", toolModelOverride, "error", err)
				} else {
					slog.Info("Using per-tool model override for this turn",
						"agent", a.Name(), "override", overrideModel.ID(), "primary", model.ID())
					model = overrideModel
				}
				toolModelOverride = ""
			}

			// Apply thinking setting based on session state.
			// When thinking is disabled: clone with thinking=false to clear any thinking config.
			// When thinking is enabled: clone with thinking=true to ensure defaults are applied
			// (this handles models with no thinking config, explicitly disabled thinking, or
			// models that already have thinking configured).
			model = provider.CloneWithOptions(ctx, model, options.WithThinking(sess.Thinking))
			slog.Debug("Cloned provider with thinking setting", "agent", a.Name(), "model", model.ID(), "thinking", sess.Thinking)

			modelID := model.ID()

			// Notify sidebar when this turn uses a different model
			// (per-tool override, model picker, fallback cooldown, …).
			emitModelInfo(a, modelID)

			slog.Debug("Using agent", "agent", a.Name(), "model", modelID)
			slog.Debug("Getting model definition", "model_id", modelID)
			m, err := r.modelsStore.GetModel(ctx, modelID)
			if err != nil {
				slog.Debug("Failed to get model definition", "error", err)
			}

			var contextLimit int64
			if m != nil {
				contextLimit = int64(m.Limit.Context)
			}

			if m != nil && r.sessionCompaction && compaction.ShouldCompact(sess.InputTokens, sess.OutputTokens, 0, contextLimit) {
				r.Summarize(ctx, sess, "", events)
			}

			messages := sess.GetMessages(a)
			slog.Debug("Retrieved messages for processing", "agent", a.Name(), "message_count", len(messages))

			// Strip image content from messages if the model doesn't support image input.
			// This prevents API errors when conversation history contains images (e.g. from
			// tool results or user attachments) but the current model is text-only.
			if m != nil && len(m.Modalities.Input) > 0 && !slices.Contains(m.Modalities.Input, "image") {
				messages = stripImageContent(messages)
			}

			// Try primary model with fallback chain if configured
			res, usedModel, err := r.tryModelWithFallback(streamCtx, a, model, messages, agentTools, sess, m, events)
			if err != nil {
				// Treat context cancellation as a graceful stop
				if errors.Is(err, context.Canceled) {
					slog.Debug("Model stream canceled by context", "agent", a.Name(), "session_id", sess.ID)
					streamSpan.End()
					return
				}

				// Auto-recovery: if the error is a context overflow and
				// session compaction is enabled, compact the conversation
				// and retry the request instead of surfacing raw errors.
				if _, ok := errors.AsType[*modelerrors.ContextOverflowError](err); ok && r.sessionCompaction {
					slog.Warn("Context window overflow detected, attempting auto-compaction",
						"agent", a.Name(),
						"session_id", sess.ID,
						"input_tokens", sess.InputTokens,
						"output_tokens", sess.OutputTokens,
						"context_limit", contextLimit,
					)
					events <- Warning(
						"The conversation has exceeded the model's context window. Automatically compacting the conversation history...",
						a.Name(),
					)
					r.Summarize(ctx, sess, "", events)

					// After compaction, loop back to retry with the
					// compacted context. The next iteration will re-fetch
					// messages from the (now compacted) session.
					streamSpan.End()
					continue
				}

				streamSpan.RecordError(err)
				streamSpan.SetStatus(codes.Error, "error handling stream")
				slog.Error("All models failed", "agent", a.Name(), "error", err)
				// Track error in telemetry
				telemetry.RecordError(ctx, err.Error())
				events <- Error(modelerrors.FormatError(err))
				streamSpan.End()
				return
			}

			// Update sidebar to reflect the model actually used this turn.
			// When no fallback kicked in, revert to the agent's default
			// (undoes any temporary per-tool override).
			actualModelID := defaultModelID
			if usedModel != nil && usedModel.ID() != model.ID() {
				slog.Info("Used fallback model", "agent", a.Name(), "primary", model.ID(), "used", usedModel.ID())
				actualModelID = usedModel.ID()
			}
			emitModelInfo(a, actualModelID)
			streamSpan.SetAttributes(
				attribute.Int("tool.calls", len(res.Calls)),
				attribute.Int("content.length", len(res.Content)),
				attribute.Bool("stopped", res.Stopped),
			)
			streamSpan.End()
			slog.Debug("Stream processed", "agent", a.Name(), "tool_calls", len(res.Calls), "content_length", len(res.Content), "stopped", res.Stopped)

			msgUsage := r.recordAssistantMessage(sess, a, res, agentTools, modelID, m, events)

			usage := SessionUsage(sess, contextLimit)
			usage.LastMessage = msgUsage
			events <- NewTokenUsageEvent(sess.ID, a.Name(), usage)

			// Record the message count before tool calls so we can
			// measure how much content was added by tool results.
			messageCountBeforeTools := len(sess.GetAllMessages())

			r.processToolCalls(ctx, sess, res.Calls, agentTools, events)

			// Tool handlers (e.g. change_model, revert_model) may have
			// switched the effective model. Notify the TUI now so the
			// sidebar updates even when the model stops after the tool call.
			emitModelInfo(a, r.getEffectiveModelID(a))

			// Record per-toolset model override for the next LLM turn.
			toolModelOverride = resolveToolCallModelOverride(res.Calls, agentTools)

			if res.Stopped {
				slog.Debug("Conversation stopped", "agent", a.Name())
				break
			}

			r.compactIfNeeded(ctx, sess, a, m, contextLimit, messageCountBeforeTools, events)
		}
	}()

	return events
}

// Run executes the agent loop synchronously and returns the final session
// messages. This is a convenience wrapper around RunStream for non-streaming
// callers.
func (r *LocalRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	events := r.RunStream(ctx, sess)
	for event := range events {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}
	return sess.GetAllMessages(), nil
}

// recordAssistantMessage adds the model's response to the session and returns
// per-message usage information for the token-usage event. Empty responses
// (no text and no tool calls) are silently skipped since providers reject them.
func (r *LocalRuntime) recordAssistantMessage(
	sess *session.Session,
	a *agent.Agent,
	res streamResult,
	agentTools []tools.Tool,
	modelID string,
	m *modelsdev.Model,
	events chan Event,
) *MessageUsage {
	if strings.TrimSpace(res.Content) == "" && len(res.Calls) == 0 {
		slog.Debug("Skipping empty assistant message (no content and no tool calls)", "agent", a.Name())
		return nil
	}

	// Resolve tool definitions for the tool calls.
	var toolDefs []tools.Tool
	if len(res.Calls) > 0 {
		toolMap := make(map[string]tools.Tool, len(agentTools))
		for _, t := range agentTools {
			toolMap[t.Name] = t
		}
		for _, call := range res.Calls {
			if def, ok := toolMap[call.Function.Name]; ok {
				toolDefs = append(toolDefs, def)
			}
		}
	}

	// Calculate per-message cost when pricing information is available.
	var messageCost float64
	if res.Usage != nil && m != nil && m.Cost != nil {
		messageCost = (float64(res.Usage.InputTokens)*m.Cost.Input +
			float64(res.Usage.OutputTokens)*m.Cost.Output +
			float64(res.Usage.CachedInputTokens)*m.Cost.CacheRead +
			float64(res.Usage.CacheWriteTokens)*m.Cost.CacheWrite) / 1e6
	}

	messageModel := cmp.Or(res.ActualModel, modelID)

	assistantMessage := chat.Message{
		Role:              chat.MessageRoleAssistant,
		Content:           res.Content,
		ReasoningContent:  res.ReasoningContent,
		ThinkingSignature: res.ThinkingSignature,
		ThoughtSignature:  res.ThoughtSignature,
		ToolCalls:         res.Calls,
		ToolDefinitions:   toolDefs,
		CreatedAt:         time.Now().Format(time.RFC3339),
		Usage:             res.Usage,
		Model:             messageModel,
		Cost:              messageCost,
	}

	addAgentMessage(sess, a, &assistantMessage, events)
	slog.Debug("Added assistant message to session", "agent", a.Name(), "total_messages", len(sess.GetAllMessages()))

	// Build per-message usage for the event.
	if res.Usage == nil {
		return nil
	}
	msgUsage := &MessageUsage{
		Usage: *res.Usage,
		Cost:  messageCost,
		Model: messageModel,
	}
	if res.RateLimit != nil {
		msgUsage.RateLimit = *res.RateLimit
	}
	return msgUsage
}

// compactIfNeeded estimates the token impact of tool results added since
// messageCountBefore and triggers proactive compaction when the estimated
// total exceeds 90% of the context window. This prevents sending an
// oversized request on the next iteration.
func (r *LocalRuntime) compactIfNeeded(
	ctx context.Context,
	sess *session.Session,
	a *agent.Agent,
	m *modelsdev.Model,
	contextLimit int64,
	messageCountBefore int,
	events chan Event,
) {
	if m == nil || !r.sessionCompaction || contextLimit <= 0 {
		return
	}

	newMessages := sess.GetAllMessages()[messageCountBefore:]
	var addedTokens int64
	for _, msg := range newMessages {
		addedTokens += compaction.EstimateMessageTokens(&msg.Message)
	}

	if !compaction.ShouldCompact(sess.InputTokens, sess.OutputTokens, addedTokens, contextLimit) {
		return
	}

	slog.Info("Proactive compaction: tool results pushed estimated context past 90%% threshold",
		"agent", a.Name(),
		"input_tokens", sess.InputTokens,
		"output_tokens", sess.OutputTokens,
		"added_estimated_tokens", addedTokens,
		"estimated_total", sess.InputTokens+sess.OutputTokens+addedTokens,
		"context_limit", contextLimit,
	)
	r.Summarize(ctx, sess, "", events)
}

// getTools executes tool retrieval with automatic OAuth handling
func (r *LocalRuntime) getTools(ctx context.Context, a *agent.Agent, sessionSpan trace.Span, events chan Event) ([]tools.Tool, error) {
	shouldEmitMCPInit := len(a.ToolSets()) > 0
	if shouldEmitMCPInit {
		events <- MCPInitStarted(a.Name())
	}
	defer func() {
		if shouldEmitMCPInit {
			events <- MCPInitFinished(a.Name())
		}
	}()

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.Error("Failed to get agent tools", "agent", a.Name(), "error", err)
		sessionSpan.RecordError(err)
		sessionSpan.SetStatus(codes.Error, "failed to get tools")
		telemetry.RecordError(ctx, err.Error())
		return nil, err
	}

	slog.Debug("Retrieved agent tools", "agent", a.Name(), "tool_count", len(agentTools))
	return agentTools, nil
}

// configureToolsetHandlers sets up elicitation and OAuth handlers for all toolsets of an agent.
func (r *LocalRuntime) configureToolsetHandlers(a *agent.Agent, events chan Event) {
	for _, toolset := range a.ToolSets() {
		tools.ConfigureHandlers(toolset,
			r.elicitationHandler,
			func() { events <- Authorization(tools.ElicitationActionAccept, a.Name()) },
			r.managedOAuth,
		)
	}
}

// emitAgentWarnings drains and emits any agent initialization warnings.
func (r *LocalRuntime) emitAgentWarnings(a *agent.Agent, send func(Event)) {
	warnings := a.DrainWarnings()
	if len(warnings) == 0 {
		return
	}

	slog.Warn("Tool setup partially failed; continuing", "agent", a.Name(), "warnings", warnings)
	send(Warning(formatToolWarning(a, warnings), a.Name()))
}

func formatToolWarning(a *agent.Agent, warnings []string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Some toolsets failed to initialize for agent '%s'.\n\nDetails:\n\n", a.Name())
	for _, warning := range warnings {
		fmt.Fprintf(&builder, "- %s\n", warning)
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

// chanSend wraps a channel as a func(Event) for use with emitAgentWarnings.
func chanSend(ch chan Event) func(Event) {
	return func(e Event) { ch <- e }
}
