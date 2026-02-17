package oca

import (
	"os"
	"testing"
	"time"
)

func TestTokenStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{
		AccessToken:      "test-access",
		RefreshToken:     "test-refresh",
		TokenType:        "bearer",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(8 * time.Hour).Unix(),
		Mode:             ModeInternal,
	}

	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}
	if loaded.ExpiresAt != token.ExpiresAt {
		t.Errorf("ExpiresAt = %d, want %d", loaded.ExpiresAt, token.ExpiresAt)
	}
	if loaded.Mode != token.Mode {
		t.Errorf("Mode = %q, want %q", loaded.Mode, token.Mode)
	}
}

func TestTokenStore_SaveLoad_ExternalMode(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{
		AccessToken: "test-access",
		ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
		Mode:        ModeExternal,
	}

	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Mode != ModeExternal {
		t.Errorf("Mode = %q, want %q", loaded.Mode, ModeExternal)
	}
}

func TestTokenStore_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if token != nil {
		t.Errorf("Load() = %v, want nil", token)
	}
}

func TestTokenStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{AccessToken: "test", ExpiresAt: time.Now().Add(1 * time.Hour).Unix()}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded != nil {
		t.Errorf("Load() after Delete() = %v, want nil", loaded)
	}
}

func TestTokenStore_DeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	// Should not error when file doesn't exist
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestTokenStore_SaveAtomicity(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	token := &Token{AccessToken: "test", ExpiresAt: time.Now().Add(1 * time.Hour).Unix()}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Check that no .tmp file is left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == tokenFileName+".tmp" {
			t.Errorf("temp file %q should not exist after save", entry.Name())
		}
	}
}

func TestTokenStore_HasValidToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStoreWithDir(dir)

	// No token stored
	if store.HasValidToken() {
		t.Error("HasValidToken() = true, want false (no token)")
	}

	// Save valid token
	token := &Token{
		AccessToken:  "test",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !store.HasValidToken() {
		t.Error("HasValidToken() = false, want true (valid token)")
	}

	// Save expired token with refresh capability
	token = &Token{
		AccessToken:      "test",
		RefreshToken:     "refresh",
		ExpiresAt:        time.Now().Add(-1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !store.HasValidToken() {
		t.Error("HasValidToken() = false, want true (refreshable token)")
	}
}
