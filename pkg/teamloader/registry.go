package teamloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/js"
	"github.com/docker/cagent/pkg/memory/database/sqlite"
	"github.com/docker/cagent/pkg/path"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tools/mcp"
)

// ToolsetCreator is a function that creates a toolset based on the provided configuration
type ToolsetCreator func(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig) (tools.ToolSet, error)

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
func (r *ToolsetRegistry) CreateTool(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	creator, ok := r.Get(toolset.Type)
	if !ok {
		return nil, fmt.Errorf("unknown toolset type: %s", toolset.Type)
	}
	return creator(ctx, toolset, parentDir, runConfig)
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

func createTodoTool(_ context.Context, toolset latest.Toolset, _ string, _ *config.RuntimeConfig) (tools.ToolSet, error) {
	if toolset.Shared {
		return builtin.NewSharedTodoTool(), nil
	}
	return builtin.NewTodoTool(), nil
}

func createMemoryTool(_ context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	var memoryPath string
	if filepath.IsAbs(toolset.Path) {
		memoryPath = ""
	} else if wd := runConfig.WorkingDir; wd != "" {
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

func createThinkTool(_ context.Context, _ latest.Toolset, _ string, _ *config.RuntimeConfig) (tools.ToolSet, error) {
	return builtin.NewThinkTool(), nil
}

func createShellTool(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), runConfig.EnvProvider())
	if err != nil {
		return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
	}
	env = append(env, os.Environ()...)
	return builtin.NewShellTool(env, runConfig), nil
}

func createScriptTool(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	if len(toolset.Shell) == 0 {
		return nil, fmt.Errorf("shell is required for script toolset")
	}

	env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), runConfig.EnvProvider())
	if err != nil {
		return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
	}
	env = append(env, os.Environ()...)
	return builtin.NewScriptShellTool(toolset.Shell, env)
}

func createFilesystemTool(_ context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	wd := runConfig.WorkingDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	var opts []builtin.FileSystemOpt

	// Handle ignore_vcs configuration (default to true)
	ignoreVCS := true
	if toolset.IgnoreVCS != nil {
		ignoreVCS = *toolset.IgnoreVCS
	}
	opts = append(opts, builtin.WithIgnoreVCS(ignoreVCS))

	// Handle post-edit commands
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

	return builtin.NewFilesystemTool(wd, opts...), nil
}

func createAPITool(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	if toolset.APIConfig.Endpoint == "" {
		return nil, fmt.Errorf("api tool requires an endpoint in api_config")
	}

	expander := js.NewJsExpander(runConfig.EnvProvider())
	toolset.APIConfig.Headers = expander.ExpandMap(ctx, toolset.APIConfig.Headers)

	return builtin.NewAPITool(toolset.APIConfig), nil
}

func createFetchTool(_ context.Context, toolset latest.Toolset, _ string, _ *config.RuntimeConfig) (tools.ToolSet, error) {
	var opts []builtin.FetchToolOption
	if toolset.Timeout > 0 {
		timeout := time.Duration(toolset.Timeout) * time.Second
		opts = append(opts, builtin.WithTimeout(timeout))
	}
	return builtin.NewFetchTool(opts...), nil
}

func createMCPTool(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig) (tools.ToolSet, error) {
	// MCP tool has three different modes: ref, command, and remote
	if toolset.Ref != "" {
		mcpServerName := gateway.ParseServerRef(toolset.Ref)
		serverSpec, err := gateway.ServerSpec(ctx, mcpServerName)
		if err != nil {
			return nil, fmt.Errorf("fetching MCP server spec for %q: %w", mcpServerName, err)
		}

		// TODO(dga): until the MCP Gateway supports oauth with cagent, we fetch the remote url and directly connect to it.
		if serverSpec.Type == "remote" {
			return mcp.NewRemoteToolset(serverSpec.Remote.URL, serverSpec.Remote.TransportType, nil), nil
		}

		return mcp.NewGatewayToolset(ctx, mcpServerName, toolset.Config, runConfig.EnvProvider(), runConfig.WorkingDir)
	}

	if toolset.Command != "" {
		env, err := environment.ExpandAll(ctx, environment.ToValues(toolset.Env), runConfig.EnvProvider())
		if err != nil {
			return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
		}
		env = append(env, os.Environ()...)
		return mcp.NewToolsetCommand(toolset.Command, toolset.Args, env, runConfig.WorkingDir), nil
	}

	if toolset.Remote.URL != "" {
		headers := map[string]string{}
		for k, v := range toolset.Remote.Headers {
			expanded, err := environment.Expand(ctx, v, runConfig.EnvProvider())
			if err != nil {
				return nil, fmt.Errorf("failed to expand header '%s': %w", k, err)
			}

			headers[k] = expanded
		}

		return mcp.NewRemoteToolset(toolset.Remote.URL, toolset.Remote.TransportType, headers), nil
	}

	return nil, fmt.Errorf("mcp toolset requires either ref, command, or remote configuration")
}
