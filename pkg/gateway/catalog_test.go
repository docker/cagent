package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequiredEnvVars(t *testing.T) {
	secrets, err := RequiredEnvVars(t.Context(), "github-official")
	require.NoError(t, err)

	assert.Len(t, secrets, 1)
	assert.Equal(t, "GITHUB_PERSONAL_ACCESS_TOKEN", secrets[0].Env)
	assert.Equal(t, "github.personal_access_token", secrets[0].Name)
}
