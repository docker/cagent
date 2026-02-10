package oca

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", r.FormValue("refresh_token"))
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	cfg := IDCSConfig{
		ClientID:      "test-client",
		TokenEndpoint: server.URL,
	}

	token, err := RefreshToken(context.Background(), cfg, "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}

	if token.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "new-access")
	}
	if token.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "new-refresh")
	}
}

func TestRefreshToken_KeepsOldRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"token_type":   "Bearer",
			"expires_in":   3600,
			// No refresh_token in response
		})
	}))
	defer server.Close()

	cfg := IDCSConfig{
		ClientID:      "test-client",
		TokenEndpoint: server.URL,
	}

	token, err := RefreshToken(context.Background(), cfg, "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}

	if token.RefreshToken != "old-refresh" {
		t.Errorf("RefreshToken = %q, want %q (should keep old)", token.RefreshToken, "old-refresh")
	}
}

func TestGetValidToken_ValidToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg := DefaultIDCSConfig()
	got, err := GetValidToken(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("GetValidToken() error = %v", err)
	}
	if got != "valid-token" {
		t.Errorf("GetValidToken() = %q, want %q", got, "valid-token")
	}
}

func TestGetValidToken_NoToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	cfg := DefaultIDCSConfig()
	_, err := GetValidToken(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("expected error for no token")
	}
}

func TestGetValidToken_ExpiredNoRefresh(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{
		AccessToken: "expired-token",
		ExpiresAt:   time.Now().Add(-1 * time.Hour).Unix(),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg := DefaultIDCSConfig()
	_, err := GetValidToken(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("expected error for expired token without refresh")
	}
}
