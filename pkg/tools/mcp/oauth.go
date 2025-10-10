package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// resourceMetadataFromWWWAuth extracts resource metadata URL from WWW-Authenticate header
var re = regexp.MustCompile(`resource="([^"]+)"`)

// oauth is a simple struct for compatibility with existing code
type oauth struct {
	metadataClient *http.Client
}

// protectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata (RFC 8707)
type protectedResourceMetadata struct {
	Resource                          string   `json:"resource"`
	AuthorizationServers              []string `json:"authorization_servers"`
	ResourceName                      string   `json:"resource_name,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported            []string `json:"bearer_methods_supported,omitempty"`
	ResourceSigningAlgValuesSupported []string `json:"resource_signing_alg_values_supported,omitempty"`
}

// authorizationServerMetadata represents OAuth 2.0 Authorization Server Metadata (RFC 8414)
type authorizationServerMetadata struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RegistrationEndpoint                   string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint                     string   `json:"revocation_endpoint,omitempty"`
	IntrospectionEndpoint                  string   `json:"introspection_endpoint,omitempty"`
	JwksURI                                string   `json:"jwks_uri,omitempty"`
	ScopesSupported                        []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	ResponseModesSupported                 []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported                    []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported,omitempty"`
}

func (o *oauth) getAuthorizationServerMetadata(ctx context.Context, authServerURL string) (*authorizationServerMetadata, error) {
	// Build well-known metadata URL
	metadataURL := authServerURL
	if !strings.HasSuffix(authServerURL, "/.well-known/oauth-authorization-server") {
		metadataURL = strings.TrimSuffix(authServerURL, "/") + "/.well-known/oauth-authorization-server"
	}

	// Attempt OAuth authorization server discovery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := o.metadataClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Try OpenID Connect discovery as fallback
		openIDURL := strings.Replace(metadataURL, "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration", 1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, openIDURL, http.NoBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := o.metadataClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Return default metadata if all discovery fails
			return createDefaultMetadata(authServerURL), nil
		}
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, metadataURL)
	}

	var metadata authorizationServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata from %s: %w", metadataURL, err)
	}

	return validateAndFillDefaults(&metadata, authServerURL), nil
}

// validateAndFillDefaults validates required fields and fills in defaults
func validateAndFillDefaults(metadata *authorizationServerMetadata, authServerURL string) *authorizationServerMetadata {
	if metadata.Issuer == "" {
		metadata.Issuer = authServerURL
	}
	if len(metadata.ResponseTypesSupported) == 0 {
		metadata.ResponseTypesSupported = []string{"code"}
	}

	if len(metadata.ResponseModesSupported) == 0 {
		metadata.ResponseModesSupported = []string{"query", "fragment"}
	}
	if len(metadata.GrantTypesSupported) == 0 {
		metadata.GrantTypesSupported = []string{"authorization_code", "implicit"}
	}
	if len(metadata.TokenEndpointAuthMethodsSupported) == 0 {
		metadata.TokenEndpointAuthMethodsSupported = []string{"client_secret_basic"}
	}
	if len(metadata.RevocationEndpointAuthMethodsSupported) == 0 {
		metadata.RevocationEndpointAuthMethodsSupported = []string{"client_secret_basic"}
	}

	if metadata.AuthorizationEndpoint == "" {
		metadata.AuthorizationEndpoint = authServerURL + "/authorize"
	}
	if metadata.TokenEndpoint == "" {
		metadata.TokenEndpoint = authServerURL + "/token"
	}
	if metadata.RegistrationEndpoint == "" {
		metadata.RegistrationEndpoint = authServerURL + "/register"
	}

	return metadata
}

// createDefaultMetadata creates minimal metadata when discovery fails
func createDefaultMetadata(authServerURL string) *authorizationServerMetadata {
	return &authorizationServerMetadata{
		Issuer:                                 authServerURL,
		AuthorizationEndpoint:                  authServerURL + "/authorize",
		TokenEndpoint:                          authServerURL + "/token",
		RegistrationEndpoint:                   authServerURL + "/register",
		ResponseTypesSupported:                 []string{"code"},
		ResponseModesSupported:                 []string{"query", "fragment"},
		GrantTypesSupported:                    []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported:      []string{"client_secret_basic"},
		RevocationEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:          []string{"S256"},
	}
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func buildAuthorizationURL(authEndpoint, clientID, redirectURI, state, codeChallenge, resourceURL string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("resource", resourceURL) // RFC 8707: Resource Indicators
	return authEndpoint + "?" + params.Encode()
}

func exchangeCodeForToken(ctx context.Context, tokenEndpoint, code, codeVerifier, clientID, clientSecret, redirectURI string) (*OAuthToken, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", clientID)
	data.Set("code_verifier", codeVerifier)
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}

	return &token, nil
}

// requestAuthorizationCode requests the user to open the authorization URL and waits for the callback
func requestAuthorizationCode(ctx context.Context, authURL string, callbackServer *CallbackServer, expectedState string) (string, string, error) {
	if err := openBrowser(ctx, authURL); err != nil {
		return "", "", err
	}

	code, state, err := callbackServer.WaitForCallback(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to receive authorization callback: %w", err)
	}

	if state != expectedState {
		return "", "", fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
	}

	return code, state, nil
}

func openBrowser(ctx context.Context, urlToOpen string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", urlToOpen}
	case "darwin":
		cmd = "open"
		args = []string{urlToOpen}
	case "linux":
		cmd = "xdg-open"
		args = []string{urlToOpen}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	err := exec.CommandContext(ctx, cmd, args...).Start()
	if err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}

	return nil
}

// registerClient performs dynamic client registration
func registerClient(ctx context.Context, authMetadata *authorizationServerMetadata, redirectURI string, scopes []string) (clientID, clientSecret string, err error) {
	if authMetadata.RegistrationEndpoint == "" {
		return "", "", fmt.Errorf("authorization server does not support dynamic client registration")
	}

	reqBody := map[string]any{
		"redirect_uris": []string{redirectURI},
		"client_name":   "cagent",
		"grant_types":   []string{"authorization_code"},
		"response_types": []string{
			"code",
		},
	}
	if len(scopes) > 0 {
		reqBody["scope"] = strings.Join(scopes, " ")
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authMetadata.RegistrationEndpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", fmt.Errorf("failed to create registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to register client: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("client registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	var respBody struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return "", "", fmt.Errorf("failed to decode registration response: %w", err)
	}

	if respBody.ClientID == "" {
		return "", "", fmt.Errorf("registration response missing client_id")
	}

	return respBody.ClientID, respBody.ClientSecret, nil
}

func resourceMetadataFromWWWAuth(wwwAuth string) string {
	matches := re.FindStringSubmatch(wwwAuth)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}

// oauthTransport wraps an HTTP transport with OAuth support
type oauthTransport struct {
	base http.RoundTripper
	// TODO(rumpl): remove client reference, we need to find a better way to send elicitation requests
	client     *remoteMCPClient
	tokenStore OAuthTokenStore
	baseURL    string
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqClone := req.Clone(req.Context())

	if token, err := t.tokenStore.GetToken(t.baseURL); err == nil && !token.IsExpired() {
		reqClone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	}

	resp, err := t.base.RoundTrip(reqClone)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if wwwAuth != "" {
			resp.Body.Close()

			authServer := req.URL.Scheme + "://" + req.URL.Host
			if err := t.handleOAuthFlow(req.Context(), authServer, wwwAuth); err != nil {
				return nil, fmt.Errorf("OAuth flow failed: %w", err)
			}

			return t.RoundTrip(req)
		}
	}

	return resp, nil
}

// handleOAuthFlow performs the OAuth flow when a 401 response is received
func (t *oauthTransport) handleOAuthFlow(ctx context.Context, authServer, wwwAuth string) error {
	slog.Debug("Starting OAuth flow for server", "url", t.baseURL)

	var resourceURL string
	resourceURL = resourceMetadataFromWWWAuth(wwwAuth)
	slog.Debug("Extracted resource URL from WWW-Authenticate header", "resource_url", resourceURL)
	if resourceURL == "" {
		resourceURL = authServer + "/.well-known/oauth-protected-resource"
	}

	resp, err := http.DefaultClient.Get(resourceURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return err
	}
	var resourceMetadata protectedResourceMetadata
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&resourceMetadata); err != nil {
			return err
		}
	}

	if len(resourceMetadata.AuthorizationServers) == 0 {
		slog.Debug("No authorization servers in resource metadata, using auth server from WWW-Authenticate header")
		resourceMetadata.AuthorizationServers = []string{authServer}
	}

	oauth := &oauth{metadataClient: &http.Client{Timeout: 5 * time.Second}}
	authServerMetadata, err := oauth.getAuthorizationServerMetadata(ctx, resourceMetadata.AuthorizationServers[0])
	if err != nil {
		return fmt.Errorf("failed to fetch authorization server metadata: %w", err)
	}

	slog.Debug("Creating OAuth callback server")
	callbackServer, err := NewCallbackServer()
	if err != nil {
		return fmt.Errorf("failed to create callback server: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := callbackServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown callback server", "error", err)
		}
	}()

	if err := callbackServer.Start(); err != nil {
		return fmt.Errorf("failed to start callback server: %w", err)
	}

	redirectURI := callbackServer.GetRedirectURI()
	slog.Debug("Using redirect URI", "uri", redirectURI)

	clientID := ""
	clientSecret := ""

	if authServerMetadata.RegistrationEndpoint != "" {
		slog.Debug("Attempting dynamic client registration")
		clientID, clientSecret, err = registerClient(ctx, authServerMetadata, redirectURI, nil)
		if err != nil {
			slog.Debug("Dynamic registration failed", "error", err)
			// TODO(rumpl): fall back to requesting client ID from user
			return err
		}
	} else {
		// TODO(rumpl): fall back to requesting client ID from user
		return errors.New("authorization server does not support dynamic client registration")
	}

	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	callbackServer.SetExpectedState(state)
	verifier := oauth2.GenerateVerifier()

	authURL := buildAuthorizationURL(
		authServerMetadata.AuthorizationEndpoint,
		clientID,
		redirectURI,
		state,
		oauth2.S256ChallengeFromVerifier(verifier),
		t.baseURL,
	)

	// Request user consent via elicitation
	consent, err := t.client.requestUserConsent(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user consent: %w", err)
	}
	if !consent {
		return fmt.Errorf("user declined OAuth authorization")
	}

	slog.Debug("Requesting authorization code", "url", authURL)

	code, receivedState, err := requestAuthorizationCode(ctx, authURL, callbackServer, state)
	if err != nil {
		return fmt.Errorf("failed to get authorization code: %w", err)
	}

	if receivedState != state {
		return fmt.Errorf("state mismatch in authorization response")
	}

	slog.Debug("Exchanging authorization code for token")
	token, err := exchangeCodeForToken(
		ctx,
		authServerMetadata.TokenEndpoint,
		code,
		verifier,
		clientID,
		clientSecret,
		redirectURI,
	)
	if err != nil {
		return fmt.Errorf("failed to exchange code for token: %w", err)
	}

	if err := t.tokenStore.StoreToken(t.baseURL, token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	// Notify the runtime that the OAuth flow was successful
	t.client.oauthSuccess()

	slog.Debug("OAuth flow completed successfully")
	return nil
}
