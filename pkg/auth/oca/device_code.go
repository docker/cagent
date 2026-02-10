package oca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// LoginWithDeviceCode performs the OAuth2 Device Code flow against IDCS.
func LoginWithDeviceCode(ctx context.Context, cfg IDCSConfig, output io.Writer) (*Token, error) {
	// Step 1: Request device code
	form := url.Values{
		"response_type": {"device_code"},
		"scope":         {cfg.Scope},
		"client_id":     {cfg.ClientID},
	}

	resp, err := http.PostForm(cfg.DeviceEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var dcResp deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&dcResp); err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}

	// Step 2: Display instructions
	fmt.Fprintf(output, "To sign in, open: %s\n", dcResp.VerificationURI)
	fmt.Fprintf(output, "Enter code: %s\n", dcResp.UserCode)
	fmt.Fprintln(output, "Waiting for authorization...")

	// Step 3: Poll for token
	interval := dcResp.Interval
	if interval <= 0 {
		interval = 5
	}
	expiresIn := dcResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device code authorization timed out after %ds", expiresIn)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		token, done, newInterval, err := pollDeviceToken(cfg, dcResp.DeviceCode)
		if err != nil {
			return nil, err
		}
		if newInterval > 0 {
			interval = newInterval
		}
		if done {
			return token, nil
		}
	}
}

func pollDeviceToken(cfg IDCSConfig, deviceCode string) (token *Token, done bool, newInterval int, err error) {
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {cfg.ClientID},
		"device_code": {deviceCode},
	}

	resp, err := http.PostForm(cfg.TokenEndpoint, form)
	if err != nil {
		return nil, false, 0, fmt.Errorf("polling token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, 0, fmt.Errorf("reading token response: %w", err)
	}

	// Parse error response
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil {
			switch errResp.Error {
			case "authorization_pending":
				return nil, false, 0, nil
			case "slow_down":
				return nil, false, 10, nil // increase interval
			case "access_denied":
				return nil, false, 0, fmt.Errorf("authorization denied by user")
			case "expired_token":
				return nil, false, 0, fmt.Errorf("device code expired, please try again")
			}
		}
		return nil, false, 0, fmt.Errorf("token request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Parse success response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, false, 0, fmt.Errorf("parsing token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, false, 0, fmt.Errorf("empty access token in response")
	}

	t := TokenFromResponse(
		tokenResp.AccessToken,
		tokenResp.RefreshToken,
		strings.ToLower(tokenResp.TokenType),
		tokenResp.Scope,
		tokenResp.ExpiresIn,
	)
	return t, true, 0, nil
}
