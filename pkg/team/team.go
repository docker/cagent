package team

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config/types"
	"github.com/docker/cagent/pkg/permissions"
	"github.com/docker/cagent/pkg/rag"
)

type Team struct {
	agents      []*agent.Agent
	ragManagers map[string]*rag.Manager
	permissions *permissions.Checker
}

type Opt func(*Team)

func WithAgents(agents ...*agent.Agent) Opt {
	return func(t *Team) {
		t.agents = agents
	}
}

func WithRAGManagers(managers map[string]*rag.Manager) Opt {
	return func(t *Team) {
		t.ragManagers = managers
	}
}

func WithPermissions(checker *permissions.Checker) Opt {
	return func(t *Team) {
		t.permissions = checker
	}
}

func New(opts ...Opt) *Team {
	t := &Team{
		ragManagers: make(map[string]*rag.Manager),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *Team) AgentNames() []string {
	var names []string
	for i := range t.agents {
		names = append(names, t.agents[i].Name())
	}
	return names
}

// AgentInfo contains information about an agent
type AgentInfo struct {
	Name        string
	Description string
	Provider    string
	Model       string
	Commands    types.Commands
}

// AgentsInfo returns information about all agents in the team
func (t *Team) AgentsInfo() []AgentInfo {
	var infos []AgentInfo
	for _, a := range t.agents {
		info := AgentInfo{
			Name:        a.Name(),
			Description: a.Description(),
			Commands:    a.Commands(),
		}
		if model := a.Model(); model != nil {
			modelID := model.ID()
			if prov, modelName, found := strings.Cut(modelID, "/"); found {
				info.Provider = prov
				info.Model = modelName
			} else {
				info.Model = modelID
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func (t *Team) DefaultAgent() (*agent.Agent, error) {
	if t.Size() == 0 {
		return nil, errors.New("no agents loaded; ensure your agent configuration defines at least one agent")
	}

	// Before v4, the default agent was the one named "root". If it exists, return it.
	for _, a := range t.agents {
		if a.Name() == "root" {
			return a, nil
		}
	}

	// Otherwise, return the first agent.
	return t.agents[0], nil
}

func (t *Team) Agent(name string) (*agent.Agent, error) {
	if t.Size() == 0 {
		return nil, errors.New("no agents loaded; ensure your agent configuration defines at least one agent")
	}

	for _, a := range t.agents {
		if a.Name() == name {
			return a, nil
		}
	}

	return nil, fmt.Errorf("agent not found: %s (available agents: %s)", name, strings.Join(t.AgentNames(), ", "))
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
	for name, mgr := range t.ragManagers {
		if err := mgr.Close(); err != nil {
			slog.Error("Failed to close RAG manager", "name", name, "error", err)
		}
	}

	return nil
}

// RAGManagers returns the RAG managers for this team
func (t *Team) RAGManagers() map[string]*rag.Manager {
	return t.ragManagers
}

// InitializeRAG initializes all RAG managers in the background
func (t *Team) InitializeRAG(ctx context.Context) {
	for _, mgr := range t.ragManagers {
		go func(m *rag.Manager) {
			slog.Debug("Starting RAG manager initialization goroutine", "rag", m.Name())
			if err := m.Initialize(ctx); err != nil {
				slog.Error("Failed to initialize RAG manager", "rag", m.Name(), "error", err)
			} else {
				slog.Info("RAG manager initialized successfully", "rag", m.Name())
			}
		}(mgr)
	}
}

// StartRAGFileWatchers starts file watchers for all RAG managers
func (t *Team) StartRAGFileWatchers(ctx context.Context) {
	for _, mgr := range t.ragManagers {
		go func(m *rag.Manager) {
			slog.Debug("Starting RAG file watcher goroutine", "rag", m.Name())
			if err := m.StartFileWatcher(ctx); err != nil {
				slog.Error("Failed to start RAG file watcher", "rag", m.Name(), "error", err)
			}
		}(mgr)
	}
}

// Permissions returns the permission checker for this team.
// Returns nil if no permissions are configured.
func (t *Team) Permissions() *permissions.Checker {
	return t.permissions
}
