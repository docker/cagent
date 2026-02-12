package oca

import "testing"

func TestDefaultIDCSConfig(t *testing.T) {
	cfg := DefaultIDCSConfig()

	if cfg.ClientID == "" {
		t.Error("ClientID should not be empty")
	}
	if cfg.IDCSBaseURL == "" {
		t.Error("IDCSBaseURL should not be empty")
	}
	if cfg.AuthEndpoint == "" {
		t.Error("AuthEndpoint should not be empty")
	}
	if cfg.TokenEndpoint == "" {
		t.Error("TokenEndpoint should not be empty")
	}
	if cfg.DeviceEndpoint == "" {
		t.Error("DeviceEndpoint should not be empty")
	}
	if cfg.LiteLLMBaseURL == "" {
		t.Error("LiteLLMBaseURL should not be empty")
	}
	if cfg.Scope == "" {
		t.Error("Scope should not be empty")
	}
	if len(cfg.CallbackPorts) == 0 {
		t.Error("CallbackPorts should not be empty")
	}
}

func TestDefaultIDCSConfig_EnvVarOverrides(t *testing.T) {
	tests := []struct {
		name    string
		envVar  string
		value   string
		checkFn func(t *testing.T, cfg IDCSConfig)
	}{
		{
			name:   "OCA_CLIENT_ID overrides client ID",
			envVar: EnvClientID,
			value:  "env-client-id",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.ClientID != "env-client-id" {
					t.Errorf("ClientID = %q, want %q", cfg.ClientID, "env-client-id")
				}
			},
		},
		{
			name:   "OCA_IDCS_URL overrides IDCS base and endpoints",
			envVar: EnvIDCSURL,
			value:  "https://env-idcs.example.com",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.IDCSBaseURL != "https://env-idcs.example.com" {
					t.Errorf("IDCSBaseURL = %q, want env value", cfg.IDCSBaseURL)
				}
				if cfg.AuthEndpoint != "https://env-idcs.example.com/oauth2/v1/authorize" {
					t.Errorf("AuthEndpoint = %q, want derived from env", cfg.AuthEndpoint)
				}
			},
		},
		{
			name:   "OCA_ENDPOINT overrides litellm URL",
			envVar: EnvEndpoint,
			value:  "https://env-litellm.example.com/",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.LiteLLMBaseURL != "https://env-litellm.example.com/" {
					t.Errorf("LiteLLMBaseURL = %q, want env value", cfg.LiteLLMBaseURL)
				}
			},
		},
		{
			name:   "OCA_SCOPE overrides scope",
			envVar: EnvScope,
			value:  "custom_scope",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.Scope != "custom_scope" {
					t.Errorf("Scope = %q, want %q", cfg.Scope, "custom_scope")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.value)
			cfg := DefaultIDCSConfig()
			tt.checkFn(t, cfg)
		})
	}
}

func TestConfigFromProviderOpts(t *testing.T) {
	tests := []struct {
		name    string
		opts    map[string]any
		checkFn func(t *testing.T, cfg IDCSConfig)
	}{
		{
			name: "empty opts uses defaults",
			opts: map[string]any{},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				def := DefaultIDCSConfig()
				if cfg.ClientID != def.ClientID {
					t.Errorf("ClientID = %q, want %q", cfg.ClientID, def.ClientID)
				}
			},
		},
		{
			name: "override client_id",
			opts: map[string]any{"client_id": "custom-id"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.ClientID != "custom-id" {
					t.Errorf("ClientID = %q, want %q", cfg.ClientID, "custom-id")
				}
			},
		},
		{
			name: "override idcs_base_url rebuilds endpoints",
			opts: map[string]any{"idcs_base_url": "https://custom.example.com"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.IDCSBaseURL != "https://custom.example.com" {
					t.Errorf("IDCSBaseURL = %q, want custom", cfg.IDCSBaseURL)
				}
				if cfg.AuthEndpoint != "https://custom.example.com/oauth2/v1/authorize" {
					t.Errorf("AuthEndpoint = %q, want custom", cfg.AuthEndpoint)
				}
				if cfg.TokenEndpoint != "https://custom.example.com/oauth2/v1/token" {
					t.Errorf("TokenEndpoint = %q, want custom", cfg.TokenEndpoint)
				}
				if cfg.DeviceEndpoint != "https://custom.example.com/oauth2/v1/device" {
					t.Errorf("DeviceEndpoint = %q, want custom", cfg.DeviceEndpoint)
				}
			},
		},
		{
			name: "override litellm_base_url",
			opts: map[string]any{"litellm_base_url": "https://custom-llm.example.com/"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.LiteLLMBaseURL != "https://custom-llm.example.com/" {
					t.Errorf("LiteLLMBaseURL = %q, want custom", cfg.LiteLLMBaseURL)
				}
			},
		},
		{
			name: "provider_opts override env vars",
			opts: map[string]any{"client_id": "opts-id"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.ClientID != "opts-id" {
					t.Errorf("ClientID = %q, want %q", cfg.ClientID, "opts-id")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConfigFromProviderOpts(tt.opts)
			tt.checkFn(t, cfg)
		})
	}
}

func TestEnvVarConstants(t *testing.T) {
	// Verify all env var constants are defined and non-empty
	constants := map[string]string{
		"EnvAccessToken": EnvAccessToken,
		"EnvClientID":    EnvClientID,
		"EnvIDCSURL":     EnvIDCSURL,
		"EnvEndpoint":    EnvEndpoint,
		"EnvScope":       EnvScope,
		"EnvAuthFlow":    EnvAuthFlow,
	}
	for name, val := range constants {
		if val == "" {
			t.Errorf("%s should not be empty", name)
		}
	}
}
