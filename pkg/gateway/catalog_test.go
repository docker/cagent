package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequiredEnvVars_local(t *testing.T) {
	secrets, err := RequiredEnvVars(t.Context(), "github-official")
	require.NoError(t, err)

	assert.Len(t, secrets, 1)
	assert.Equal(t, "GITHUB_PERSONAL_ACCESS_TOKEN", secrets[0].Env)
	assert.Equal(t, "github.personal_access_token", secrets[0].Name)
}

func TestRequiredEnvVars_remote(t *testing.T) {
	secrets, err := RequiredEnvVars(t.Context(), "apify")
	require.NoError(t, err)

	assert.Empty(t, secrets)
}

func TestServerSpec_local(t *testing.T) {
	server, err := ServerSpec(t.Context(), "fetch")
	require.NoError(t, err)

	assert.Equal(t, "server", server.Type)
}

func TestServerSpec_remote(t *testing.T) {
	server, err := ServerSpec(t.Context(), "apify")
	require.NoError(t, err)

	assert.Equal(t, "remote", server.Type)
	assert.Equal(t, "https://mcp.apify.com", server.Remote.URL)
	assert.Equal(t, "streamable-http", server.Remote.TransportType)
}
