package team

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/agent"
)

type Team struct {
	ID     string
	agents map[string]*agent.Agent
}

type Opt func(*Team)

func WithID(id string) Opt {
	return func(t *Team) {
		t.ID = id
	}
}

func WithAgents(agents ...*agent.Agent) Opt {
	return func(t *Team) {
		for _, agent := range agents {
			t.agents[agent.Name()] = agent
		}
	}
}

func New(opts ...Opt) *Team {
	t := &Team{agents: make(map[string]*agent.Agent)}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *Team) AgentNames() []string {
	var names []string
	for name := range t.agents {
		names = append(names, name)
	}
	return names
}

func (t *Team) Agent(name string) (*agent.Agent, error) {
	if len(t.agents) == 0 {
		return nil, errors.New("no agents loaded; ensure your agent configuration defines at least one agent")
	}

	found, ok := t.agents[name]
	if !ok {
		var names []string
		for n := range t.agents {
			names = append(names, n)
		}

		return nil, fmt.Errorf("agent not found: %s (available agents: %s)", name, strings.Join(names, ", "))
	}

	return found, nil
}

func (t *Team) Size() int {
	return len(t.agents)
}

func (t *Team) StopToolSets(ctx context.Context) error {
	for _, agent := range t.agents {
		if err := agent.StopToolSets(ctx); err != nil {
			return fmt.Errorf("failed to stop tool sets: %w", err)
		}
	}

	return nil
}
