package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/modelsdev"
)

func collectExamples(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(filepath.Join("..", "..", "examples"), func(path string, d fs.DirEntry, err error) error {
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

	return files
}

func TestParseExamples(t *testing.T) {
	modelsStore, err := modelsdev.NewStore()
	require.NoError(t, err)

	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(t.Context(), testfileSource(file))

			require.NoError(t, err)
			require.Equal(t, latest.Version, cfg.Version, "Version should be %d in %s", latest.Version, file)
			require.NotEmpty(t, cfg.Agents["root"].Description, "Description should not be empty in %s", file)
			require.NotEmpty(t, cfg.Agents["root"].Instruction, "Instruction should not be empty in %s", file)

			for _, agent := range cfg.Agents {
				require.NotEmpty(t, agent.Model)
			}

			for _, model := range cfg.Models {
				require.NotEmpty(t, model.Provider)
				require.NotEmpty(t, model.Model)
				if model.Provider == "dmr" || model.Provider == "bedrock" {
					continue
				}

				model, err := modelsStore.GetModel(t.Context(), model.Provider+"/"+model.Model)
				require.NoError(t, err)
				require.NotNil(t, model)
			}
		})
	}
}

func TestJsonSchemaWorksForExamples(t *testing.T) {
	// Read json schema.
	schemaFile, err := os.ReadFile(filepath.Join("..", "..", "cagent-schema.json"))
	require.NoError(t, err)

	schema, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(schemaFile))
	require.NoError(t, err)

	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			buf, err := os.ReadFile(file)
			require.NoError(t, err)

			var rawJSON any
			err = yaml.Unmarshal(buf, &rawJSON)
			require.NoError(t, err)

			result, err := schema.Validate(gojsonschema.NewRawLoader(rawJSON))
			require.NoError(t, err)
			assert.True(t, result.Valid(), "Example %s does not match schema: %v", file, result.Errors())
		})
	}
}
