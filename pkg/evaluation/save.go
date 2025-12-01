package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/session"
)

func Save(sess *session.Session, filename ...string) (string, error) {
	if err := os.MkdirAll("evals", 0o755); err != nil {
		return "", err
	}

	var baseFilename string
	if len(filename) > 0 && filename[0] != "" {
		// Use the provided filename
		baseFilename = filename[0]
		// Ensure .json extension
		if !strings.HasSuffix(baseFilename, ".json") {
			baseFilename += ".json"
		}
	} else {
		// Default to session ID
		baseFilename = fmt.Sprintf("%s.json", sess.ID)
	}

	evalFile := filepath.Join("evals", baseFilename)
	for number := 1; ; number++ {
		if _, err := os.Stat(evalFile); err != nil {
			break
		}

		// Add suffix before .json extension
		nameWithoutExt := strings.TrimSuffix(baseFilename, ".json")
		evalFile = filepath.Join("evals", fmt.Sprintf("%s_%d.json", nameWithoutExt, number))
	}

	file, err := os.Create(evalFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return evalFile, encoder.Encode(sess)
}
