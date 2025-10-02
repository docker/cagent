package session

import (
	"os"
	"path/filepath"
)

func readPromptFile(workDir, filename string) (string, error) {
	current, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(current, filename)

		info, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
		} else if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}
