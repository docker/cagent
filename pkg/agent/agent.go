package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"

	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/tools"
)

// Agent represents an AI agent
type Agent struct {
	name               string
	description        string
	welcomeMessage     string
	instruction        string
	toolsets           []*StartableToolSet
	models             []provider.Provider
	subAgents          []*Agent
	parents            []*Agent
	addDate            bool
	addEnvironmentInfo bool
	maxIterations      int
	numHistoryItems    int
	addPromptFiles     []string
	tools              []tools.Tool
	commands           map[string]string
	pendingWarnings    []string
}

// New creates a new agent
func New(name, prompt string, opts ...Opt) *Agent {
	agent := &Agent{
		name:        name,
		instruction: prompt,
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

// WelcomeMessage returns the agent's welcome message
func (a *Agent) WelcomeMessage() string {
	return a.welcomeMessage
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

// Model returns a random model from the available models
func (a *Agent) Model() provider.Provider {
	return a.models[rand.Intn(len(a.models))]
}

// Commands returns the named commands configured for this agent.
func (a *Agent) Commands() map[string]string {
	return a.commands
}

// Tools returns the tools available to this agent
func (a *Agent) Tools(ctx context.Context) ([]tools.Tool, error) {
	a.ensureToolSetsAreStarted(ctx)

	var agentTools []tools.Tool
	for _, toolSet := range a.toolsets {
		if !toolSet.started.Load() {
			// Toolset failed to start; skip it
			continue
		}
		ta, err := toolSet.Tools(ctx)
		if err != nil {
			slog.Warn("Toolset listing failed; skipping", "agent", a.Name(), "toolset", fmt.Sprintf("%T", toolSet.ToolSet), "error", err)
			a.addToolWarning(fmt.Sprintf("%T list failed: %v", toolSet.ToolSet, err))
			continue
		}
		agentTools = append(agentTools, ta...)
	}

	agentTools = append(agentTools, a.tools...)

	return agentTools, nil
}

func (a *Agent) ToolSets() []tools.ToolSet {
	var toolSets []tools.ToolSet

	for _, ts := range a.toolsets {
		toolSets = append(toolSets, ts)
	}

	return toolSets
}

func (a *Agent) ensureToolSetsAreStarted(ctx context.Context) {
	for _, toolSet := range a.toolsets {
		// Skip if toolset is already started
		if toolSet.started.Load() {
			continue
		}

		if err := toolSet.Start(ctx); err != nil {
			slog.Warn("Toolset start failed; skipping", "agent", a.Name(), "toolset", fmt.Sprintf("%T", toolSet.ToolSet), "error", err)
			a.addToolWarning(fmt.Sprintf("%T start failed: %v", toolSet.ToolSet, err))
			continue
		}

		// Mark toolset as started
		toolSet.started.Store(true)
	}
}

// addToolWarning records a warning generated while loading or starting toolsets.
func (a *Agent) addToolWarning(msg string) {
	if msg == "" {
		return
	}
	a.pendingWarnings = append(a.pendingWarnings, msg)
}

// DrainWarnings returns pending warnings and clears them.
func (a *Agent) DrainWarnings() []string {
	if len(a.pendingWarnings) == 0 {
		return nil
	}
	warnings := a.pendingWarnings
	a.pendingWarnings = nil
	return warnings
}

func (a *Agent) StopToolSets(ctx context.Context) error {
	for _, toolSet := range a.toolsets {
		// Only stop toolsets that are marked as started
		if !toolSet.started.Load() {
			continue
		}

		if err := toolSet.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop toolset: %w", err)
		}

		// Mark toolset as stopped
		toolSet.started.Store(false)
	}

	return nil
}
