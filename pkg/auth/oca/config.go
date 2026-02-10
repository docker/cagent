package oca

// IDCSConfig holds Oracle IDCS OAuth2 configuration.
type IDCSConfig struct {
	ClientID      string
	IDCSBaseURL   string
	AuthEndpoint  string
	TokenEndpoint string
	DeviceEndpoint string
	LiteLLMBaseURL string
	Scope         string
	CallbackPorts []int
}

// DefaultIDCSConfig returns the default IDCS configuration
// using production-validated values from the ocaider reference implementation.
func DefaultIDCSConfig() IDCSConfig {
	base := "https://idcs-9dc693e80d9b469480d7afe00e743931.identity.oraclecloud.com"
	return IDCSConfig{
		ClientID:       "a8331954c0cf48ba99b5dd223a14c6ea",
		IDCSBaseURL:    base,
		AuthEndpoint:   base + "/oauth2/v1/authorize",
		TokenEndpoint:  base + "/oauth2/v1/token",
		DeviceEndpoint: base + "/oauth2/v1/device",
		LiteLLMBaseURL: "https://code-internal.aiservice.us-chicago-1.oci.oraclecloud.com/20250206/app/litellm/",
		Scope:          "openid offline_access",
		CallbackPorts:  []int{8669, 8668, 8667},
	}
}

// ConfigFromProviderOpts builds an IDCSConfig from provider_opts, falling back to defaults.
func ConfigFromProviderOpts(opts map[string]any) IDCSConfig {
	cfg := DefaultIDCSConfig()

	if v, ok := opts["client_id"].(string); ok && v != "" {
		cfg.ClientID = v
	}
	if v, ok := opts["idcs_base_url"].(string); ok && v != "" {
		cfg.IDCSBaseURL = v
		cfg.AuthEndpoint = v + "/oauth2/v1/authorize"
		cfg.TokenEndpoint = v + "/oauth2/v1/token"
		cfg.DeviceEndpoint = v + "/oauth2/v1/device"
	}
	if v, ok := opts["litellm_base_url"].(string); ok && v != "" {
		cfg.LiteLLMBaseURL = v
	}
	if v, ok := opts["scope"].(string); ok && v != "" {
		cfg.Scope = v
	}

	return cfg
}
