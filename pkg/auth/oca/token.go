package oca

import "time"

// renewBuffer is the time before actual expiration at which a token
// is considered expired and should be refreshed (matches ocaider's 3-minute buffer).
const renewBuffer = 3 * time.Minute

// refreshTokenLifetime is the assumed lifetime for refresh tokens
// when the server doesn't provide an explicit expiration (matches ocaider's ~8h).
const refreshTokenLifetime = 8 * time.Hour

// Token holds the OAuth2 tokens returned by IDCS.
type Token struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	Scope            string `json:"scope,omitempty"`
	ExpiresAt        int64  `json:"expires_at"`
	RefreshExpiresAt int64  `json:"refresh_expires_at,omitempty"`
}

// IsExpired returns true if the access token is expired or will expire within the renewal buffer.
func (t *Token) IsExpired() bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	return time.Now().Unix() >= t.ExpiresAt-int64(renewBuffer.Seconds())
}

// CanRefresh returns true if the token has a refresh token that hasn't expired.
func (t *Token) CanRefresh() bool {
	if t == nil || t.RefreshToken == "" {
		return false
	}
	if t.RefreshExpiresAt == 0 {
		return true
	}
	return time.Now().Unix() < t.RefreshExpiresAt
}

// TokenFromResponse creates a Token from the raw IDCS token response fields.
func TokenFromResponse(accessToken, refreshToken, tokenType, scope string, expiresIn int64) *Token {
	now := time.Now().Unix()
	if expiresIn <= 0 {
		expiresIn = 3600 // default 1 hour
	}
	t := &Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Scope:        scope,
		ExpiresAt:    now + expiresIn,
	}
	if refreshToken != "" {
		t.RefreshExpiresAt = now + int64(refreshTokenLifetime.Seconds())
	}
	return t
}
