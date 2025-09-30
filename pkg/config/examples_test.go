package config

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExamples(t *testing.T) {
	var files []string
	err := filepath.WalkDir("../../examples", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".yaml" {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			cfg, err := loadConfig(file)

			require.NoError(t, err)
			require.Equal(t, "2", cfg.Version, "Version should be 2 in %s", file)
			require.NotEmpty(t, cfg.Agents["root"].Description, "Description should not be empty in %s", file)
			require.NotEmpty(t, cfg.Agents["root"].Instruction, "Instruction should not be empty in %s", file)
		})
	}
}
