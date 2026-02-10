package oca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(ctx context.Context, cfg IDCSConfig, refreshToken string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {cfg.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token in refresh response")
	}

	// Use the new refresh token if provided, otherwise keep the old one
	rt := tokenResp.RefreshToken
	if rt == "" {
		rt = refreshToken
	}

	return TokenFromResponse(
		tokenResp.AccessToken,
		rt,
		strings.ToLower(tokenResp.TokenType),
		tokenResp.Scope,
		tokenResp.ExpiresIn,
	), nil
}

// GetValidToken retrieves a valid access token, refreshing if necessary.
// Returns the access token string or an error if no valid token is available.
func GetValidToken(ctx context.Context, cfg IDCSConfig, store *TokenStore) (string, error) {
	t, err := store.Load()
	if err != nil {
		return "", fmt.Errorf("loading stored token: %w", err)
	}

	if t == nil {
		return "", fmt.Errorf("not authenticated with OCA. Run 'cagent login oca' to authenticate")
	}

	// Token is still valid
	if !t.IsExpired() {
		return t.AccessToken, nil
	}

	// Try to refresh
	if t.CanRefresh() {
		newToken, err := RefreshToken(ctx, cfg, t.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("token expired and refresh failed: %w\nRun 'cagent login oca' to re-authenticate", err)
		}
		if err := store.Save(newToken); err != nil {
			return "", fmt.Errorf("saving refreshed token: %w", err)
		}
		return newToken.AccessToken, nil
	}

	return "", fmt.Errorf("OCA token expired. Run 'cagent login oca' to re-authenticate")
}
