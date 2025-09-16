package team

import (
	"fmt"

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
	names := make([]string, 0, len(t.agents))
	for name := range t.agents {
		names = append(names, name)
	}
	return names
}

func (t *Team) Agent(name string) *agent.Agent {
	if t.agents == nil {
		return nil
	}
	if _, ok := t.agents[name]; !ok {
		return nil
	}
	return t.agents[name]
}

func (t *Team) Size() int {
	return len(t.agents)
}

func (t *Team) StopToolSets() error {
	for _, agent := range t.agents {
		if err := agent.StopToolSets(); err != nil {
			return fmt.Errorf("failed to stop tool sets: %w", err)
		}
	}

	return nil
}
