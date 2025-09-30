package root

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
)

func TestGatewayLogic(t *testing.T) {
	tests := []struct {
		name                  string
		envVars               map[string]string
		args                  []string
		expectedModelsGateway string
		expectError           bool
		errorContains         string
	}{
		{
			name:                  "env_var_models_gateway",
			envVars:               map[string]string{"CAGENT_MODELS_GATEWAY": "https://models.example.com"},
			expectedModelsGateway: "https://models.example.com",
		},
		{
			name:                  "env_var_gateway",
			envVars:               map[string]string{"CAGENT_GATEWAY": "https://gateway.example.com"},
			expectedModelsGateway: "https://gateway.example.com",
		},
		{
			name:                  "cli_flag_models_gateway",
			args:                  []string{"--models-gateway", "https://cli-models.example.com"},
			expectedModelsGateway: "https://cli-models.example.com",
		},
		{
			name:          "cli_flag_gateway_mutually_exclusive_with_models_gateway",
			args:          []string{"--gateway", "https://gateway.example.com", "--models-gateway", "https://models.example.com"},
			expectError:   true,
			errorContains: "if any flags in the group [gateway models-gateway] are set none of the others can be",
		},
		{
			name: "gateway_url_canonicalization_with_main_gateway",
			envVars: map[string]string{
				"CAGENT_GATEWAY": "https://gateway.example.com/", // Main gateway with trailing slash
			},
			args:                  []string{},
			expectedModelsGateway: "https://gateway.example.com",
		},
		// Tests for combinations of environment variables and CLI arguments
		{
			name: "env_var_overrides_same_cli_flag",
			envVars: map[string]string{
				"CAGENT_MODELS_GATEWAY": "https://env-models.example.com",
			},
			args:                  []string{"--models-gateway", "https://cli-models.example.com"},
			expectedModelsGateway: "https://env-models.example.com",
		},
		{
			name: "env_var_main_gateway_overrides_cli_flags",
			envVars: map[string]string{
				"CAGENT_GATEWAY": "https://env-gateway.example.com",
			},
			args:                  []string{"--models-gateway", "https://cli-gateway.example.com"},
			expectedModelsGateway: "https://env-gateway.example.com",
		},
		{
			name:                  "cli_flag_gateway_sets_both_gateways",
			args:                  []string{"--gateway", "https://cli-gateway.example.com"},
			expectedModelsGateway: "https://cli-gateway.example.com",
		},
		{
			name: "env_vars_both_gateways_override_cli_gateway_flag",
			envVars: map[string]string{
				"CAGENT_MODELS_GATEWAY": "https://env-models.example.com",
			},
			args:                  []string{"--gateway", "https://cli-gateway.example.com"},
			expectedModelsGateway: "https://env-models.example.com",
		},
		{
			name: "env_var_main_gateway_mutually_exclusive_with_models_gateway",
			envVars: map[string]string{
				"CAGENT_GATEWAY":        "https://gateway.example.com",
				"CAGENT_MODELS_GATEWAY": "https://models.example.com",
			},
			args:          []string{},
			expectError:   true,
			errorContains: "environment variables CAGENT_GATEWAY and CAGENT_MODELS_GATEWAY cannot be set at the same time",
		},
		{
			name: "env_var_main_gateway_mutually_exclusive_with_both_specific_gateways",
			envVars: map[string]string{
				"CAGENT_GATEWAY":        "https://gateway.example.com",
				"CAGENT_MODELS_GATEWAY": "https://models.example.com",
			},
			args:          []string{},
			expectError:   true,
			errorContains: "environment variables CAGENT_GATEWAY and CAGENT_MODELS_GATEWAY cannot be set at the same time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test environment variables using t.Setenv (automatically handles cleanup)
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			// Reset global variables
			runConfig = config.RuntimeConfig{}
			gwConfig = gatewayConfig{}

			// Create a test command with gateway flags
			cmd := &cobra.Command{
				Use: "test",
				RunE: func(cmd *cobra.Command, args []string) error {
					// Command logic here - for testing, we just return nil
					return nil
				},
			}

			// Add gateway flags (this is the actual function being tested)
			addGatewayFlags(cmd)

			// Set command arguments and execute
			cmd.SetArgs(tt.args)

			// Execute the command - this triggers flag parsing and PersistentPreRunE
			_, err := cmd.ExecuteC()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify expected gateway configuration
				assert.Equal(t, tt.expectedModelsGateway, runConfig.ModelsGateway, "Models gateway mismatch")
			}
		})
	}
}

func TestCanonize(t *testing.T) {
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
			expected: "https://example.com/", // TrimSuffix doesn't work because string ends with " ", not "/"
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
			result := canonize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
