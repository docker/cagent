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
)

// IDCSConfig holds Oracle IDCS OAuth2 configuration.
type IDCSConfig struct {
	ClientID       string
	IDCSBaseURL    string
	AuthEndpoint   string
	TokenEndpoint  string
	DeviceEndpoint string
	LiteLLMBaseURL string
	Scope          string
	CallbackPorts  []int
}

// DefaultIDCSConfig returns the default IDCS configuration
// using production-validated values from the ocaider reference implementation.
// Environment variables override the hardcoded defaults.
func DefaultIDCSConfig() IDCSConfig {
	base := "https://idcs-9dc693e80d9b469480d7afe00e743931.identity.oraclecloud.com"
	cfg := IDCSConfig{
		ClientID:       "a8331954c0cf48ba99b5dd223a14c6ea",
		IDCSBaseURL:    base,
		AuthEndpoint:   base + "/oauth2/v1/authorize",
		TokenEndpoint:  base + "/oauth2/v1/token",
		DeviceEndpoint: base + "/oauth2/v1/device",
		LiteLLMBaseURL: "https://code-internal.aiservice.us-chicago-1.oci.oraclecloud.com/20250206/app/litellm/",
		Scope:          "openid offline_access",
		CallbackPorts:  []int{8669, 8668, 8667},
	}

	// Environment variables override defaults
	applyEnvOverrides(&cfg)

	return cfg
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *IDCSConfig) {
	if v := os.Getenv(EnvClientID); v != "" {
		cfg.ClientID = v
	}
	if v := os.Getenv(EnvIDCSURL); v != "" {
		setIDCSBaseURL(cfg, v)
	}
	if v := os.Getenv(EnvEndpoint); v != "" {
		cfg.LiteLLMBaseURL = v
	}
	if v := os.Getenv(EnvScope); v != "" {
		cfg.Scope = v
	}
}

// setIDCSBaseURL sets the IDCS base URL and derives all endpoint URLs from it.
func setIDCSBaseURL(cfg *IDCSConfig, baseURL string) {
	cfg.IDCSBaseURL = baseURL
	cfg.AuthEndpoint = baseURL + "/oauth2/v1/authorize"
	cfg.TokenEndpoint = baseURL + "/oauth2/v1/token"
	cfg.DeviceEndpoint = baseURL + "/oauth2/v1/device"
}

// ConfigFromProviderOpts builds an IDCSConfig from provider_opts, falling back to defaults.
// Precedence: provider_opts > env vars > hardcoded defaults.
func ConfigFromProviderOpts(opts map[string]any) IDCSConfig {
	cfg := DefaultIDCSConfig() // already has env var overrides

	if v, ok := opts["client_id"].(string); ok && v != "" {
		cfg.ClientID = v
	}
	if v, ok := opts["idcs_base_url"].(string); ok && v != "" {
		setIDCSBaseURL(&cfg, v)
	}
	if v, ok := opts["litellm_base_url"].(string); ok && v != "" {
		cfg.LiteLLMBaseURL = v
	}
	if v, ok := opts["scope"].(string); ok && v != "" {
		cfg.Scope = v
	}

	return cfg
}
