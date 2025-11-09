package teamloader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/js"
	"github.com/docker/cagent/pkg/memory/database/sqlite"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/path"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tools/codemode"
	"github.com/docker/cagent/pkg/tools/mcp"
)

// ToolsetCreator is a function that creates a toolset based on the provided configuration
type ToolsetCreator func(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error)

// ToolsetRegistry manages the registration of toolset creators by type
type ToolsetRegistry struct {
	creators map[string]ToolsetCreator
}

// NewToolsetRegistry creates a new empty toolset registry
func NewToolsetRegistry() *ToolsetRegistry {
	return &ToolsetRegistry{
		creators: make(map[string]ToolsetCreator),
	}
}

// Register adds a new toolset creator for the given type
func (r *ToolsetRegistry) Register(toolsetType string, creator ToolsetCreator) {
	r.creators[toolsetType] = creator
}

// Get retrieves a toolset creator for the given type
func (r *ToolsetRegistry) Get(toolsetType string) (ToolsetCreator, bool) {
	creator, ok := r.creators[toolsetType]
	return creator, ok
}

// CreateTool creates a toolset using the registered creator for the given type
func (r *ToolsetRegistry) CreateTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	creator, ok := r.Get(toolset.Type)
	if !ok {
		return nil, fmt.Errorf("unknown toolset type: %s", toolset.Type)
	}
	return creator(ctx, toolset, parentDir, envProvider, runtimeConfig)
}

func NewDefaultToolsetRegistry() *ToolsetRegistry {
	r := NewToolsetRegistry()
	// Register all built-in toolset creators
	r.Register("todo", createTodoTool)
	r.Register("memory", createMemoryTool)
	r.Register("think", createThinkTool)
	r.Register("shell", createShellTool)
	r.Register("script", createScriptTool)
	r.Register("filesystem", createFilesystemTool)
	r.Register("fetch", createFetchTool)
	r.Register("mcp", createMCPTool)
	r.Register("api", createAPITool)
	return r
}

func createTodoTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	if toolset.Shared {
		return builtin.NewSharedTodoTool(), nil
	}
	return builtin.NewTodoTool(), nil
}

func createMemoryTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	var memoryPath string
	if filepath.IsAbs(toolset.Path) {
		memoryPath = ""
	} else if wd, err := os.Getwd(); err == nil {
		memoryPath = wd
	} else {
		memoryPath = parentDir
	}

	validatedMemoryPath, err := path.ValidatePathInDirectory(toolset.Path, memoryPath)
	if err != nil {
		return nil, fmt.Errorf("invalid memory database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(validatedMemoryPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create memory database directory: %w", err)
	}

	db, err := sqlite.NewMemoryDatabase(validatedMemoryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory database: %w", err)
	}

	return builtin.NewMemoryTool(db), nil
}

func createThinkTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	return builtin.NewThinkTool(), nil
}

func createShellTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), envProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
	}
	env = append(env, os.Environ()...)
	return builtin.NewShellTool(env), nil
}

func createScriptTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	if len(toolset.Shell) == 0 {
		return nil, fmt.Errorf("shell is required for script toolset")
	}

	env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), envProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
	}
	env = append(env, os.Environ()...)
	return builtin.NewScriptShellTool(toolset.Shell, env), nil
}

func createFilesystemTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	wd := runtimeConfig.WorkingDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	var opts []builtin.FileSystemOpt
	if len(toolset.PostEdit) > 0 {
		postEditConfigs := make([]builtin.PostEditConfig, len(toolset.PostEdit))
		for i, pe := range toolset.PostEdit {
			postEditConfigs[i] = builtin.PostEditConfig{
				Path: pe.Path,
				Cmd:  pe.Cmd,
			}
		}
		opts = append(opts, builtin.WithPostEditCommands(postEditConfigs))
	}

	return builtin.NewFilesystemTool([]string{wd}, opts...), nil
}

func createAPITool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	if toolset.APIConfig.Endpoint == "" {
		return nil, fmt.Errorf("api tool requires an endpoint in api_config")
	}

	toolset.APIConfig.Headers = js.Expand(ctx, toolset.APIConfig.Headers, envProvider)

	return builtin.NewAPITool(toolset.APIConfig), nil
}

func createFetchTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	var opts []builtin.FetchToolOption
	if toolset.Timeout > 0 {
		timeout := time.Duration(toolset.Timeout) * time.Second
		opts = append(opts, builtin.WithTimeout(timeout))
	}
	return builtin.NewFetchTool(opts...), nil
}

func createMCPTool(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
	// MCP tool has three different modes: ref, command, and remote
	if toolset.Ref != "" {
		mcpServerName := gateway.ParseServerRef(toolset.Ref)
		serverSpec, err := gateway.ServerSpec(ctx, mcpServerName)
		if err != nil {
			return nil, fmt.Errorf("fetching MCP server spec for %q: %w", mcpServerName, err)
		}

		// TODO(dga): until the MCP Gateway supports oauth with cagent, we fetch the remote url and directly connect to it.
		if serverSpec.Type == "remote" {
			return mcp.NewRemoteToolset(serverSpec.Remote.URL, serverSpec.Remote.TransportType, nil, runtimeConfig.RedirectURI), nil
		}

		return mcp.NewGatewayToolset(ctx, mcpServerName, toolset.Config, envProvider)
	}

	if toolset.Command != "" {
		env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), envProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
		}
		env = append(env, os.Environ()...)
		return mcp.NewToolsetCommand(toolset.Command, toolset.Args, env), nil
	}

	if toolset.Remote.URL != "" {
		headers := map[string]string{}
		for k, v := range toolset.Remote.Headers {
			expanded, err := environment.Expand(ctx, v, envProvider)
			if err != nil {
				return nil, fmt.Errorf("failed to expand header '%s': %w", k, err)
			}

			headers[k] = expanded
		}

		return mcp.NewRemoteToolset(toolset.Remote.URL, toolset.Remote.TransportType, headers, runtimeConfig.RedirectURI), nil
	}

	return nil, fmt.Errorf("mcp toolset requires either ref, command, or remote configuration")
}

// LoadTeams loads all agent teams from the given directory or file path
func LoadTeams(ctx context.Context, agentsPathOrDirectory string, runtimeConfig config.RuntimeConfig) (map[string]*team.Team, error) {
	teams := make(map[string]*team.Team)

	agentPaths, err := findAgentPaths(agentsPathOrDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to find agents: %w", err)
	}

	for _, agentPath := range agentPaths {
		t, err := Load(ctx, agentPath, runtimeConfig)
		if err != nil {
			slog.Warn("Failed to load agent", "file", agentPath, "error", err)
			continue
		}

		teams[t.ID] = t
	}

	return teams, nil
}

// findAgentPaths finds all agent YAML files in the given directory or returns the single file path
func findAgentPaths(agentsPathOrDirectory string) ([]string, error) {
	stat, err := os.Stat(agentsPathOrDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to stat agents path: %w", err)
	}

	if !stat.IsDir() {
		return []string{agentsPathOrDirectory}, nil
	}

	var agents []string

	agentsDirectory := agentsPathOrDirectory
	entries, err := os.ReadDir(agentsDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		agents = append(agents, filepath.Join(agentsDirectory, entry.Name()))
	}

	return agents, nil
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

func Load(ctx context.Context, p string, runtimeConfig config.RuntimeConfig, opts ...Opt) (*team.Team, error) {
	var loadOpts loadOptions
	loadOpts.toolsetRegistry = NewDefaultToolsetRegistry()

	for _, o := range opts {
		if err := o(&loadOpts); err != nil {
			return nil, err
		}
	}

	fileName := filepath.Base(p)
	parentDir := filepath.Dir(p)

	// Make env file paths absolute relative to the agent config file.
	var err error
	runtimeConfig.EnvFiles, err = environment.AbsolutePaths(parentDir, runtimeConfig.EnvFiles)
	if err != nil {
		return nil, err
	}

	envFilesProviders, err := environment.NewEnvFilesProvider(runtimeConfig.EnvFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to read env files: %w", err)
	}

	defaultEnvProvider := runtimeConfig.DefaultEnvProvider
	if defaultEnvProvider == nil {
		defaultEnvProvider = environment.NewDefaultProvider()
	}
	env := environment.NewMultiProvider(envFilesProviders, defaultEnvProvider)

	// Load the agent's configuration
	cfg, err := config.LoadConfigSecureDeprecated(fileName, parentDir)
	if err != nil {
		return nil, err
	}

	// Apply model overrides from CLI flags before checking required env vars
	if err := config.ApplyModelOverrides(cfg, loadOpts.modelOverrides); err != nil {
		return nil, err
	}

	// Early check for required env vars before loading models and tools.
	if err := config.CheckRequiredEnvVars(ctx, cfg, env, runtimeConfig); err != nil {
		return nil, err
	}

	// Load agents
	var agents []*agent.Agent
	agentsByName := make(map[string]*agent.Agent)

	for name, agentConfig := range cfg.Agents {
		opts := []agent.Opt{
			agent.WithName(name),
			agent.WithDescription(agentConfig.Description),
			agent.WithWelcomeMessage(agentConfig.WelcomeMessage),
			agent.WithAddDate(agentConfig.AddDate),
			agent.WithAddEnvironmentInfo(agentConfig.AddEnvironmentInfo),
			agent.WithAddPromptFiles(agentConfig.AddPromptFiles),
			agent.WithMaxIterations(agentConfig.MaxIterations),
			agent.WithNumHistoryItems(agentConfig.NumHistoryItems),
			agent.WithCommands(js.Expand(ctx, agentConfig.Commands, env)),
		}

		models, err := getModelsForAgent(ctx, cfg, &agentConfig, env, runtimeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get models: %w", err)
		}
		for _, model := range models {
			opts = append(opts, agent.WithModel(model))
		}

		agentTools, warnings := getToolsForAgent(ctx, &agentConfig, parentDir, env, runtimeConfig, loadOpts.toolsetRegistry)
		if len(warnings) > 0 {
			opts = append(opts, agent.WithLoadTimeWarnings(warnings))
		}

		if len(agentConfig.SubAgents) > 0 {
			agentTools = append(agentTools, builtin.NewTransferTaskTool())
		}

		if len(agentTools) > 0 {
			opts = append(opts, agent.WithToolSets(agentTools...))
		}

		ag := agent.New(name, agentConfig.Instruction, opts...)
		agents = append(agents, ag)
		agentsByName[name] = ag
	}

	for name := range cfg.Agents {
		agentConfig := cfg.Agents[name]
		if len(agentConfig.SubAgents) == 0 {
			continue
		}

		subAgents := make([]*agent.Agent, 0, len(agentConfig.SubAgents))
		for _, subName := range agentConfig.SubAgents {
			if subAgent, exists := agentsByName[subName]; exists {
				subAgents = append(subAgents, subAgent)
			}
		}

		if a, exists := agentsByName[name]; exists && len(subAgents) > 0 {
			agent.WithSubAgents(subAgents...)(a)
		}
	}

	return team.New(team.WithID(fileName), team.WithAgents(agents...)), nil
}

func getModelsForAgent(ctx context.Context, cfg *latest.Config, a *latest.AgentConfig, env environment.Provider, runtimeConfig config.RuntimeConfig) ([]provider.Provider, error) {
	var models []provider.Provider

	for name := range strings.SplitSeq(a.Model, ",") {
		modelCfg, exists := cfg.Models[name]
		if !exists {
			return nil, fmt.Errorf("model '%s' not found in configuration", name)
		}

		opts := []options.Opt{options.WithGateway(runtimeConfig.ModelsGateway)}
		if a.StructuredOutput != nil {
			opts = append(opts, options.WithStructuredOutput(a.StructuredOutput))
		}

		model, err := provider.New(ctx, &modelCfg, env, opts...)
		if err != nil {
			return nil, err
		}

		models = append(models, model)
	}

	return models, nil
}

// getToolsForAgent returns the tool definitions for an agent based on its configuration
func getToolsForAgent(ctx context.Context, a *latest.AgentConfig, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig, registry *ToolsetRegistry) ([]tools.ToolSet, []string) {
	var (
		toolSets []tools.ToolSet
		warnings []string
	)

	for i := range a.Toolsets {
		toolset := a.Toolsets[i]

		tool, err := registry.CreateTool(ctx, toolset, parentDir, envProvider, runtimeConfig)
		if err != nil {
			// Collect error but continue loading other toolsets
			slog.Warn("Toolset configuration failed; skipping", "type", toolset.Type, "ref", toolset.Ref, "command", toolset.Command, "error", err)
			warnings = append(warnings, fmt.Sprintf("toolset %s failed: %v", toolset.Type, err))
			continue
		}

		wrapped := WithToolsFilter(tool, toolset.Tools...)
		wrapped = WithInstructions(wrapped, toolset.Instruction)
		wrapped = WithToon(wrapped, toolset.Toon)

		toolSets = append(toolSets, wrapped)
	}

	// Wrap all tools in a single Code Mode toolset.
	// This allows the agent to call multiple tools in a single response.
	// It also allows to combine the results of multiple tools in a single response.
	if a.CodeModeTools || runtimeConfig.GlobalCodeMode {
		toolSets = []tools.ToolSet{codemode.Wrap(toolSets...)}
	}

	return toolSets, warnings
}
