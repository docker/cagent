package oca

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/browser"
)

// LoginWithPKCE performs the OAuth2 Authorization Code + PKCE flow.
func LoginWithPKCE(ctx context.Context, cfg IDCSConfig) (*Token, error) {
	p := cfg.ActiveProfile()

	// Generate PKCE verifier and challenge
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	challenge := computeCodeChallenge(verifier)

	// Generate CSRF state
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	// Find available port and start callback server
	listener, port, err := findAvailablePort(cfg.CallbackPorts)
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Channel to receive the authorization code
	type authResult struct {
		code string
		err  error
	}
	resultCh := make(chan authResult, 1)

	// Start callback server
	var srv http.Server
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			q := r.URL.Query()

			// Check for error
			if errMsg := q.Get("error"); errMsg != "" {
				desc := q.Get("error_description")
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, "<html><body><h2>Authentication Failed</h2><p>%s: %s</p></body></html>", errMsg, desc)
				resultCh <- authResult{err: fmt.Errorf("IDCS error: %s - %s", errMsg, desc)}
				return
			}

			// Validate state
			if q.Get("state") != state {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<html><body><h2>Authentication Failed</h2><p>State mismatch</p></body></html>")
				resultCh <- authResult{err: fmt.Errorf("state parameter mismatch (CSRF)")}
				return
			}

			code := q.Get("code")
			if code == "" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<html><body><h2>Authentication Failed</h2><p>No authorization code</p></body></html>")
				resultCh <- authResult{err: fmt.Errorf("no authorization code in callback")}
				return
			}

			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Authentication Successful</h2><p>You can close this window.</p></body></html>")
			resultCh <- authResult{code: code}
		})
	})
	srv.Handler = mux

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			resultCh <- authResult{err: fmt.Errorf("callback server error: %w", err)}
		}
	}()
	defer srv.Shutdown(context.Background())

	// Build authorization URL
	authURL := buildAuthorizationURL(p, challenge, state, redirectURI)

	// Open browser
	if err := browser.Open(ctx, authURL); err != nil {
		return nil, fmt.Errorf("opening browser: %w\n\nPlease open this URL manually:\n%s", err, authURL)
	}

	// Wait for callback
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		// Exchange code for tokens
		token, err := exchangeCodeForToken(p, result.code, verifier, redirectURI)
		if err != nil {
			return nil, err
		}
		token.Mode = cfg.Mode
		return token, nil
	}
}

func buildAuthorizationURL(p *IDCSProfile, challenge, state, redirectURI string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {p.Scope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return p.AuthEndpoint + "?" + params.Encode()
}

func exchangeCodeForToken(p *IDCSProfile, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {p.ClientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	resp, err := http.PostForm(p.TokenEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token in response")
	}

	return TokenFromResponse(
		tokenResp.AccessToken,
		tokenResp.RefreshToken,
		strings.ToLower(tokenResp.TokenType),
		tokenResp.Scope,
		tokenResp.ExpiresIn,
	), nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func findAvailablePort(ports []int) (net.Listener, int, error) {
	for _, port := range ports {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return l, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no available port among %v", ports)
}
