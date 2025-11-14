package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
)

type App struct {
	agentFilename    string
	runtime          runtime.Runtime
	session          *session.Session
	firstMessage     *string
	events           chan tea.Msg
	throttleDuration time.Duration
	cancel           context.CancelFunc
	sessionStore     session.Store
	saveTimer        *time.Timer
	savePending      bool
}

func New(agentFilename string, rt runtime.Runtime, sess *session.Session, firstMessage *string, sessionStore session.Store) *App {
	return &App{
		agentFilename:    agentFilename,
		runtime:          rt,
		session:          sess,
		firstMessage:     firstMessage,
		events:           make(chan tea.Msg, 128),
		throttleDuration: 50 * time.Millisecond, // Throttle rapid events
		sessionStore:     sessionStore,
	}
}

func (a *App) FirstMessage() *string {
	return a.firstMessage
}

// SessionStore returns the session store
func (a *App) SessionStore() session.Store {
	return a.sessionStore
}

// AgentFilename returns the agent filename
func (a *App) AgentFilename() string {
	return a.agentFilename
}

// CurrentWelcomeMessage returns the welcome message for the active agent
func (a *App) CurrentWelcomeMessage(ctx context.Context) string {
	return a.runtime.CurrentWelcomeMessage(ctx)
}

// CurrentAgentCommands returns the commands for the active agent
func (a *App) CurrentAgentCommands(ctx context.Context) map[string]string {
	return a.runtime.CurrentAgentCommands(ctx)
}

// ResolveCommand converts /command to its prompt text
func (a *App) ResolveCommand(ctx context.Context, userInput string) string {
	return runtime.ResolveCommand(ctx, a.runtime, userInput)
}

// Run one agent loop
func (a *App) Run(ctx context.Context, cancel context.CancelFunc, message string) {
	a.cancel = cancel
	go func() {
		a.session.AddMessage(session.UserMessage(a.agentFilename, message))

		// Generate title from first user message if not set
		if a.session.Title == "" && message != "" {
			a.generateSessionTitle(message)
		}

		// Save after user message
		a.scheduleSave(ctx)

		for event := range a.runtime.RunStream(ctx, a.session) {
			if ctx.Err() != nil {
				return
			}
			a.events <- event

			// Save after certain events
			switch event.(type) {
			case *runtime.StreamStoppedEvent, *runtime.ToolCallResponseEvent:
				a.scheduleSave(ctx)
			}
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
	if a.runtime != nil {
		a.runtime.Resume(context.Background(), resumeType)
	}
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

// LoadSession loads a session from the store by ID and replaces the current session
func (a *App) LoadSession(ctx context.Context, sessionID string) error {
	if a.sessionStore == nil {
		return fmt.Errorf("no session store available")
	}

	// Retrieve the session from store
	loadedSession, err := a.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Cancel any running context
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}

	// Replace the current session
	a.session = loadedSession

	return nil
}

// SessionExists checks if the current session exists in the store
func (a *App) SessionExists(ctx context.Context) bool {
	if a.sessionStore == nil || a.session == nil {
		return false
	}

	_, err := a.sessionStore.GetSession(ctx, a.session.ID)
	return err == nil
}

func (a *App) CompactSession() {
	if a.runtime != nil && a.session != nil {
		events := make(chan runtime.Event, 100)
		a.runtime.Summarize(context.Background(), a.session, events)
		close(events)
		for event := range events {
			a.events <- event
		}
	}
}

// ResumeStartOAuth resumes the runtime with OAuth authorization confirmation
func (a *App) ResumeStartOAuth(bool) {
	if a.runtime != nil {
		// TODO(rumpl): handle the error
		_ = a.runtime.ResumeElicitation(context.Background(), "accept", nil)
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

// generateSessionTitle creates a title from the first user message
func (a *App) generateSessionTitle(message string) {
	// Take first line or first 50 characters
	title := message
	if idx := strings.Index(title, "\n"); idx > 0 && idx < 50 {
		title = title[:idx]
	} else if len(title) > 50 {
		title = title[:47] + "..."
	}
	a.session.Title = title
}

// scheduleSave schedules a session save with debouncing
func (a *App) scheduleSave(ctx context.Context) {
	if a.sessionStore == nil {
		return
	}

	// Mark that a save is pending
	a.savePending = true

	// Cancel existing timer if any
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}

	// Schedule new save after 2 seconds
	a.saveTimer = time.AfterFunc(2*time.Second, func() {
		if a.savePending {
			a.saveSession(ctx)
			a.savePending = false
		}
	})
}

// saveSession saves or updates the session in the store
func (a *App) saveSession(ctx context.Context) {
	if a.sessionStore == nil || a.session == nil {
		return
	}

	// Check if session exists
	_, err := a.sessionStore.GetSession(ctx, a.session.ID)
	switch err {
	case session.ErrNotFound:
		// Session doesn't exist, add it
		if err := a.sessionStore.AddSession(ctx, a.session); err != nil {
			// Log error but don't interrupt user experience
			fmt.Fprintf(os.Stderr, "failed to add session: %v\n", err)
		}
	case nil:
		// Session exists, update it
		if err := a.sessionStore.UpdateSession(ctx, a.session); err != nil {
			fmt.Fprintf(os.Stderr, "failed to update session: %v\n", err)
		}
	}
}
