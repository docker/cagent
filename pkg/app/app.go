package app

import (
	"context"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

type App struct {
	title         string
	agentFilename string
	runtime       runtime.Runtime
	team          *team.Team
	session       *session.Session
	firstMessage  *string
	events        chan tea.Msg
}

func New(title, agentFilename string, rt runtime.Runtime, agents *team.Team, sess *session.Session, firstMessage *string) *App {
	return &App{
		title:         title,
		agentFilename: agentFilename,
		runtime:       rt,
		team:          agents,
		session:       sess,
		firstMessage:  firstMessage,
		events:        make(chan tea.Msg, 128),
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
func (a *App) Run(ctx context.Context, message string) {
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
			a.events <- event
		}
	}()
}

func (a *App) Subscribe(ctx context.Context, program *tea.Program) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-a.events:
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

// ResumeStartOAuth resumes the runtime with OAuth authorization confirmation
func (a *App) ResumeStartOAuth(confirmation bool) {
	if a.runtime != nil {
		a.runtime.ResumeStartAuthorizationFlow(context.Background(), confirmation)
	}
}
