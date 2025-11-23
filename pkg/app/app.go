package app

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
)

type App struct {
	agentFilename    string
	runtime          runtime.Runtime
	session          *session.Session
	firstMessage     *string
	events           chan tea.Msg
	throttleDuration time.Duration
	cancel           context.CancelFunc
}

func New(ctx context.Context, agentFilename string, rt runtime.Runtime, sess *session.Session, firstMessage *string) *App {
	app := &App{
		agentFilename:    agentFilename,
		runtime:          rt,
		session:          sess,
		firstMessage:     firstMessage,
		events:           make(chan tea.Msg, 128),
		throttleDuration: 50 * time.Millisecond, // Throttle rapid events
	}

	// If the runtime supports background RAG initialization, start it
	// and forward events to the TUI. Remote runtimes typically handle RAG server-side
	// and won't implement this optional interface.
	if ragRuntime, ok := rt.(runtime.RAGInitializer); ok {
		go ragRuntime.StartBackgroundRAGInit(ctx, func(event runtime.Event) {
			app.events <- event
		})
	}

	return app
}

func (a *App) FirstMessage() *string {
	return a.firstMessage
}

// CurrentWelcomeMessage returns the welcome message for the active agent
func (a *App) CurrentWelcomeMessage(ctx context.Context) string {
	return a.runtime.CurrentWelcomeMessage(ctx)
}

// CurrentAgentCommands returns the commands for the active agent
func (a *App) CurrentAgentCommands(ctx context.Context) map[string]string {
	return a.runtime.CurrentAgentCommands(ctx)
}

// CurrentMCPPrompts returns the available MCP prompts for the active agent
func (a *App) CurrentMCPPrompts(ctx context.Context) map[string]mcptools.PromptInfo {
	if localRuntime, ok := a.runtime.(*runtime.LocalRuntime); ok {
		return localRuntime.CurrentMCPPrompts(ctx)
	}
	return make(map[string]mcptools.PromptInfo)
}

// ExecuteMCPPrompt executes an MCP prompt with provided arguments and returns the content
func (a *App) ExecuteMCPPrompt(ctx context.Context, promptName string, arguments map[string]string) (string, error) {
	localRuntime, ok := a.runtime.(*runtime.LocalRuntime)
	if !ok {
		return "", fmt.Errorf("MCP prompts are only supported with local runtime")
	}

	currentAgent := localRuntime.CurrentAgent()
	if currentAgent == nil {
		return "", fmt.Errorf("no current agent available")
	}

	for _, toolset := range currentAgent.ToolSets() {
		if mcpToolset := runtime.UnwrapMCPToolset(toolset); mcpToolset != nil {
			result, err := mcpToolset.GetPrompt(ctx, promptName, arguments)
			if err == nil {
				// Convert the MCP result to a string format suitable for the editor
				// The result contains Messages which are the prompt content
				if len(result.Messages) == 0 {
					return "No content returned from MCP prompt", nil
				}

				var content string
				for i, message := range result.Messages {
					if i > 0 {
						content += "\n\n"
					}
					if textContent, ok := message.Content.(*mcp.TextContent); ok {
						content += textContent.Text
					} else {
						content += fmt.Sprintf("[Non-text content: %T]", message.Content)
					}
				}
				return content, nil
			}
			// If error is "prompt not found", continue to next toolset
			// Otherwise, return the error
			if err.Error() != "prompt not found" {
				return "", fmt.Errorf("error executing prompt '%s': %w", promptName, err)
			}
		}
	}

	return "", fmt.Errorf("MCP prompt '%s' not found in any active toolset", promptName)
}

// ResolveCommand converts /command to its prompt text
func (a *App) ResolveCommand(ctx context.Context, userInput string) string {
	return runtime.ResolveCommand(ctx, a.runtime, userInput)
}

// EmitStartupInfo emits initial agent, team, and toolset information to the provided channel
func (a *App) EmitStartupInfo(ctx context.Context, events chan runtime.Event) {
	a.runtime.EmitStartupInfo(ctx, events)
}

// Run one agent loop
func (a *App) Run(ctx context.Context, cancel context.CancelFunc, message string) {
	a.cancel = cancel
	go func() {
		a.session.AddMessage(session.UserMessage(a.agentFilename, message))
		for event := range a.runtime.RunStream(ctx, a.session) {
			if ctx.Err() != nil {
				return
			}
			a.events <- event
		}
	}()
}

func (a *App) RunBangCommand(ctx context.Context, command string) {
	out, _ := exec.CommandContext(ctx, "/bin/sh", "-c", command).CombinedOutput()
	a.events <- runtime.ShellOutput("$ " + command + "\n" + string(out))
}

func (a *App) Subscribe(ctx context.Context, program *tea.Program) {
	throttledChan := a.throttleEvents(ctx, a.events)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-throttledChan:
			if !ok {
				return
			}
			program.Send(msg)
		}
	}
}

// Resume resumes the runtime with the given confirmation type
func (a *App) Resume(resumeType runtime.ResumeType) {
	a.runtime.Resume(context.Background(), resumeType)
}

// ResumeElicitation resumes an elicitation request with the given action and content
func (a *App) ResumeElicitation(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
	return a.runtime.ResumeElicitation(ctx, action, content)
}

func (a *App) NewSession() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.session = session.New()
}

func (a *App) Session() *session.Session {
	return a.session
}

func (a *App) CompactSession() {
	if a.session != nil {
		events := make(chan runtime.Event, 100)
		a.runtime.Summarize(context.Background(), a.session, events)
		close(events)
		for event := range events {
			a.events <- event
		}
	}
}

func (a *App) PlainTextTranscript() string {
	return transcript(a.session)
}

// throttleEvents buffers and merges rapid events to prevent UI flooding
func (a *App) throttleEvents(ctx context.Context, in <-chan tea.Msg) <-chan tea.Msg {
	out := make(chan tea.Msg, 128)

	go func() {
		defer close(out)

		var buffer []tea.Msg
		ticker := time.NewTicker(a.throttleDuration)
		defer ticker.Stop()

		flush := func() {
			if len(buffer) == 0 {
				return
			}

			// Merge events if possible
			merged := a.mergeEvents(buffer)
			for _, msg := range merged {
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			}
			buffer = buffer[:0]
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return

			case msg, ok := <-in:
				if !ok {
					flush()
					return
				}

				// Check if this event type should be throttled
				if a.shouldThrottle(msg) {
					buffer = append(buffer, msg)
				} else {
					// Pass through immediately for important events
					flush() // Flush any buffered events first
					select {
					case out <- msg:
					case <-ctx.Done():
						return
					}
				}

			case <-ticker.C:
				flush()
			}
		}
	}()

	return out
}

// shouldThrottle determines if an event should be buffered/throttled
func (a *App) shouldThrottle(msg tea.Msg) bool {
	switch msg.(type) {
	case *runtime.AgentChoiceEvent:
		return true
	case *runtime.AgentChoiceReasoningEvent:
		return true
	case *runtime.PartialToolCallEvent:
		return true
	default:
		return false
	}
}

// mergeEvents merges consecutive similar events to reduce UI updates
func (a *App) mergeEvents(events []tea.Msg) []tea.Msg {
	if len(events) == 0 {
		return events
	}

	var result []tea.Msg

	// Group events by type and merge
	for i := 0; i < len(events); i++ {
		current := events[i]

		switch ev := current.(type) {
		case *runtime.AgentChoiceEvent:
			// Merge consecutive AgentChoiceEvents with same agent
			merged := ev
			for i+1 < len(events) {
				if next, ok := events[i+1].(*runtime.AgentChoiceEvent); ok && next.AgentName == ev.AgentName {
					// Concatenate content
					merged = &runtime.AgentChoiceEvent{
						Type:         ev.Type,
						Content:      merged.Content + next.Content,
						AgentContext: ev.AgentContext,
					}
					i++
				} else {
					break
				}
			}
			result = append(result, merged)

		case *runtime.AgentChoiceReasoningEvent:
			// Merge consecutive AgentChoiceReasoningEvents with same agent
			merged := ev
			for i+1 < len(events) {
				if next, ok := events[i+1].(*runtime.AgentChoiceReasoningEvent); ok && next.AgentName == ev.AgentName {
					// Concatenate content
					merged = &runtime.AgentChoiceReasoningEvent{
						Type:         ev.Type,
						Content:      merged.Content + next.Content,
						AgentContext: ev.AgentContext,
					}
					i++
				} else {
					break
				}
			}
			result = append(result, merged)

		case *runtime.PartialToolCallEvent:
			// For PartialToolCallEvent, keep only the latest one per tool call ID
			// Check if there's a newer one in the buffer
			latest := ev
			for j := i + 1; j < len(events); j++ {
				if next, ok := events[j].(*runtime.PartialToolCallEvent); ok {
					if next.ToolCall.ID == ev.ToolCall.ID {
						latest = next
						i = j // Skip to this position
					}
				}
			}
			result = append(result, latest)

		default:
			// Pass through other events as-is
			result = append(result, current)
		}
	}

	return result
}
