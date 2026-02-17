package root

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/auth/oca"
	"github.com/docker/cagent/pkg/telemetry"
)

type loginFlags struct {
	method   string
	mode     string
	clientID string
	idcsURL  string
	endpoint string
	scope    string
}

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "login",
		Short:   "Authenticate with a model provider",
		GroupID: "core",
	}

	cmd.AddCommand(newLoginOCACmd())

	return cmd
}

func newLoginOCACmd() *cobra.Command {
	var flags loginFlags

	cmd := &cobra.Command{
		Use:   "oca",
		Short: "Authenticate with Oracle Code Assist (OCA)",
		Long: `Authenticate with Oracle Code Assist using OAuth2.

Modes:
  internal   For Oracle employees (default)
  external   For non-Oracle users

By default, opens a browser for PKCE authentication.
Use --method=headless for environments without a browser (SSH, containers).

All configuration values can also be set via environment variables:
  OCA_MODE         Authentication mode (internal or external)
  OCA_AUTH_FLOW    Authentication method (headless for device code)
  OCA_CLIENT_ID    IDCS client ID
  OCA_IDCS_URL     IDCS base URL
  OCA_ENDPOINT     LiteLLM endpoint URL
  OCA_SCOPE        OAuth2 scope

Precedence: flags > env vars > defaults`,
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("login oca", args)
			return runLoginOCA(cmd.Context(), cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.mode, "mode", "", "Authentication mode: internal (Oracle employees) or external (env: OCA_MODE)")
	cmd.Flags().StringVar(&flags.method, "method", "", "Authentication method: browser (PKCE) or headless (device code)")
	cmd.Flags().StringVar(&flags.clientID, "client-id", "", "IDCS client ID (env: OCA_CLIENT_ID)")
	cmd.Flags().StringVar(&flags.idcsURL, "idcs-url", "", "IDCS base URL (env: OCA_IDCS_URL)")
	cmd.Flags().StringVar(&flags.endpoint, "endpoint", "", "LiteLLM endpoint URL (env: OCA_ENDPOINT)")
	cmd.Flags().StringVar(&flags.scope, "scope", "", "OAuth2 scope (env: OCA_SCOPE)")

	return cmd
}

func runLoginOCA(ctx context.Context, cmd *cobra.Command, flags loginFlags) error {
	// Start from defaults (which already include env var overrides)
	cfg := oca.DefaultIDCSConfig()

	// CLI --mode flag overrides env var
	if flags.mode != "" {
		if flags.mode != oca.ModeInternal && flags.mode != oca.ModeExternal {
			return fmt.Errorf("unknown mode: %s (use 'internal' or 'external')", flags.mode)
		}
		cfg.Mode = flags.mode
	}

	// CLI flags override env vars and defaults for the active profile
	p := cfg.ActiveProfile()
	if flags.clientID != "" {
		p.ClientID = flags.clientID
	}
	if flags.idcsURL != "" {
		p.IDCSBaseURL = flags.idcsURL
		p.AuthEndpoint = flags.idcsURL + "/oauth2/v1/authorize"
		p.TokenEndpoint = flags.idcsURL + "/oauth2/v1/token"
		p.DeviceEndpoint = flags.idcsURL + "/oauth2/v1/device"
	}
	if flags.endpoint != "" {
		p.LiteLLMBaseURL = flags.endpoint
	}
	if flags.scope != "" {
		p.Scope = flags.scope
	}

	// Resolve auth method: flag > env var > default ("browser")
	method := "browser"
	if envFlow := os.Getenv(oca.EnvAuthFlow); envFlow != "" {
		if envFlow == "headless" || envFlow == "pc" || envFlow == "browser" {
			method = envFlow
		}
	}
	if flags.method != "" {
		method = flags.method
	}
	// Normalize "pc" to "browser" (ocaider compat)
	if method == "pc" {
		method = "browser"
	}

	output := cmd.OutOrStdout()

	fmt.Fprintf(output, "Mode: %s\n", cfg.Mode)

	var token *oca.Token
	var err error

	switch method {
	case "browser":
		fmt.Fprintln(output, "Opening browser for OCA authentication...")
		token, err = oca.LoginWithPKCE(ctx, cfg)
	case "headless":
		fmt.Fprintln(output, "Starting device code authentication...")
		token, err = oca.LoginWithDeviceCode(ctx, cfg, output)
	default:
		return fmt.Errorf("unknown authentication method: %s (use 'browser' or 'headless')", method)
	}

	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Ensure mode is stored with the token
	token.Mode = cfg.Mode

	// Save token
	store := oca.NewTokenStore()
	if err := store.Save(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Fprintln(output, "Successfully authenticated with Oracle Code Assist!")

	// Fetch and display available models
	models, err := oca.FetchModels(ctx, p.LiteLLMBaseURL, token.AccessToken)
	if err != nil {
		fmt.Fprintf(output, "Warning: could not fetch available models: %v\n", err)
		return nil
	}

	if len(models) > 0 {
		fmt.Fprintln(output, "\nAvailable models:")
		for _, m := range models {
			fmt.Fprintf(output, "  - %s\n", m.ID)
		}
		// Model IDs from litellm may already include "oca/" prefix
		exampleModel := models[0].ID
		if !hasOCAPrefix(exampleModel) {
			exampleModel = "oca/" + exampleModel
		}
		fmt.Fprintf(output, "\nTo use: cagent run --model %s <agent.yaml>\n", exampleModel)
	}

	return nil
}

func hasOCAPrefix(modelID string) bool {
	return len(modelID) > 4 && modelID[:4] == "oca/"
}

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "logout",
		Short:   "Log out from a model provider",
		GroupID: "core",
	}

	cmd.AddCommand(newLogoutOCACmd())

	return cmd
}

func newLogoutOCACmd() *cobra.Command {
	return &cobra.Command{
		Use:   "oca",
		Short: "Log out from Oracle Code Assist (OCA)",
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("logout oca", args)

			store := oca.NewTokenStore()
			if err := store.Delete(); err != nil {
				return fmt.Errorf("logout failed: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Successfully logged out from Oracle Code Assist.")
			return nil
		},
	}
}
