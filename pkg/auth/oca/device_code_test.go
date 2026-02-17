package oca

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginWithDeviceCode(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/device"):
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "test-device-code",
				"user_code":        "TEST-CODE",
				"verification_uri": "https://example.com/activate",
				"expires_in":       300,
				"interval":         1,
			})

		case strings.HasSuffix(r.URL.Path, "/token"):
			callCount++
			if callCount < 3 {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "authorization_pending",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "test-access-token",
				"refresh_token": "test-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "openid offline_access",
			})
		}
	}))
	defer server.Close()

	cfg := IDCSConfig{
		Internal: IDCSProfile{
			ClientID:       "test-client",
			DeviceEndpoint: server.URL + "/device",
			TokenEndpoint:  server.URL + "/token",
			Scope:          "openid offline_access",
		},
		Mode: ModeInternal,
	}

	var output bytes.Buffer
	token, err := LoginWithDeviceCode(context.Background(), cfg, &output)
	if err != nil {
		t.Fatalf("LoginWithDeviceCode() error = %v", err)
	}

	if token.AccessToken != "test-access-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "test-access-token")
	}
	if token.RefreshToken != "test-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "test-refresh-token")
	}
	if token.Mode != ModeInternal {
		t.Errorf("Mode = %q, want %q", token.Mode, ModeInternal)
	}

	outputStr := output.String()
	if !strings.Contains(outputStr, "TEST-CODE") {
		t.Errorf("output should contain user code, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "https://example.com/activate") {
		t.Errorf("output should contain verification URI, got: %s", outputStr)
	}
}

func TestLoginWithDeviceCode_AccessDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/device"):
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "test-device-code",
				"user_code":        "TEST-CODE",
				"verification_uri": "https://example.com/activate",
				"expires_in":       300,
				"interval":         1,
			})
		case strings.HasSuffix(r.URL.Path, "/token"):
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "access_denied",
			})
		}
	}))
	defer server.Close()

	cfg := IDCSConfig{
		Internal: IDCSProfile{
			ClientID:       "test-client",
			DeviceEndpoint: server.URL + "/device",
			TokenEndpoint:  server.URL + "/token",
			Scope:          "openid offline_access",
		},
		Mode: ModeInternal,
	}

	var output bytes.Buffer
	_, err := LoginWithDeviceCode(context.Background(), cfg, &output)
	if err == nil {
		t.Fatal("expected error for access denied")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("error = %v, want containing 'denied'", err)
	}
}
