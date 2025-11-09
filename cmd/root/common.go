package root

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func setupWorkingDirectory(workingDir string) error {
	if workingDir == "" {
		return nil
	}

	absWd, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}

	info, err := os.Stat(absWd)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("working directory does not exist or is not a directory: %s", absWd)
	}

	if err := os.Chdir(absWd); err != nil {
		return fmt.Errorf("failed to change working directory: %w", err)
	}

	_ = os.Setenv("PWD", absWd)
	slog.Debug("Working directory set", "path", absWd)

	return nil
}
