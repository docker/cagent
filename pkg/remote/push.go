package remote

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/docker/cagent/pkg/content"
)

// Push pushes an artifact from the content store to an OCI registry
func Push(reference string, _ ...crane.Option) error {
	store, err := content.NewStore()
	if err != nil {
		return fmt.Errorf("creating content store: %w", err)
	}

	img, err := store.GetArtifactImage(reference)
	if err != nil {
		return fmt.Errorf("loading artifact from store: %w", err)
	}

	ref, err := name.ParseReference(reference)
	if err != nil {
		return fmt.Errorf("parsing registry reference %s: %w", reference, err)
	}

	if err := crane.Push(img, ref.String()); err != nil {
		return fmt.Errorf("pushing image to registry %s: %w", reference, err)
	}

	return nil
}
