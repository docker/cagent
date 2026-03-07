package root

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/sandbox"
)

// runInSandbox delegates the current command to a Docker sandbox.
// It ensures a sandbox exists (creating or recreating as needed), then
// executes docker agent inside it via `docker sandbox exec`.
func runInSandbox(cmd *cobra.Command, runConfig *config.RuntimeConfig, template string) error {
	if environment.InSandbox() {
		return fmt.Errorf("already running inside a Docker sandbox (VM %s)", os.Getenv("SANDBOX_VM_ID"))
	}

	ctx := cmd.Context()
	if err := sandbox.CheckAvailable(ctx); err != nil {
		return err
	}

	cagentArgs := sandbox.BuildCagentArgs(os.Args)
	agentRef := sandbox.AgentRefFromArgs(cagentArgs)
	configDir := paths.GetConfigDir()

	// Always forward config directory paths so the sandbox-side
	// docker agent resolves it to the same host directories
	// (which is mounted read-write by ensureSandbox).
	cagentArgs = sandbox.AppendFlagIfMissing(cagentArgs, "--config-dir", configDir)

	stopTokenWriter := sandbox.StartTokenWriterIfNeeded(ctx, configDir, runConfig.ModelsGateway)
	defer stopTokenWriter()

	// Ensure a sandbox with the right workspace mounts exists.
	wd := cmp.Or(runConfig.WorkingDir, ".")
	name, err := sandbox.Ensure(ctx, wd, sandbox.ExtraWorkspace(wd, agentRef), template, configDir)
	if err != nil {
		return err
	}

	// Resolve env vars the agent needs and forward them into the sandbox.
	// Docker Desktop proxies well-known API keys automatically; this handles
	// any additional vars (e.g. MCP tool secrets).
	envFlags, envVars := sandbox.EnvForAgent(ctx, agentRef, environment.NewDefaultProvider())

	// Forward the gateway as an env var so docker sandbox exec sets it
	// directly inside the sandbox.
	if gateway := runConfig.ModelsGateway; gateway != "" {
		envFlags = append(envFlags, "-e", envModelsGateway+"="+gateway)
	}

	dockerCmd := sandbox.BuildExecCmd(ctx, name, cagentArgs, envFlags, envVars)
	slog.Debug("Executing in sandbox", "name", name, "args", dockerCmd.Args)

	if err := dockerCmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			// Clean up the token writer before exiting so the temp
			// file is removed (defers won't run after os.Exit).
			stopTokenWriter()
			os.Exit(exitErr.ExitCode()) //nolint:gocritic // intentional exit to propagate sandbox exit code
		}
		return fmt.Errorf("docker sandbox exec failed: %w", err)
	}

	return nil
}
