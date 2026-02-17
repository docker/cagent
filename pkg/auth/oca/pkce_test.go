package oca

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGenerateCodeVerifier(t *testing.T) {
	v1, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier() error = %v", err)
	}
	v2, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier() error = %v", err)
	}

	if v1 == v2 {
		t.Error("two verifiers should be different")
	}

	// base64url without padding, from 40 random bytes
	if len(v1) == 0 {
		t.Error("verifier should not be empty")
	}

	// Verify it's valid base64url
	_, err = base64.RawURLEncoding.DecodeString(v1)
	if err != nil {
		t.Errorf("verifier is not valid base64url: %v", err)
	}
}

func TestComputeCodeChallenge(t *testing.T) {
	verifier := "test-verifier-value"
	challenge := computeCodeChallenge(verifier)

	// Verify manually: SHA256(verifier) base64url-encoded
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])

	if challenge != expected {
		t.Errorf("computeCodeChallenge() = %q, want %q", challenge, expected)
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error = %v", err)
	}
	s2, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error = %v", err)
	}

	if s1 == s2 {
		t.Error("two states should be different")
	}
	if len(s1) == 0 {
		t.Error("state should not be empty")
	}
}

func TestBuildAuthorizationURL(t *testing.T) {
	p := &IDCSProfile{
		AuthEndpoint: "https://idcs.example.com/oauth2/v1/authorize",
		ClientID:     "test-client",
		Scope:        "openid offline_access",
	}

	url := buildAuthorizationURL(p, "challenge123", "state456", "http://localhost:8669/callback")

	if url == "" {
		t.Fatal("URL should not be empty")
	}

	// Check key params are present
	checks := []string{
		"response_type=code",
		"client_id=test-client",
		"code_challenge=challenge123",
		"code_challenge_method=S256",
		"state=state456",
		"redirect_uri=http",
	}
	for _, check := range checks {
		if !contains(url, check) {
			t.Errorf("URL missing %q: %s", check, url)
		}
	}
}

func TestFindAvailablePort(t *testing.T) {
	// Try finding a port from a reasonable range
	listener, port, err := findAvailablePort([]int{8669, 8668, 8667})
	if err != nil {
		t.Fatalf("findAvailablePort() error = %v", err)
	}
	defer listener.Close()

	if port < 8667 || port > 8669 {
		t.Errorf("port = %d, expected 8667-8669", port)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
