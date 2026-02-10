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

func TestConfigFromProviderOpts(t *testing.T) {
	tests := []struct {
		name     string
		opts     map[string]any
		checkFn  func(t *testing.T, cfg IDCSConfig)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConfigFromProviderOpts(tt.opts)
			tt.checkFn(t, cfg)
		})
	}
}
