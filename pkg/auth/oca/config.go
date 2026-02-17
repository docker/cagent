package oca

import "os"

// Environment variable names for OCA configuration.
const (
	EnvAccessToken = "OCA_ACCESS_TOKEN"
	EnvClientID    = "OCA_CLIENT_ID"
	EnvIDCSURL     = "OCA_IDCS_URL"
	EnvEndpoint    = "OCA_ENDPOINT"
	EnvScope       = "OCA_SCOPE"
	EnvAuthFlow    = "OCA_AUTH_FLOW"
	EnvMode        = "OCA_MODE"
)

// ModeInternal is the mode for Oracle employees.
const ModeInternal = "internal"

// ModeExternal is the mode for non-Oracle (external) users.
const ModeExternal = "external"

// IDCSProfile holds OAuth2 settings for a single authentication profile.
type IDCSProfile struct {
	ClientID       string
	IDCSBaseURL    string
	AuthEndpoint   string
	TokenEndpoint  string
	DeviceEndpoint string
	LiteLLMBaseURL string
	Scope          string
}

// IDCSConfig holds Oracle IDCS OAuth2 configuration with dual profiles.
type IDCSConfig struct {
	Internal      IDCSProfile
	External      IDCSProfile
	Mode          string
	CallbackPorts []int
}

// ActiveProfile returns the profile matching the current mode.
// Defaults to internal if mode is empty or unrecognized.
func (c *IDCSConfig) ActiveProfile() *IDCSProfile {
	if c.Mode == ModeExternal {
		return &c.External
	}
	return &c.Internal
}

// DefaultInternalProfile returns the default internal (Oracle employee) profile.
func DefaultInternalProfile() IDCSProfile {
	base := "https://idcs-9dc693e80d9b469480d7afe00e743931.identity.oraclecloud.com"
	return IDCSProfile{
		ClientID:       "a8331954c0cf48ba99b5dd223a14c6ea",
		IDCSBaseURL:    base,
		AuthEndpoint:   base + "/oauth2/v1/authorize",
		TokenEndpoint:  base + "/oauth2/v1/token",
		DeviceEndpoint: base + "/oauth2/v1/device",
		LiteLLMBaseURL: "https://code-internal.aiservice.us-chicago-1.oci.oraclecloud.com/20250206/app/litellm/",
		Scope:          "openid offline_access",
	}
}

// DefaultExternalProfile returns the default external (non-Oracle) profile.
func DefaultExternalProfile() IDCSProfile {
	base := "https://login-ext.identity.oraclecloud.com"
	return IDCSProfile{
		ClientID:       "c1aba3deed5740659981a752714eba33",
		IDCSBaseURL:    base,
		AuthEndpoint:   base + "/oauth2/v1/authorize",
		TokenEndpoint:  base + "/oauth2/v1/token",
		DeviceEndpoint: base + "/oauth2/v1/device",
		LiteLLMBaseURL: "https://code.aiservice.us-chicago-1.oci.oraclecloud.com/20250206/app/litellm/",
		Scope:          "openid offline_access",
	}
}

// DefaultIDCSConfig returns the default IDCS configuration with both profiles.
// Environment variables override the hardcoded defaults for the active profile.
func DefaultIDCSConfig() IDCSConfig {
	mode := ModeInternal
	if v := os.Getenv(EnvMode); v == ModeExternal {
		mode = ModeExternal
	}

	cfg := IDCSConfig{
		Internal:      DefaultInternalProfile(),
		External:      DefaultExternalProfile(),
		Mode:          mode,
		CallbackPorts: []int{8669, 8668, 8667},
	}

	// Environment variables override the active profile
	applyEnvOverrides(&cfg)

	return cfg
}

// applyEnvOverrides applies environment variable overrides to the active profile.
func applyEnvOverrides(cfg *IDCSConfig) {
	p := cfg.ActiveProfile()
	if v := os.Getenv(EnvClientID); v != "" {
		p.ClientID = v
	}
	if v := os.Getenv(EnvIDCSURL); v != "" {
		setProfileBaseURL(p, v)
	}
	if v := os.Getenv(EnvEndpoint); v != "" {
		p.LiteLLMBaseURL = v
	}
	if v := os.Getenv(EnvScope); v != "" {
		p.Scope = v
	}
}

// setProfileBaseURL sets the IDCS base URL and derives all endpoint URLs from it.
func setProfileBaseURL(p *IDCSProfile, baseURL string) {
	p.IDCSBaseURL = baseURL
	p.AuthEndpoint = baseURL + "/oauth2/v1/authorize"
	p.TokenEndpoint = baseURL + "/oauth2/v1/token"
	p.DeviceEndpoint = baseURL + "/oauth2/v1/device"
}

// ConfigFromProviderOpts builds an IDCSConfig from provider_opts, falling back to defaults.
// Precedence: provider_opts > env vars > hardcoded defaults.
func ConfigFromProviderOpts(opts map[string]any) IDCSConfig {
	cfg := DefaultIDCSConfig() // already has env var overrides

	// Mode from provider_opts overrides env
	if v, ok := opts["mode"].(string); ok && (v == ModeInternal || v == ModeExternal) {
		cfg.Mode = v
	}

	p := cfg.ActiveProfile()

	if v, ok := opts["client_id"].(string); ok && v != "" {
		p.ClientID = v
	}
	if v, ok := opts["idcs_base_url"].(string); ok && v != "" {
		setProfileBaseURL(p, v)
	}
	if v, ok := opts["litellm_base_url"].(string); ok && v != "" {
		p.LiteLLMBaseURL = v
	}
	if v, ok := opts["scope"].(string); ok && v != "" {
		p.Scope = v
	}

	return cfg
}
