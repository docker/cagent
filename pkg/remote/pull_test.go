package remote

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/content"
)

func TestPullNonExistentRegistry(t *testing.T) {
	_, err := Pull("registry.example.com/non-existent:latest")
	require.Error(t, err)
}

func TestPullWithOptions(t *testing.T) {
	_, err := Pull("registry.example.com/test:latest", crane.Insecure)
	require.Error(t, err)
}

func TestPullIntegration(t *testing.T) {
	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	testData := []byte("test pull integration")
	layer := static.NewLayer(testData, types.OCIUncompressedLayer)
	img := empty.Image
	img, err = mutate.AppendLayers(img, layer)
	require.NoError(t, err)

	testRef := "pull-test:latest"
	digest, err := store.StoreArtifact(img, testRef)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})

	retrievedImg, err := store.GetArtifactImage(testRef)
	require.NoError(t, err)
	assert.NotNil(t, retrievedImg)

	err = Push("invalid:reference:with:too:many:colons")
	require.Error(t, err)
}
