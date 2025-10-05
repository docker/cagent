package app

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

type App struct {
	title            string
	agentFilename    string
	runtime          runtime.Runtime
	team             *team.Team
	session          *session.Session
	firstMessage     *string
	events           chan tea.Msg
	throttleDuration time.Duration
	cancel           context.CancelFunc
}

func New(title, agentFilename string, rt runtime.Runtime, agents *team.Team, sess *session.Session, firstMessage *string) *App {
	return &App{
		title:            title,
		agentFilename:    agentFilename,
		runtime:          rt,
		team:             agents,
		session:          sess,
		firstMessage:     firstMessage,
		events:           make(chan tea.Msg, 128),
		throttleDuration: 50 * time.Millisecond, // Throttle rapid events
	}
}

func (a *App) FirstMessage() *string {
	return a.firstMessage
}

func (a *App) Team() *team.Team {
	return a.team
}

func (a *App) Title() string {
	return a.title
}

// Run one agent loop
func (a *App) Run(ctx context.Context, cancel context.CancelFunc, message string) {
	a.cancel = cancel
	go func() {
		// Special shell command
		if strings.HasPrefix(message, "!") {
			out, _ := exec.CommandContext(ctx, "/bin/sh", "-c", message[1:]).CombinedOutput()
			a.events <- runtime.ShellOutput("$ " + message[1:] + "\n" + string(out))
			return
		}

		// User message
		a.session.AddMessage(session.UserMessage(a.agentFilename, message))
		for event := range a.runtime.RunStream(ctx, a.session) {
			if ctx.Err() != nil {
				return
			}
			a.events <- event
		}
	}()
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
func (a *App) Resume(confirmationType string) {
	if a.runtime != nil {
		a.runtime.Resume(context.Background(), confirmationType)
	}
}

func (a *App) NewSession() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.session = session.New()
}

func (a *App) CompactSession() {
	if a.runtime != nil && a.session != nil {
		events := make(chan runtime.Event, 100)
		a.runtime.Summarize(context.Background(), a.session, events)
		for event := range events {
			a.events <- event
		}
	}
}

// ResumeStartOAuth resumes the runtime with OAuth authorization confirmation
func (a *App) ResumeStartOAuth(confirmation bool) {
	if a.runtime != nil {
		// TODO(rumpl): handle the error
		_ = a.runtime.ResumeElicitation(context.Background(), "accept", nil)
	}
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
