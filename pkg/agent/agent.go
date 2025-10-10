package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"

	"github.com/docker/cagent/pkg/memorymanager"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/tools"
)

// Agent represents an AI agent
type Agent struct {
	name               string
	description        string
	instruction        string
	toolsets           []tools.ToolSet
	startedToolsets    map[tools.ToolSet]bool
	toolsetsMutex      sync.RWMutex
	models             []provider.Provider
	subAgents          []*Agent
	parents            []*Agent
	addDate            bool
	addEnvironmentInfo bool
	maxIterations      int
	numHistoryItems    int
	addPromptFiles     []string
	toolWrapper        toolWrapper
	memoryManager      memorymanager.Manager
	commands           map[string]string
}

// New creates a new agent
func New(name, prompt string, opts ...Opt) *Agent {
	agent := &Agent{
		name:            name,
		instruction:     prompt,
		startedToolsets: make(map[tools.ToolSet]bool),
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

func (a *Agent) Name() string {
	return a.name
}

// Instruction returns the agent's instructions
func (a *Agent) Instruction() string {
	return a.instruction
}

func (a *Agent) AddDate() bool {
	return a.addDate
}

func (a *Agent) AddEnvironmentInfo() bool {
	return a.addEnvironmentInfo
}

func (a *Agent) MaxIterations() int {
	return a.maxIterations
}

func (a *Agent) NumHistoryItems() int {
	return a.numHistoryItems
}

func (a *Agent) AddPromptFiles() []string {
	return a.addPromptFiles
}

// Description returns the agent's description
func (a *Agent) Description() string {
	return a.description
}

// SubAgents returns the list of sub-agent names
func (a *Agent) SubAgents() []*Agent {
	return a.subAgents
}

// Parents returns the list of parent agent names
func (a *Agent) Parents() []*Agent {
	return a.parents
}

// HasSubAgents checks if the agent has sub-agents
func (a *Agent) HasSubAgents() bool {
	return len(a.subAgents) > 0
}

func (a *Agent) HasParents() bool {
	return len(a.parents) > 0
}

// Model returns a random model from the available models
func (a *Agent) Model() provider.Provider {
	return a.models[rand.Intn(len(a.models))]
}

// Tools returns the tools available to this agent
func (a *Agent) Tools(ctx context.Context) ([]tools.Tool, error) {
	if err := a.ensureToolSetsAreStarted(); err != nil {
		return nil, err
	}

	var agentTools []tools.Tool
	for _, toolSet := range a.toolsets {
		ta, err := toolSet.Tools(ctx)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, ta...)
	}

	agentTools = append(agentTools, a.toolWrapper.allTools...)

	return agentTools, nil
}

func (a *Agent) ToolDisplayName(ctx context.Context, toolName string) string {
	allTools, err := a.Tools(ctx)
	if err != nil {
		slog.Error("Failed to get tools for display name", "agent", a.Name(), "error", err)
		return toolName
	}

	for _, tool := range allTools {
		if tool.Name == toolName {
			return tool.DisplayName()
		}
	}

	return toolName
}

func (a *Agent) ToolSets() []tools.ToolSet {
	return a.toolsets
}

// Commands returns the named commands configured for this agent.
func (a *Agent) Commands() map[string]string {
	return a.commands
}

func (a *Agent) ensureToolSetsAreStarted() error {
	a.toolsetsMutex.Lock()
	defer a.toolsetsMutex.Unlock()

	for _, toolSet := range a.toolsets {
		// Skip if toolset is already started
		if a.startedToolsets[toolSet] {
			continue
		}

		// The MCP toolset connection needs to persist beyond the initial HTTP request that triggered its creation.
		// When OAuth succeeds, subsequent agent requests should reuse the already-authenticated MCP connection.
		// But if the connection's underlying context is tied to the first HTTP request, it gets cancelled when that request
		// completes, killing the connection even though OAuth succeeded.
		// Use background context for starting toolsets to ensure they persist beyond request lifecycle
		// This is critical for OAuth flows where the toolset connection needs to remain alive after the initial HTTP request completes.
		if err := toolSet.Start(context.Background()); err != nil {
			return err
		}

		// Mark toolset as started
		a.startedToolsets[toolSet] = true
	}

	return nil
}

func (a *Agent) StopToolSets() error {
	a.toolsetsMutex.Lock()
	defer a.toolsetsMutex.Unlock()

	for _, toolSet := range a.toolsets {
		// Only stop toolsets that are marked as started
		if !a.startedToolsets[toolSet] {
			continue
		}

		if err := toolSet.Stop(); err != nil {
			return fmt.Errorf("failed to stop toolset: %w", err)
		}

		// Mark toolset as stopped
		a.startedToolsets[toolSet] = false
	}

	return nil
}
