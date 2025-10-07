package teamloader

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/environment"
)

type noEnvProvider struct{}

func (p *noEnvProvider) Get(context.Context, string) string { return "" }

func TestCheckRequiredEnvVars(t *testing.T) {
	tests := []struct {
		yaml            string
		expectedMissing []string
	}{
		{
			yaml:            "openai_inline.yaml",
			expectedMissing: []string{"OPENAI_API_KEY"},
		},
		{
			yaml:            "anthropic_inline.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY"},
		},
		{
			yaml:            "google_inline.yaml",
			expectedMissing: []string{"GOOGLE_API_KEY"},
		},
		{
			yaml:            "dmr_inline.yaml",
			expectedMissing: []string{},
		},
		{
			yaml:            "openai_model.yaml",
			expectedMissing: []string{"OPENAI_API_KEY"},
		},
		{
			yaml:            "anthropic_model.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY"},
		},
		{
			yaml:            "google_model.yaml",
			expectedMissing: []string{"GOOGLE_API_KEY"},
		},
		{
			yaml:            "dmr_model.yaml",
			expectedMissing: []string{},
		},
		{
			yaml:            "all.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"},
		},
	}
	for _, test := range tests {
		t.Run(test.yaml, func(t *testing.T) {
			t.Parallel()

			root := openRoot(t, "testdata")

			cfg, err := config.LoadConfig(test.yaml, root)
			require.NoError(t, err)

			err = checkRequiredEnvVars(t.Context(), cfg, &noEnvProvider{}, config.RuntimeConfig{})

			if len(test.expectedMissing) == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, test.expectedMissing, err.(*environment.RequiredEnvError).Missing)
			}
		})
	}
}

func TestCheckRequiredEnvVarsWithModelGateway(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := config.LoadConfig("all.yaml", root)
	require.NoError(t, err)

	err = checkRequiredEnvVars(t.Context(), cfg, &noEnvProvider{}, config.RuntimeConfig{
		ModelsGateway: "gateway:8080",
	})

	require.NoError(t, err)
}

func TestLoadExamples(t *testing.T) {
	// Collect the missing env vars.
	missingEnvs := map[string]bool{}

	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			_, err := Load(t.Context(), file, config.RuntimeConfig{})
			if err != nil {
				envErr := &environment.RequiredEnvError{}
				require.ErrorAs(t, err, &envErr)

				for _, env := range envErr.Missing {
					missingEnvs[env] = true
				}
			}
		})
	}

	for name := range missingEnvs {
		t.Setenv(name, "dummy")
	}

	// Load all the examples.
	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			teams, err := Load(t.Context(), file, config.RuntimeConfig{})
			require.NoError(t, err)
			require.NotEmpty(t, teams)
		})
	}
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })

	return root
}

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
