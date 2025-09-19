package teamloader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/memory"
	"github.com/docker/cagent/pkg/memory/database/sqlite"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/secrets"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tools/mcp"
)

// LoadTeams loads all agent teams from the given directory or file path
func LoadTeams(ctx context.Context, agentsPathOrDirectory string, runtimeConfig config.RuntimeConfig) (map[string]*team.Team, error) {
	teams := make(map[string]*team.Team)

	agentPaths, err := FindAgentPaths(agentsPathOrDirectory)
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

// FindAgentPaths finds all agent YAML files in the given directory or returns the single file path
func FindAgentPaths(agentsPathOrDirectory string) ([]string, error) {
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

// checkRequiredEnvVars checks which environment variables are required by the models and tools.
// This allows exiting early with a proper error message instead of failing later when trying to use a model or tool.
func checkRequiredEnvVars(ctx context.Context, cfg *latest.Config, env environment.Provider, runtimeConfig config.RuntimeConfig) error {
	requiredEnv, err := secrets.GatherMissingEnvVars(ctx, cfg, env, runtimeConfig)
	if err != nil {
		return fmt.Errorf("gathering required environment variables: %w", err)
	}

	if len(requiredEnv) == 0 {
		return nil
	}

	return &environment.RequiredEnvError{
		Missing: requiredEnv,
	}
}

func Load(ctx context.Context, path string, runtimeConfig config.RuntimeConfig) (*team.Team, error) {
	fileName := filepath.Base(path)
	parentDir := filepath.Dir(path)

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

	env := environment.NewMultiProvider(
		envFilesProviders,
		environment.NewDefaultProvider(ctx),
	)

	// Load the agent's configuration
	cfg, err := config.LoadConfigSecure(fileName, parentDir)
	if err != nil {
		return nil, err
	}

	// Early check for required env vars before loading models and tools.
	if err := checkRequiredEnvVars(ctx, cfg, env, runtimeConfig); err != nil {
		return nil, err
	}

	// Load agents
	var agents []*agent.Agent
	agentsByName := make(map[string]*agent.Agent)

	sharedTools := map[string]tools.ToolSet{
		"todo": builtin.NewTodoTool(),
	}

	for name := range cfg.Agents {
		agentConfig := cfg.Agents[name]

		models, err := getModelsForAgent(ctx, cfg, &agentConfig, env, runtimeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get models: %w", err)
		}

		opts := []agent.Opt{
			agent.WithName(name),
			agent.WithDescription(agentConfig.Description),
			agent.WithAddDate(agentConfig.AddDate),
			agent.WithAddEnvironmentInfo(agentConfig.AddEnvironmentInfo),
			agent.WithMaxIterations(agentConfig.MaxIterations),
		}
		for _, model := range models {
			opts = append(opts, agent.WithModel(model))
		}

		a, ok := cfg.Agents[name]
		if !ok {
			return nil, fmt.Errorf("agent '%s' not found in configuration", name)
		}
		agentTools, err := getToolsForAgent(ctx, &a, parentDir, sharedTools, models[0], env, runtimeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get tools: %w", err)
		}

		opts = append(opts, agent.WithToolSets(agentTools...))

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

		model, err := provider.New(ctx, &modelCfg, env, options.WithGateway(runtimeConfig.ModelsGateway))
		if err != nil {
			return nil, err
		}

		models = append(models, model)
	}

	return models, nil
}

// getToolsForAgent returns the tool definitions for an agent based on its configuration
func getToolsForAgent(ctx context.Context, a *latest.AgentConfig, parentDir string, sharedTools map[string]tools.ToolSet, model provider.Provider, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) ([]tools.ToolSet, error) {
	var t []tools.ToolSet

	if len(a.SubAgents) > 0 {
		t = append(t, builtin.NewTransferTaskTool())
	}

	toolsets := a.Toolsets
	for i := range toolsets {
		toolset := toolsets[i]

		env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), envProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
		}
		env = append(env, os.Environ()...)

		switch {
		case toolset.Type == "todo":
			if toolset.Shared {
				t = append(t, sharedTools["todo"])
			} else {
				t = append(t, builtin.NewTodoTool())
			}

		case toolset.Type == "memory":
			if toolset.Path == "" {
				continue
			}

			var memoryPath string
			if filepath.IsAbs(toolset.Path) {
				memoryPath = ""
			} else {
				if wd, err := os.Getwd(); err == nil {
					memoryPath = wd
				} else {
					memoryPath = parentDir
				}
			}

			validatedMemoryPath, err := config.ValidatePathInDirectory(toolset.Path, memoryPath)
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

			mm := memory.NewManager(db, model)
			t = append(t, builtin.NewMemoryTool(mm))

		case toolset.Type == "think":
			t = append(t, builtin.NewThinkTool())

		case toolset.Type == "shell":
			t = append(t, builtin.NewShellTool(env))

		case toolset.Type == "script":
			_, _ = json.Marshal(a)
			if len(toolset.Shell) == 0 {
				return nil, fmt.Errorf("shell is required for script toolset")
			}

			t = append(t, builtin.NewScriptShellTool(toolset.Shell, env))

		case toolset.Type == "filesystem":
			wd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("failed to get working directory: %w", err)
			}

			opts := []builtin.FileSystemOpt{builtin.WithAllowedTools(toolset.Tools)}
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

			t = append(t, builtin.NewFilesystemTool([]string{wd}, opts...))

		case toolset.Type == "mcp" && toolset.Ref != "":
			mcpServerName := gateway.ParseServerRef(toolset.Ref)
			if mcpServerURL := os.Getenv(mcp.ENV_DOCKER_MCP_URL_PREFIX + mcpServerName); mcpServerURL != "" {
				t = append(t, mcp.NewToolsetCommand("socat", []string{"STDIO", fmt.Sprintf("TCP:mcp-%s:4444", mcpServerName)}, nil, toolset.Tools))
			} else {
				t = append(t, mcp.NewGatewayToolset(mcpServerName, toolset.Config, toolset.Tools, envProvider))
			}

		case toolset.Type == "mcp" && toolset.Command != "":
			t = append(t, mcp.NewToolsetCommand(toolset.Command, toolset.Args, env, toolset.Tools))

		case toolset.Type == "mcp" && toolset.Remote.URL != "":
			// TODO: the tool's config can set env variables that could be used in headers.
			// Expand env vars in headers.
			headers := map[string]string{}
			for k, v := range toolset.Remote.Headers {
				expanded, err := environment.Expand(ctx, v, envProvider)
				if err != nil {
					return nil, fmt.Errorf("failed to expand header '%s': %w", k, err)
				}

				headers[k] = expanded
			}

			mcpc, err := mcp.NewToolsetRemote(toolset.Remote.URL, toolset.Remote.TransportType, headers, toolset.Tools, runtimeConfig.RedirectURI)
			if err != nil {
				return nil, fmt.Errorf("failed to create remote mcp client: %w", err)
			}

			t = append(t, mcpc)

		default:
			return nil, fmt.Errorf("unknown toolset type: %s", toolset.Type)
		}
	}

	return t, nil
}
