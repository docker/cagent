package teamloader

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/js"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/dmr"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/permissions"
	"github.com/docker/cagent/pkg/rag"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tools/codemode"
)

var defaultMaxTokens int64 = 32000

// isThinkingBudgetDisabled returns true if the thinking budget is explicitly set to disable thinking
// (e.g., thinking_budget: 0 or thinking_budget: none).
func isThinkingBudgetDisabled(tb *latest.ThinkingBudget) bool {
	if tb == nil {
		return false
	}
	// Disabled if tokens is explicitly 0
	if tb.Tokens == 0 && tb.Effort == "" {
		return true
	}
	// Disabled if effort is "none"
	return tb.Effort == "none"
}

type loadOptions struct {
	modelOverrides  []string
	toolsetRegistry *ToolsetRegistry
}

type Opt func(*loadOptions) error

func WithModelOverrides(overrides []string) Opt {
	return func(opts *loadOptions) error {
		opts.modelOverrides = overrides
		return nil
	}
}

// WithToolsetRegistry allows using a custom toolset registry instead of the default
func WithToolsetRegistry(registry *ToolsetRegistry) Opt {
	return func(opts *loadOptions) error {
		opts.toolsetRegistry = registry
		return nil
	}
}

// LoadResult contains the result of loading an agent team, including
// the team and configuration needed for runtime model switching.
type LoadResult struct {
	Team      *team.Team
	Models    map[string]latest.ModelConfig
	Providers map[string]latest.ProviderConfig
	// AgentDefaultModels maps agent names to their configured default model references
	AgentDefaultModels map[string]string
}

// Load loads an agent team from the given source
func Load(ctx context.Context, agentSource config.Source, runConfig *config.RuntimeConfig, opts ...Opt) (*team.Team, error) {
	result, err := LoadWithConfig(ctx, agentSource, runConfig, opts...)
	if err != nil {
		return nil, err
	}
	return result.Team, nil
}

// LoadWithConfig loads an agent team and returns both the team and config info
// needed for runtime model switching.
func LoadWithConfig(ctx context.Context, agentSource config.Source, runConfig *config.RuntimeConfig, opts ...Opt) (*LoadResult, error) {
	var loadOpts loadOptions
	loadOpts.toolsetRegistry = NewDefaultToolsetRegistry()

	for _, o := range opts {
		if err := o(&loadOpts); err != nil {
			return nil, err
		}
	}

	// Load the agent's configuration
	cfg, err := config.Load(ctx, agentSource)
	if err != nil {
		return nil, err
	}

	// Resolve model aliases (e.g., "claude-sonnet-4-5" -> "claude-sonnet-4-5-20250929")
	// This ensures the sidebar and other UI elements show the actual model being used.
	config.ResolveModelAliases(ctx, cfg)

	// Apply model overrides from CLI flags before checking required env vars
	if err := config.ApplyModelOverrides(cfg, loadOpts.modelOverrides); err != nil {
		return nil, err
	}

	// Early check for required env vars before loading models and tools.
	env := runConfig.EnvProvider()
	if err := config.CheckRequiredEnvVars(ctx, cfg, runConfig.ModelsGateway, env); err != nil {
		return nil, err
	}

	// Create RAG managers
	parentDir := cmp.Or(agentSource.ParentDir(), runConfig.WorkingDir)
	ragManagers, err := rag.NewManagers(ctx, cfg, rag.ManagersBuildConfig{
		ParentDir:     parentDir,
		ModelsGateway: runConfig.ModelsGateway,
		Env:           env,
		Models:        cfg.Models,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create RAG managers: %w", err)
	}

	// Load agents
	var agents []*agent.Agent
	agentsByName := make(map[string]*agent.Agent)

	autoModel := sync.OnceValue(func() latest.ModelConfig {
		return config.AutoModelConfig(ctx, runConfig.ModelsGateway, env)
	})

	expander := js.NewJsExpander(env)

	for _, agentConfig := range cfg.Agents {
		skillsEnabled := false
		if agentConfig.Skills != nil {
			skillsEnabled = *agentConfig.Skills
		}

		opts := []agent.Opt{
			agent.WithName(agentConfig.Name),
			agent.WithDescription(expander.Expand(ctx, agentConfig.Description)),
			agent.WithWelcomeMessage(expander.Expand(ctx, agentConfig.WelcomeMessage)),
			agent.WithAddDate(agentConfig.AddDate),
			agent.WithAddEnvironmentInfo(agentConfig.AddEnvironmentInfo),
			agent.WithAddDescriptionParameter(agentConfig.AddDescriptionParameter),
			agent.WithAddPromptFiles(agentConfig.AddPromptFiles),
			agent.WithMaxIterations(agentConfig.MaxIterations),
			agent.WithNumHistoryItems(agentConfig.NumHistoryItems),
			agent.WithCommands(expander.ExpandCommands(ctx, agentConfig.Commands)),
			agent.WithSkillsEnabled(skillsEnabled),
			agent.WithHooks(agentConfig.Hooks),
		}

		models, thinkingConfigured, err := getModelsForAgent(ctx, cfg, &agentConfig, autoModel, runConfig)
		if err != nil {
			// Return auto model fallback errors and DMR not installed errors directly
			// without wrapping to provide cleaner messages
			var autoErr *config.ErrAutoModelFallback
			if errors.As(err, &autoErr) || errors.Is(err, dmr.ErrNotInstalled) {
				return nil, err
			}
			return nil, fmt.Errorf("failed to get models: %w", err)
		}
		for _, model := range models {
			opts = append(opts, agent.WithModel(model))
		}
		opts = append(opts, agent.WithThinkingConfigured(thinkingConfigured))

		agentTools, warnings := getToolsForAgent(ctx, &agentConfig, parentDir, runConfig, loadOpts.toolsetRegistry)
		if len(warnings) > 0 {
			opts = append(opts, agent.WithLoadTimeWarnings(warnings))
		}

		// Add RAG tools if agent has RAG sources
		if len(agentConfig.RAG) > 0 {
			ragTools := createRAGToolsForAgent(&agentConfig, ragManagers)
			agentTools = append(agentTools, ragTools...)
		}

		opts = append(opts, agent.WithToolSets(agentTools...))

		ag := agent.New(agentConfig.Name, agentConfig.Instruction, opts...)
		agents = append(agents, ag)
		agentsByName[agentConfig.Name] = ag
	}

	// Connect sub-agents and handoff agents
	for _, agentConfig := range cfg.Agents {
		name := agentConfig.Name

		subAgents := make([]*agent.Agent, 0, len(agentConfig.SubAgents))
		for _, subName := range agentConfig.SubAgents {
			if subAgent, exists := agentsByName[subName]; exists {
				subAgents = append(subAgents, subAgent)
			}
		}

		if a, exists := agentsByName[name]; exists && len(subAgents) > 0 {
			agent.WithSubAgents(subAgents...)(a)
		}

		handoffs := make([]*agent.Agent, 0, len(agentConfig.Handoffs))
		for _, handoffName := range agentConfig.Handoffs {
			if handoffAgent, exists := agentsByName[handoffName]; exists {
				handoffs = append(handoffs, handoffAgent)
			}
		}

		if a, exists := agentsByName[name]; exists && len(handoffs) > 0 {
			agent.WithHandoffs(handoffs...)(a)
		}
	}

	// Create permissions checker from config
	permChecker := permissions.NewChecker(cfg.Permissions)

	// Build agent default models map
	agentDefaultModels := make(map[string]string)
	for _, agent := range cfg.Agents {
		if agent.Model != "" {
			agentDefaultModels[agent.Name] = agent.Model
		}
	}

	return &LoadResult{
		Team: team.New(
			team.WithAgents(agents...),
			team.WithRAGManagers(ragManagers),
			team.WithPermissions(permChecker),
		),
		Models:             cfg.Models,
		Providers:          cfg.Providers,
		AgentDefaultModels: agentDefaultModels,
	}, nil
}

func getModelsForAgent(ctx context.Context, cfg *latest.Config, a *latest.AgentConfig, autoModelFn func() latest.ModelConfig, runConfig *config.RuntimeConfig) ([]provider.Provider, bool, error) {
	var models []provider.Provider
	thinkingConfigured := false

	for name := range strings.SplitSeq(a.Model, ",") {
		modelCfg, exists := cfg.Models[name]
		isAutoModel := false
		if !exists {
			if name == "auto" {
				modelCfg = autoModelFn()
				isAutoModel = true
			} else {
				return nil, false, fmt.Errorf("model '%s' not found in configuration", name)
			}
		}

		// Check if thinking_budget was explicitly configured BEFORE provider defaults are applied.
		// This is used to initialize session thinking state - thinking is only enabled by default
		// when the user explicitly configured it in their YAML.
		if modelCfg.ThinkingBudget != nil && !isThinkingBudgetDisabled(modelCfg.ThinkingBudget) {
			thinkingConfigured = true
		}

		opts := []options.Opt{
			options.WithGateway(runConfig.ModelsGateway),
			options.WithStructuredOutput(a.StructuredOutput),
			options.WithProviders(cfg.Providers),
		}

		// Use max_tokens from config if specified, otherwise look up from models.dev
		var maxTokens *int64
		if modelCfg.MaxTokens != nil {
			maxTokens = modelCfg.MaxTokens
		} else {
			maxTokens = &defaultMaxTokens
			modelsStore, err := modelsdev.NewStore()
			if err != nil {
				return nil, false, err
			}
			m, err := modelsStore.GetModel(ctx, modelCfg.Provider+"/"+modelCfg.Model)
			if err == nil {
				maxTokens = &m.Limit.Output
			}
		}
		if maxTokens != nil {
			opts = append(opts, options.WithMaxTokens(*maxTokens))
		}

		// Pass the full models map for routing rules to resolve model references
		model, err := provider.NewWithModels(ctx,
			&modelCfg,
			cfg.Models,
			runConfig.EnvProvider(),
			opts...,
		)
		if err != nil {
			// Return a cleaner error message for auto model selection failures
			if isAutoModel {
				return nil, false, &config.ErrAutoModelFallback{}
			}
			return nil, false, err
		}
		models = append(models, model)
	}

	return models, thinkingConfigured, nil
}

// getToolsForAgent returns the tool definitions for an agent based on its configuration
func getToolsForAgent(ctx context.Context, a *latest.AgentConfig, parentDir string, runConfig *config.RuntimeConfig, registry *ToolsetRegistry) ([]tools.ToolSet, []string) {
	var (
		toolSets []tools.ToolSet
		warnings []string
	)

	deferredToolset := builtin.NewDeferredToolset()

	for i := range a.Toolsets {
		toolset := a.Toolsets[i]

		tool, err := registry.CreateTool(ctx, toolset, parentDir, runConfig)
		if err != nil {
			// Collect error but continue loading other toolsets
			slog.Warn("Toolset configuration failed; skipping", "type", toolset.Type, "ref", toolset.Ref, "command", toolset.Command, "error", err)
			warnings = append(warnings, fmt.Sprintf("toolset %s failed: %v", toolset.Type, err))
			continue
		}

		wrapped := WithToolsFilter(tool, toolset.Tools...)
		wrapped = WithInstructions(wrapped, toolset.Instruction)
		wrapped = WithToon(wrapped, toolset.Toon)

		// Handle deferred tools
		if !toolset.Defer.IsEmpty() {
			deferredToolset.AddSource(wrapped, toolset.Defer.DeferAll, toolset.Defer.Tools)
			if toolset.Defer.DeferAll {
				// Don't add the wrapped toolset to toolSets - all its tools are deferred
				// TODO: maybe we _do_ want to add this toolset since it has instructions?
				continue
			} else {
				wrapped = WithToolsExcludeFilter(wrapped, toolset.Defer.Tools...)
			}
		}

		toolSets = append(toolSets, wrapped)
	}

	if deferredToolset.HasSources() {
		toolSets = append(toolSets, deferredToolset)
	}

	if len(a.SubAgents) > 0 {
		toolSets = append(toolSets, builtin.NewTransferTaskTool())
	}
	if len(a.Handoffs) > 0 {
		toolSets = append(toolSets, builtin.NewHandoffTool())
	}

	// Wrap all tools in a single Code Mode toolset.
	// This allows the agent to call multiple tools in a single response.
	// It also allows to combine the results of multiple tools in a single response.
	if a.CodeModeTools || runConfig.GlobalCodeMode {
		toolSets = []tools.ToolSet{codemode.Wrap(toolSets...)}
	}

	return toolSets, warnings
}

// createRAGToolsForAgent creates RAG tools for an agent, one for each referenced RAG source
func createRAGToolsForAgent(agentConfig *latest.AgentConfig, allManagers map[string]*rag.Manager) []tools.ToolSet {
	if len(agentConfig.RAG) == 0 {
		return nil
	}

	var ragTools []tools.ToolSet

	for _, ragName := range agentConfig.RAG {
		mgr, exists := allManagers[ragName]
		if !exists {
			slog.Error("RAG source not found", "rag_source", ragName)
			continue
		}

		// Use custom tool name if configured, otherwise use the RAG source name
		toolName := cmp.Or(mgr.ToolName(), ragName)

		// Create a separate tool for this RAG source
		ragTool := builtin.NewRAGTool(mgr, toolName)

		ragTools = append(ragTools, ragTool)

		slog.Debug("Created RAG tool for agent",
			"rag_source", ragName,
			"tool_name", toolName,
			"manager_name", mgr.Name(),
			"description", mgr.Description(),
			"instruction", mgr.ToolInstruction())
	}

	return ragTools
}
