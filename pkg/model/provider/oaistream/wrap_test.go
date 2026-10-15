package oaistream

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/modelerrors"
)

// makeTestOpenAIError creates an *openai.Error with the given status code and
// optional Retry-After header value for testing.
func makeTestOpenAIError(t *testing.T, statusCode int, retryAfterValue string) *openaisdk.Error {
	t.Helper()
	header := http.Header{}
	if retryAfterValue != "" {
		header.Set("Retry-After", retryAfterValue)
	}
	resp := httptest.NewRecorder().Result()
	resp.StatusCode = statusCode
	resp.Header = header
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.openai.com/v1/chat/completions", http.NoBody)
	return &openaisdk.Error{
		StatusCode: statusCode,
		Response:   resp,
		Request:    req,
	}
}

func TestWrapOpenAIError(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, WrapOpenAIError(nil))
	})

	t.Run("non-openai error passes through unchanged", func(t *testing.T) {
		t.Parallel()
		orig := errors.New("some network error")
		result := WrapOpenAIError(orig)
		assert.Equal(t, orig, result)
		var se *modelerrors.StatusError
		assert.NotErrorAs(t, result, &se)
	})

	t.Run("429 without Retry-After wraps with zero RetryAfter", func(t *testing.T) {
		t.Parallel()
		apiErr := makeTestOpenAIError(t, 429, "")
		result := WrapOpenAIError(apiErr)
		var se *modelerrors.StatusError
		require.ErrorAs(t, result, &se)
		assert.Equal(t, 429, se.StatusCode)
		assert.Equal(t, time.Duration(0), se.RetryAfter)
		// Original error still accessible
		assert.ErrorIs(t, result, apiErr)
	})

	t.Run("429 with Retry-After header sets RetryAfter", func(t *testing.T) {
		t.Parallel()
		apiErr := makeTestOpenAIError(t, 429, "30")
		result := WrapOpenAIError(apiErr)
		var se *modelerrors.StatusError
		require.ErrorAs(t, result, &se)
		assert.Equal(t, 429, se.StatusCode)
		assert.Equal(t, 30*time.Second, se.RetryAfter)
	})

	t.Run("500 wraps with correct status code", func(t *testing.T) {
		t.Parallel()
		apiErr := makeTestOpenAIError(t, 500, "")
		result := WrapOpenAIError(apiErr)
		var se *modelerrors.StatusError
		require.ErrorAs(t, result, &se)
		assert.Equal(t, 500, se.StatusCode)
	})

	t.Run("wrapped error is classified correctly by ClassifyModelError", func(t *testing.T) {
		t.Parallel()
		apiErr := makeTestOpenAIError(t, 429, "10")
		result := WrapOpenAIError(apiErr)
		retryable, rateLimited, retryAfter := modelerrors.ClassifyModelError(result)
		assert.False(t, retryable)
		assert.True(t, rateLimited)
		assert.Equal(t, 10*time.Second, retryAfter)
	})

	t.Run("wrapped in fmt.Errorf still classified correctly", func(t *testing.T) {
		t.Parallel()
		apiErr := makeTestOpenAIError(t, 500, "")
		wrapped := fmt.Errorf("stream error: %w", WrapOpenAIError(apiErr))
		retryable, rateLimited, _ := modelerrors.ClassifyModelError(wrapped)
		assert.True(t, retryable)
		assert.False(t, rateLimited)
	})
}

func TestWrapOpenAIErrorClassifiesPrivateDetails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		status     int
		kind       modelerrors.OverflowKind
	}{
		{"ordinary error", `{"message":"private provider message"}`, 400, ""},
		{"context code", `{"message":"request rejected","code":"context_length_exceeded"}`, 400, modelerrors.OverflowKindTokens},
		{"long context message", `{"message":"` + strings.Repeat("request rejected ", 30) + `input exceeds the context window"}`, 400, modelerrors.OverflowKindTokens},
		{"brace-heavy message", `{"message":` + fmt.Sprintf("%q", strings.Repeat(`{"x":`, 6000)+"input exceeds the context window") + `}`, 400, modelerrors.OverflowKindTokens},
		{"embedded JSON is prose", `{"message":"{\"error\":{\"type\":\"request_too_large\"}} image exceeds the limit"}`, 400, modelerrors.OverflowKindMedia},
		{"string error", `{"error":"input exceeds the context window"}`, 400, modelerrors.OverflowKindTokens},
		{"detail", `{"detail":"input exceeds the context window"}`, 400, modelerrors.OverflowKindTokens},
		{"wire type", `{"type":"request_too_large","message":"maximum context length"}`, 400, modelerrors.OverflowKindWire},
		{"wire status", `{"message":"image exceeds the limit"}`, 413, modelerrors.OverflowKindWire},
		{"media", `{"message":"image exceeds the limit"}`, 400, modelerrors.OverflowKindMedia},
		{"code precedes media", `{"code":"context_length_exceeded","message":"image exceeds the limit"}`, 400, modelerrors.OverflowKindTokens},
		{"unknown fields ignored", `{"request_body":"maximum context length","detail":{"message":"maximum context length"}}`, 400, ""},
		{"rate limit", `{"message":"private rate limit diagnostic"}`, 429, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiErr := makeTestOpenAIError(t, tc.status, "30")
			require.NoError(t, json.Unmarshal([]byte(tc.body), apiErr))
			original := fmt.Errorf("stream error: %w", apiErr)
			wrapped := WrapOpenAIError(original)
			require.ErrorIs(t, wrapped, original)
			var got *openaisdk.Error
			require.ErrorAs(t, wrapped, &got)
			assert.Same(t, apiErr, got)
			var statusErr *modelerrors.StatusError
			require.ErrorAs(t, wrapped, &statusErr)
			assert.Equal(t, tc.status, statusErr.StatusCode)
			assert.Equal(t, 30*time.Second, statusErr.RetryAfter)
			assert.Equal(t, tc.kind, modelerrors.OverflowKindOf(wrapped))
			if tc.kind != "" {
				var overflow *modelerrors.ContextOverflowError
				require.ErrorAs(t, wrapped, &overflow)
				assert.Equal(t, tc.kind, overflow.Kind)
				assert.NotContains(t, modelerrors.FormatError(wrapped), "request rejected")
			}
			assert.NotContains(t, wrapped.Error(), "private")
			assert.NotContains(t, wrapped.Error(), "input exceeds the context window")
			retryable, limited, _ := modelerrors.ClassifyModelError(wrapped)
			assert.False(t, retryable)
			assert.Equal(t, tc.status == 429, limited)
		})
	}
}

func TestWrapOpenAIErrorKeepsProviderDetailsPrivate(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"message", "type", "code", "error", "detail", "param", "request_body"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			apiErr := makeTestOpenAIError(t, http.StatusBadRequest, "")
			apiErr.Request.URL.RawQuery = "api_key=request-url-secret"
			body, err := json.Marshal(map[string]string{field: "Invalid API key: random-opaque-production-password; confidential prompt content"})
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, apiErr))
			wrapped := WrapOpenAIError(apiErr)
			assert.Equal(t, "HTTP 400: "+apiErr.Error(), wrapped.Error())
			assert.Equal(t, wrapped.Error(), modelerrors.FormatError(wrapped))
			assert.Contains(t, apiErr.RawJSON(), "random-opaque-production-password", "explicit diagnostics remain available")
		})
	}
}
