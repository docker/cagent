package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/config/latest"
)

func TestClone_DefaultModelDeepCopy(t *testing.T) {
	temp := 0.7
	original := &RuntimeConfig{
		Config: Config{
			DefaultModel: &latest.ModelConfig{
				Provider:    "openai",
				Model:       "gpt-4o",
				Temperature: &temp,
			},
			WorkingDir: "/original",
		},
	}

	clone := original.Clone()

	// Mutate the original's DefaultModel
	*original.DefaultModel.Temperature = 0.9
	original.DefaultModel.Model = "gpt-4o-mini"

	// Clone must not be affected by mutations to the original
	assert.InDelta(t, 0.7, *clone.DefaultModel.Temperature, 0.001)
	assert.Equal(t, "gpt-4o", clone.DefaultModel.Model)
}

func TestClone_NilDefaultModel(t *testing.T) {
	original := &RuntimeConfig{
		Config: Config{
			DefaultModel: nil,
			WorkingDir:   "/app",
		},
	}

	clone := original.Clone()

	assert.Nil(t, clone.DefaultModel)
	assert.Equal(t, "/app", clone.WorkingDir)
}

func TestClone_EnvFilesIsolated(t *testing.T) {
	original := &RuntimeConfig{
		Config: Config{
			EnvFiles: []string{"a.env", "b.env"},
		},
	}

	clone := original.Clone()
	clone.EnvFiles = append(clone.EnvFiles, "c.env")

	assert.Len(t, original.EnvFiles, 2, "original must not be modified when clone is mutated")
	assert.Len(t, clone.EnvFiles, 3)
}
