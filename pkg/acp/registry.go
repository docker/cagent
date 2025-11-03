package acp

import (
	"context"
	"os"

	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

// createToolsetRegistry creates a custom toolset registry with ACP-specific filesystem toolset
func createToolsetRegistry(agent *Agent) *teamloader.ToolsetRegistry {
	registry := teamloader.NewDefaultToolsetRegistry()

	registry.Register("filesystem", func(ctx context.Context, toolset latest.Toolset, parentDir string, envProvider environment.Provider, runtimeConfig config.RuntimeConfig) (tools.ToolSet, error) {
		wd := runtimeConfig.WorkingDir
		if wd == "" {
			var err error
			wd, err = os.Getwd()
			if err != nil {
				return nil, err
			}
		}

		var opts []builtin.FileSystemOpt
		allowedDirectories := []string{wd}
		if len(toolset.AllowedDirectories) > 0 {
			opts = append(opts, builtin.WithAllowedDirectories(append(allowedDirectories, toolset.AllowedDirectories...)))
		}

		return NewFilesystemToolset(agent, opts...), nil
	})

	return registry
}
