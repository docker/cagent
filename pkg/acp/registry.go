package acp

import (
	"context"
	"os"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
)

// createToolsetRegistry creates a custom ToolsetRegistry for the ACP agent.
//
// Responsibilities:
// - start from the default toolset registry (YAML + built-in toolsets)
// - register ACP-aware toolsets (e.g. filesystem)
// - act as the single extension point for ACP-specific integrations
//
// NOTE:
// MCP toolsets provided by the ACP client are NOT registered here yet.
// This function intentionally only prepares the registry structure.
// MCP toolset injection is handled separately at the session level
// to respect ACP scoping rules (MCP servers are session-scoped).
func createToolsetRegistry(agent *Agent) *teamloader.ToolsetRegistry {
	// Start with the default registry (built-in + YAML-defined toolsets)
	registry := teamloader.NewDefaultToolsetRegistry()

	// Register ACP-aware filesystem toolset.
	//
	// This wraps the standard filesystem tools to allow ACP-specific
	// behavior such as:
	// - respecting the client's working directory
	// - interacting with the ACP connection when needed
	registry.Register(
		"filesystem",
		func(
			ctx context.Context,
			toolset latest.Toolset,
			parentDir string,
			runConfig *config.RuntimeConfig,
		) (tools.ToolSet, error) {

			// Determine working directory:
			// 1. runtime config working dir
			// 2. fallback to process working directory
			wd := runConfig.WorkingDir
			if wd == "" {
				var err error
				wd, err = os.Getwd()
				if err != nil {
					return nil, err
				}
			}

			return NewFilesystemToolset(agent, wd), nil
		},
	)

	return registry
}
