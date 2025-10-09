package content

import (
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreBasicOperations(t *testing.T) {
	store, err := NewStore(WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	testData := []byte("Hello, World! This is a test artifact.")
	layer := static.NewLayer(testData, types.OCIUncompressedLayer)
	img := empty.Image
	img, err = mutate.AppendLayers(img, layer)
	require.NoError(t, err)

	testRef := "hello-world:v1.0.0"
	digest, err := store.StoreArtifact(img, testRef)
	require.NoError(t, err)

	retrievedImg, err := store.GetArtifactImage(testRef)
	require.NoError(t, err)
	assert.NotNil(t, retrievedImg)

	metadata, err := store.GetArtifactMetadata(testRef)
	require.NoError(t, err)

	assert.Equal(t, testRef, metadata.Reference)
	assert.Equal(t, digest, metadata.Digest)

	artifacts, err := store.ListArtifacts()
	require.NoError(t, err)

	found := false
	for _, artifact := range artifacts {
		if artifact.Reference == testRef {
			found = true
			break
		}
	}

	assert.True(t, found, "Artifact not found in list")
}

func TestStoreMultipleArtifacts(t *testing.T) {
	store, err := NewStore(WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	testRefs := []string{
		"app1:v1.0.0",
		"app2:v2.0.0",
		"app3:latest",
	}

	for i, ref := range testRefs {
		testData := fmt.Appendf(nil, "Test artifact %d", i+1)
		layer := static.NewLayer(testData, types.OCIUncompressedLayer)
		img := empty.Image
		img, err = mutate.AppendLayers(img, layer)
		require.NoError(t, err)

		digest, err := store.StoreArtifact(img, ref)
		require.NoError(t, err)

		assert.NotEmpty(t, digest)
	}

	artifacts, err := store.ListArtifacts()
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(artifacts), len(testRefs))

	for _, ref := range testRefs {
		img, err := store.GetArtifactImage(ref)
		require.NoError(t, err)
		assert.NotNil(t, img)
	}
}

func TestStoreResolution(t *testing.T) {
	store, err := NewStore(WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	testData := []byte("Resolution test artifact")
	layer := static.NewLayer(testData, types.OCIUncompressedLayer)
	img := empty.Image
	img, err = mutate.AppendLayers(img, layer)
	require.NoError(t, err)

	testRef := "resolution-test:latest"
	_, err = store.StoreArtifact(img, testRef)
	require.NoError(t, err)

	testCases := []string{
		testRef,
	}

	for _, tc := range testCases {
		img, err := store.GetArtifactImage(tc)
		require.NoError(t, err)
		assert.NotNil(t, img)
	}
}
