package agentfile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/aliases"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/remote"
)

// IsLocalFile checks if the input is a local file
func IsLocalFile(input string) bool {
	ext := strings.ToLower(filepath.Ext(input))
	// Check for YAML file extensions or file descriptors
	if ext == ".yaml" || ext == ".yml" || strings.HasPrefix(input, "/dev/fd/") {
		return true
	}
	// Check if it exists as a file on disk
	return fileExists(input)
}

// OciRefToFilename converts an OCI reference to a safe, consistent filename
// Examples:
//   - "docker.io/myorg/agent:v1" -> "docker.io_myorg_agent_v1.yaml"
//   - "localhost:5000/test" -> "localhost_5000_test.yaml"
func OciRefToFilename(ociRef string) string {
	// Replace characters that are invalid in filenames with underscores
	// Keep the structure recognizable but filesystem-safe
	safe := strings.NewReplacer(
		"/", "_",
		":", "_",
		"@", "_",
		"\\", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	).Replace(ociRef)

	// Ensure it has .yaml extension
	if !strings.HasSuffix(safe, ".yaml") {
		safe += ".yaml"
	}

	return safe
}

// Resolve resolves an agent file reference (local file or OCI image) to a local file path
func Resolve(ctx context.Context, out *cli.Printer, agentFilename string) (string, error) {
	originalOCIRef := agentFilename // Store the original for OCI ref tracking

	// Try to resolve as an alias first
	if aliasStore, err := aliases.Load(); err == nil {
		if resolvedPath, ok := aliasStore.Get(agentFilename); ok {
			slog.Debug("Resolved alias", "alias", agentFilename, "path", resolvedPath)
			agentFilename = resolvedPath
		}
	}

	if IsLocalFile(agentFilename) {
		// Treat as local YAML file: resolve to absolute path so later chdir doesn't break it
		// TODO(rumpl): Why are we checking for newlines here?
		if !strings.Contains(agentFilename, "\n") {
			if abs, err := filepath.Abs(agentFilename); err == nil {
				agentFilename = abs
			}
		}
		if !fileExists(agentFilename) {
			return "", fmt.Errorf("agent file not found: %s", agentFilename)
		}
		return agentFilename, nil
	}

	// Treat as an OCI image reference. Try local store first, otherwise pull then load.
	a, err := FromStore(agentFilename)
	if err != nil {
		out.Println("Pulling agent", agentFilename)
		if _, pullErr := remote.Pull(agentFilename); pullErr != nil {
			return "", fmt.Errorf("failed to pull OCI image %s: %w", agentFilename, pullErr)
		}
		// Retry after pull
		a, err = FromStore(agentFilename)
		if err != nil {
			return "", fmt.Errorf("failed to load agent from store after pull: %w", err)
		}
	}

	filename := OciRefToFilename(originalOCIRef)
	tmpFilename := filepath.Join(os.TempDir(), filename)

	if err := os.WriteFile(tmpFilename, []byte(a), 0o644); err != nil {
		return "", fmt.Errorf("failed to write agent file: %w", err)
	}

	slog.Debug("Resolved OCI reference to file", "oci_ref", originalOCIRef, "file", tmpFilename)

	go func() {
		<-ctx.Done()
		os.Remove(tmpFilename)
		slog.Debug("Cleaned up OCI reference file", "file", tmpFilename)
	}()

	return tmpFilename, nil
}

// fileExists checks if a file exists at the given path
func fileExists(path string) bool {
	_, err := os.Stat(path)
	exists := err == nil
	return exists
}

// FromStore loads an agent configuration from the OCI content store
func FromStore(reference string) (string, error) {
	store, err := content.NewStore()
	if err != nil {
		return "", err
	}

	img, err := store.GetArtifactImage(reference)
	if err != nil {
		return "", err
	}

	layers, err := img.Layers()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	layer := layers[0]
	b, err := layer.Uncompressed()
	if err != nil {
		return "", err
	}

	_, err = io.Copy(&buf, b)
	if err != nil {
		return "", err
	}
	b.Close()

	return buf.String(), nil
}
