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
	latest "github.com/docker/cagent/pkg/config/v1"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/memory"
	"github.com/docker/cagent/pkg/memory/database/sqlite"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tools/mcp"
)

// LoadTeams loads all agent teams from the given directory or file path
func LoadTeams(ctx context.Context, agentsPathOrDirectory string, runConfig latest.RuntimeConfig) (map[string]*team.Team, error) {
	teams := make(map[string]*team.Team)

	agentPaths, err := FindAgentPaths(agentsPathOrDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to find agents: %w", err)
	}

	for _, agentPath := range agentPaths {
		t, err := Load(ctx, agentPath, runConfig)
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

func Load(ctx context.Context, path string, runConfig latest.RuntimeConfig) (*team.Team, error) {
	parentDir := filepath.Dir(path)
	cfg, err := config.LoadConfigSecure(path, parentDir)
	if err != nil {
		return nil, err
	}
	fileName := filepath.Base(path)
	absEnvFiles, err := environment.AbsolutePaths(parentDir, runConfig.EnvFiles)
	if err != nil {
		return nil, err
	}

	var agents []*agent.Agent
	agentsByName := make(map[string]*agent.Agent)

	sharedTools := map[string]tools.ToolSet{
		"todo": builtin.NewTodoTool(),
	}

	for name := range cfg.Agents {
		agentConfig := cfg.Agents[name]

		opts := []agent.Opt{
			agent.WithName(name),
			agent.WithDescription(agentConfig.Description),
			agent.WithAddDate(agentConfig.AddDate),
			agent.WithAddEnvironmentInfo(agentConfig.AddEnvironmentInfo),
		}
		models, err := getModelsForAgent(ctx, cfg, &agentConfig, absEnvFiles, options.WithGateway(runConfig.ModelsGateway))
		if err != nil {
			return nil, fmt.Errorf("failed to get models: %w", err)
		}
		for _, model := range models {
			opts = append(opts, agent.WithModel(model))
		}

		a, ok := cfg.Agents[name]
		if !ok {
			return nil, fmt.Errorf("agent '%s' not found in configuration", name)
		}
		agentTools, err := getToolsForAgent(&a, parentDir, sharedTools, models[0], absEnvFiles, runConfig.ToolsGateway)
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

func getModelsForAgent(ctx context.Context, cfg *latest.Config, a *latest.AgentConfig, absEnvFiles []string, opts ...options.Opt) ([]provider.Provider, error) {
	var models []provider.Provider

	for name := range strings.SplitSeq(a.Model, ",") {
		modelCfg, exists := cfg.Models[name]
		if !exists {
			return nil, fmt.Errorf("model '%s' not found in configuration", name)
		}

		providers := []environment.Provider{
			environment.NewKeyValueProvider(modelCfg.Env),
			environment.NewKeyValueProvider(cfg.Env),
			environment.NewEnvFilesProvider(absEnvFiles),
			environment.NewOsEnvProvider(), // TODO(dga): Which env should take precedence? OS or config?
			environment.NewNoFailProvider(
				environment.NewOnePasswordProvider(),
			),
		}

		// Append pass provider at the end if available
		passProvider, err := environment.NewPassProvider()
		if err == nil {
			providers = append(providers, passProvider)
		}

		env := environment.NewMultiProvider(providers...)

		model, err := provider.New(ctx, &modelCfg, env, opts...)
		if err != nil {
			return nil, err
		}

		models = append(models, model)
	}

	return models, nil
}

// getToolsForAgent returns the tool definitions for an agent based on its configuration
func getToolsForAgent(a *latest.AgentConfig, parentDir string, sharedTools map[string]tools.ToolSet, model provider.Provider, absEnvFiles []string, gateway string) ([]tools.ToolSet, error) {
	var t []tools.ToolSet

	if len(a.SubAgents) > 0 {
		t = append(t, builtin.NewTransferTaskTool())
	}

	toolsets := a.Toolsets
	for i := range toolsets {
		toolset := toolsets[i]

		switch {
		case toolset.Type == "todo":
			if toolset.Shared {
				t = append(t, sharedTools["todo"])
			} else {
				t = append(t, builtin.NewTodoTool())
			}
		case toolset.Type == "memory":
			if toolset.Path != "" {
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
			}
		case toolset.Type == "think":
			t = append(t, builtin.NewThinkTool())
		case toolset.Type == "shell":
			t = append(t, builtin.NewShellTool())

		case toolset.Type == "script":
			b, _ := json.Marshal(a)
			fmt.Println(string(b))
			if len(toolset.Shell) == 0 {
				return nil, fmt.Errorf("shell is required for script toolset")
			}

			t = append(t, builtin.NewScriptShellTool(toolset.Shell))
		case toolset.Type == "filesystem":
			wd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("failed to get working directory: %w", err)
			}

			t = append(t, builtin.NewFilesystemTool([]string{wd}, builtin.WithAllowedTools(toolset.Tools)))

		case toolset.Type == "mcp" && toolset.Command != "":
			if gateway != "" {
				// TODO(dga): Really guess the server.
				var servers string
				if toolset.Command == "docker" && len(toolset.Args) >= 4 && toolset.Args[0] == "mcp" && toolset.Args[1] == "gateway" && toolset.Args[2] == "run" && strings.HasPrefix(toolset.Args[3], "--servers=") {
					servers = strings.TrimPrefix(toolset.Args[3], "--servers=")
				}

				headers := map[string]string{
					"x-mcp-servers": servers,
				}

				mcpc, err := mcp.NewToolsetRemote(gateway+"/mcp", "streamable", headers, toolset.Tools)
				if err != nil {
					return nil, fmt.Errorf("failed to create remote mcp client: %w", err)
				}

				t = append(t, mcpc)
			} else {
				// Expand env first because it's used when expanding command and args.
				env, err := toolsetEnv(toolset.Env, append(absEnvFiles, toolset.Envfiles...), parentDir)
				if err != nil {
					return nil, err
				}

				// Expand command.
				command := environment.Expand(toolset.Command, append(os.Environ(), env...))

				// Expand args.
				var args []string
				for _, arg := range toolset.Args {
					args = append(args, environment.Expand(arg, append(os.Environ(), env...)))
				}

				t = append(t, mcp.NewToolsetCommand(command, args, env, toolset.Tools))
			}

		case toolset.Type == "mcp" && toolset.Remote.URL != "":
			// Expand env first because it's used when expanding headers.
			env, err := toolsetEnv(toolset.Env, toolset.Envfiles, parentDir)
			if err != nil {
				return nil, err
			}

			// Expand headers.
			headers := map[string]string{}
			for k, v := range toolset.Remote.Headers {
				headers[k] = environment.Expand(v, append(os.Environ(), env...))
			}

			mcpc, err := mcp.NewToolsetRemote(toolset.Remote.URL, toolset.Remote.TransportType, headers, toolset.Tools)
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

func toolsetEnv(env map[string]string, envFiles []string, parentDir string) ([]string, error) {
	var envSlice []string

	for k, v := range env {
		v = environment.Expand(v, os.Environ())
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}

	absoluteEnvFiles, err := environment.AbsolutePaths(parentDir, envFiles)
	if err != nil {
		return nil, err
	}

	keyValues, err := environment.ReadEnvFiles(absoluteEnvFiles)
	if err != nil {
		return nil, err
	}

	for _, kv := range keyValues {
		v := environment.Expand(kv.Value, os.Environ())
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", kv.Key, v))
	}

	return envSlice, nil
}
