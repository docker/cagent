package oca

import "testing"

func TestDefaultIDCSConfig(t *testing.T) {
	cfg := DefaultIDCSConfig()
	p := cfg.ActiveProfile()

	if p.ClientID == "" {
		t.Error("ClientID should not be empty")
	}
	if p.IDCSBaseURL == "" {
		t.Error("IDCSBaseURL should not be empty")
	}
	if p.AuthEndpoint == "" {
		t.Error("AuthEndpoint should not be empty")
	}
	if p.TokenEndpoint == "" {
		t.Error("TokenEndpoint should not be empty")
	}
	if p.DeviceEndpoint == "" {
		t.Error("DeviceEndpoint should not be empty")
	}
	if p.LiteLLMBaseURL == "" {
		t.Error("LiteLLMBaseURL should not be empty")
	}
	if p.Scope == "" {
		t.Error("Scope should not be empty")
	}
	if len(cfg.CallbackPorts) == 0 {
		t.Error("CallbackPorts should not be empty")
	}
}

func TestDefaultIDCSConfig_DefaultsToInternal(t *testing.T) {
	cfg := DefaultIDCSConfig()
	if cfg.Mode != ModeInternal {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeInternal)
	}
	p := cfg.ActiveProfile()
	if p.ClientID != "a8331954c0cf48ba99b5dd223a14c6ea" {
		t.Errorf("ClientID = %q, want internal default", p.ClientID)
	}
}

func TestDefaultIDCSConfig_ExternalMode(t *testing.T) {
	t.Setenv(EnvMode, ModeExternal)
	cfg := DefaultIDCSConfig()
	if cfg.Mode != ModeExternal {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeExternal)
	}
	p := cfg.ActiveProfile()
	if p.ClientID != "c1aba3deed5740659981a752714eba33" {
		t.Errorf("ClientID = %q, want external default", p.ClientID)
	}
	if p.IDCSBaseURL != "https://login-ext.identity.oraclecloud.com" {
		t.Errorf("IDCSBaseURL = %q, want external default", p.IDCSBaseURL)
	}
}

func TestDefaultIDCSConfig_BothProfiles(t *testing.T) {
	cfg := DefaultIDCSConfig()

	// Internal profile
	if cfg.Internal.ClientID != "a8331954c0cf48ba99b5dd223a14c6ea" {
		t.Errorf("Internal.ClientID = %q, want internal default", cfg.Internal.ClientID)
	}
	if cfg.Internal.IDCSBaseURL != "https://idcs-9dc693e80d9b469480d7afe00e743931.identity.oraclecloud.com" {
		t.Errorf("Internal.IDCSBaseURL = %q, want internal default", cfg.Internal.IDCSBaseURL)
	}

	// External profile
	if cfg.External.ClientID != "c1aba3deed5740659981a752714eba33" {
		t.Errorf("External.ClientID = %q, want external default", cfg.External.ClientID)
	}
	if cfg.External.IDCSBaseURL != "https://login-ext.identity.oraclecloud.com" {
		t.Errorf("External.IDCSBaseURL = %q, want external default", cfg.External.IDCSBaseURL)
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
			name:   "OCA_CLIENT_ID overrides active profile client ID",
			envVar: EnvClientID,
			value:  "env-client-id",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.ClientID != "env-client-id" {
					t.Errorf("ClientID = %q, want %q", p.ClientID, "env-client-id")
				}
			},
		},
		{
			name:   "OCA_IDCS_URL overrides active profile IDCS base and endpoints",
			envVar: EnvIDCSURL,
			value:  "https://env-idcs.example.com",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.IDCSBaseURL != "https://env-idcs.example.com" {
					t.Errorf("IDCSBaseURL = %q, want env value", p.IDCSBaseURL)
				}
				if p.AuthEndpoint != "https://env-idcs.example.com/oauth2/v1/authorize" {
					t.Errorf("AuthEndpoint = %q, want derived from env", p.AuthEndpoint)
				}
			},
		},
		{
			name:   "OCA_ENDPOINT overrides active profile litellm URL",
			envVar: EnvEndpoint,
			value:  "https://env-litellm.example.com/",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.LiteLLMBaseURL != "https://env-litellm.example.com/" {
					t.Errorf("LiteLLMBaseURL = %q, want env value", p.LiteLLMBaseURL)
				}
			},
		},
		{
			name:   "OCA_SCOPE overrides active profile scope",
			envVar: EnvScope,
			value:  "custom_scope",
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.Scope != "custom_scope" {
					t.Errorf("Scope = %q, want %q", p.Scope, "custom_scope")
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
				p := cfg.ActiveProfile()
				dp := def.ActiveProfile()
				if p.ClientID != dp.ClientID {
					t.Errorf("ClientID = %q, want %q", p.ClientID, dp.ClientID)
				}
			},
		},
		{
			name: "override client_id",
			opts: map[string]any{"client_id": "custom-id"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.ClientID != "custom-id" {
					t.Errorf("ClientID = %q, want %q", p.ClientID, "custom-id")
				}
			},
		},
		{
			name: "override idcs_base_url rebuilds endpoints",
			opts: map[string]any{"idcs_base_url": "https://custom.example.com"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.IDCSBaseURL != "https://custom.example.com" {
					t.Errorf("IDCSBaseURL = %q, want custom", p.IDCSBaseURL)
				}
				if p.AuthEndpoint != "https://custom.example.com/oauth2/v1/authorize" {
					t.Errorf("AuthEndpoint = %q, want custom", p.AuthEndpoint)
				}
				if p.TokenEndpoint != "https://custom.example.com/oauth2/v1/token" {
					t.Errorf("TokenEndpoint = %q, want custom", p.TokenEndpoint)
				}
				if p.DeviceEndpoint != "https://custom.example.com/oauth2/v1/device" {
					t.Errorf("DeviceEndpoint = %q, want custom", p.DeviceEndpoint)
				}
			},
		},
		{
			name: "override litellm_base_url",
			opts: map[string]any{"litellm_base_url": "https://custom-llm.example.com/"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.LiteLLMBaseURL != "https://custom-llm.example.com/" {
					t.Errorf("LiteLLMBaseURL = %q, want custom", p.LiteLLMBaseURL)
				}
			},
		},
		{
			name: "provider_opts override env vars",
			opts: map[string]any{"client_id": "opts-id"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				p := cfg.ActiveProfile()
				if p.ClientID != "opts-id" {
					t.Errorf("ClientID = %q, want %q", p.ClientID, "opts-id")
				}
			},
		},
		{
			name: "mode from provider_opts selects external profile",
			opts: map[string]any{"mode": "external"},
			checkFn: func(t *testing.T, cfg IDCSConfig) {
				if cfg.Mode != ModeExternal {
					t.Errorf("Mode = %q, want %q", cfg.Mode, ModeExternal)
				}
				p := cfg.ActiveProfile()
				if p.ClientID != "c1aba3deed5740659981a752714eba33" {
					t.Errorf("ClientID = %q, want external default", p.ClientID)
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
		"EnvMode":        EnvMode,
	}
	for name, val := range constants {
		if val == "" {
			t.Errorf("%s should not be empty", name)
		}
	}
}

func TestActiveProfile(t *testing.T) {
	cfg := DefaultIDCSConfig()

	// Internal by default
	cfg.Mode = ModeInternal
	p := cfg.ActiveProfile()
	if p.ClientID != cfg.Internal.ClientID {
		t.Errorf("ActiveProfile() returned external profile for internal mode")
	}

	// External
	cfg.Mode = ModeExternal
	p = cfg.ActiveProfile()
	if p.ClientID != cfg.External.ClientID {
		t.Errorf("ActiveProfile() returned internal profile for external mode")
	}

	// Unknown defaults to internal
	cfg.Mode = "unknown"
	p = cfg.ActiveProfile()
	if p.ClientID != cfg.Internal.ClientID {
		t.Errorf("ActiveProfile() should default to internal for unknown mode")
	}
}
