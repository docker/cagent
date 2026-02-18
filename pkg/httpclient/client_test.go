package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithModelName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modelName string
		wantSet   bool
	}{
		{
			name:      "sets header when name is provided",
			modelName: "my-fast-model",
			wantSet:   true,
		},
		{
			name:      "skips header when name is empty",
			modelName: "",
			wantSet:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedHeaders http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				capturedHeaders = r.Header
			}))
			defer srv.Close()

			client := NewHTTPClient(WithModelName(tt.modelName))
			req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			if tt.wantSet {
				assert.Equal(t, tt.modelName, capturedHeaders.Get("X-Cagent-Model-Name"))
			} else {
				assert.Empty(t, capturedHeaders.Get("X-Cagent-Model-Name"))
			}
		})
	}
}

func TestWithModel(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
	}))
	defer srv.Close()

	client := NewHTTPClient(WithModel("gpt-4o"))
	req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "gpt-4o", capturedHeaders.Get("X-Cagent-Model"))
}

func TestWithProvider(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
	}))
	defer srv.Close()

	client := NewHTTPClient(WithProvider("openai"))
	req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "openai", capturedHeaders.Get("X-Cagent-Provider"))
}
