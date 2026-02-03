package root

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/userconfig"
)

func TestGatewayLogic(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		args       []string
		userConfig *userconfig.Config
		expected   string
	}{
		{
			name:     "env",
			env:      "https://models.example.com",
			expected: "https://models.example.com",
		},
		{
			name:     "cli",
			args:     []string{"--models-gateway", "https://cli-models.example.com"},
			expected: "https://cli-models.example.com",
		},
		{
			name:     "cli_overrides_env",
			env:      "https://env-models.example.com",
			args:     []string{"--models-gateway", "https://cli-models.example.com"},
			expected: "https://cli-models.example.com",
		},
		{
			name:       "user_config",
			userConfig: &userconfig.Config{ModelsGateway: "https://userconfig-models.example.com"},
			expected:   "https://userconfig-models.example.com",
		},
		{
			name:       "env_overrides_user_config",
			env:        "https://env-models.example.com",
			userConfig: &userconfig.Config{ModelsGateway: "https://userconfig-models.example.com"},
			expected:   "https://env-models.example.com",
		},
		{
			name:       "cli_overrides_user_config",
			args:       []string{"--models-gateway", "https://cli-models.example.com"},
			userConfig: &userconfig.Config{ModelsGateway: "https://userconfig-models.example.com"},
			expected:   "https://cli-models.example.com",
		},
		{
			name:       "cli_overrides_env_and_user_config",
			env:        "https://env-models.example.com",
			args:       []string{"--models-gateway", "https://cli-models.example.com"},
			userConfig: &userconfig.Config{ModelsGateway: "https://userconfig-models.example.com"},
			expected:   "https://cli-models.example.com",
		},
		{
			name:       "user_config_with_trailing_slash",
			userConfig: &userconfig.Config{ModelsGateway: "https://userconfig-models.example.com/"},
			expected:   "https://userconfig-models.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CAGENT_MODELS_GATEWAY", tt.env)

			// Mock user config loader
			original := loadUserConfig
			loadUserConfig = func() (*userconfig.Config, error) {
				if tt.userConfig != nil {
					return tt.userConfig, nil
				}
				return &userconfig.Config{}, nil
			}
			t.Cleanup(func() { loadUserConfig = original })

			cmd := &cobra.Command{
				RunE: func(*cobra.Command, []string) error {
					return nil
				},
			}
			runConfig := config.RuntimeConfig{}
			addGatewayFlags(cmd, &runConfig)

			cmd.SetArgs(tt.args)
			err := cmd.Execute()

			require.NoError(t, err)
			assert.Equal(t, tt.expected, runConfig.ModelsGateway)
		})
	}
}

func TestCanonize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "trailing_slash",
			input:    "https://example.com/",
			expected: "https://example.com",
		},
		{
			name:     "leading_and_trailing_whitespace",
			input:    " https://example.com ",
			expected: "https://example.com",
		},
		{
			name:     "trailing_slash_and_whitespace",
			input:    " https://example.com/ ",
			expected: "https://example.com",
		},
		{
			name:     "no_trailing_slash",
			input:    "https://example.com",
			expected: "https://example.com",
		},
		{
			name:     "path_with_trailing_slash",
			input:    "https://example.com/path/",
			expected: "https://example.com/path",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_whitespace",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := canonize(tt.input)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTaskListLogic(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		args     []string
		expected string
	}{
		{
			name:     "empty_by_default",
			expected: "",
		},
		{
			name:     "from_env",
			env:      "my-project",
			expected: "my-project",
		},
		{
			name:     "from_cli",
			args:     []string{"--task-list", "cli-project"},
			expected: "cli-project",
		},
		{
			name:     "cli_overrides_env",
			env:      "env-project",
			args:     []string{"--task-list", "cli-project"},
			expected: "cli-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CAGENT_TASK_LIST_ID", tt.env)

			cmd := &cobra.Command{
				RunE: func(*cobra.Command, []string) error {
					return nil
				},
			}
			runConfig := config.RuntimeConfig{}
			addTaskListFlags(cmd, &runConfig)

			cmd.SetArgs(tt.args)
			err := cmd.Execute()

			require.NoError(t, err)
			assert.Equal(t, tt.expected, runConfig.TaskListID)
		})
	}
}
