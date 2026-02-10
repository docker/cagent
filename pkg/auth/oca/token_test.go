package oca

import (
	"testing"
	"time"
)

func TestToken_IsExpired(t *testing.T) {
	tests := []struct {
		name    string
		token   *Token
		expired bool
	}{
		{
			name:    "nil token",
			token:   nil,
			expired: true,
		},
		{
			name:    "empty access token",
			token:   &Token{},
			expired: true,
		},
		{
			name: "valid token",
			token: &Token{
				AccessToken: "abc",
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
			},
			expired: false,
		},
		{
			name: "expired token",
			token: &Token{
				AccessToken: "abc",
				ExpiresAt:   time.Now().Add(-1 * time.Minute).Unix(),
			},
			expired: true,
		},
		{
			name: "within renewal buffer",
			token: &Token{
				AccessToken: "abc",
				ExpiresAt:   time.Now().Add(2 * time.Minute).Unix(), // within 3-minute buffer
			},
			expired: true,
		},
		{
			name: "just outside renewal buffer",
			token: &Token{
				AccessToken: "abc",
				ExpiresAt:   time.Now().Add(4 * time.Minute).Unix(), // outside 3-minute buffer
			},
			expired: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestToken_CanRefresh(t *testing.T) {
	tests := []struct {
		name       string
		token      *Token
		canRefresh bool
	}{
		{
			name:       "nil token",
			token:      nil,
			canRefresh: false,
		},
		{
			name:       "no refresh token",
			token:      &Token{AccessToken: "abc"},
			canRefresh: false,
		},
		{
			name: "valid refresh token without expiry",
			token: &Token{
				AccessToken:  "abc",
				RefreshToken: "refresh",
			},
			canRefresh: true,
		},
		{
			name: "valid refresh token with future expiry",
			token: &Token{
				AccessToken:      "abc",
				RefreshToken:     "refresh",
				RefreshExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			},
			canRefresh: true,
		},
		{
			name: "expired refresh token",
			token: &Token{
				AccessToken:      "abc",
				RefreshToken:     "refresh",
				RefreshExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
			},
			canRefresh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.CanRefresh(); got != tt.canRefresh {
				t.Errorf("CanRefresh() = %v, want %v", got, tt.canRefresh)
			}
		})
	}
}

func TestTokenFromResponse(t *testing.T) {
	before := time.Now().Unix()
	token := TokenFromResponse("access", "refresh", "bearer", "openid", 3600)
	after := time.Now().Unix()

	if token.AccessToken != "access" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "access")
	}
	if token.RefreshToken != "refresh" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "refresh")
	}
	if token.TokenType != "bearer" {
		t.Errorf("TokenType = %q, want %q", token.TokenType, "bearer")
	}
	if token.ExpiresAt < before+3600 || token.ExpiresAt > after+3600 {
		t.Errorf("ExpiresAt = %d, expected ~%d", token.ExpiresAt, before+3600)
	}
	if token.RefreshExpiresAt < before+int64(refreshTokenLifetime.Seconds()) {
		t.Errorf("RefreshExpiresAt = %d, expected >= %d", token.RefreshExpiresAt, before+int64(refreshTokenLifetime.Seconds()))
	}
}

func TestTokenFromResponse_DefaultExpiresIn(t *testing.T) {
	before := time.Now().Unix()
	token := TokenFromResponse("access", "", "bearer", "", 0)

	// Should default to 3600
	if token.ExpiresAt < before+3600 || token.ExpiresAt > before+3601 {
		t.Errorf("ExpiresAt with default = %d, expected ~%d", token.ExpiresAt, before+3600)
	}
	// No refresh token means no refresh expiry
	if token.RefreshExpiresAt != 0 {
		t.Errorf("RefreshExpiresAt = %d, want 0", token.RefreshExpiresAt)
	}
}
