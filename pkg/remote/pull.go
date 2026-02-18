package remote

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/docker/cagent/pkg/content"
)

// Pull pulls an artifact from a registry and stores it in the content store
func Pull(ctx context.Context, registryRef string, force bool, opts ...crane.Option) (string, error) {
	opts = append(opts, crane.WithContext(ctx))

	ref, err := name.ParseReference(registryRef)
	if err != nil {
		return "", fmt.Errorf("parsing registry reference %s: %w", registryRef, err)
	}

	remoteDigest, err := crane.Digest(ref.String(), opts...)
	if err != nil {
		return "", fmt.Errorf("resolving remote digest for %s: %w", registryRef, err)
	}

	store, err := content.NewStore()
	if err != nil {
		return "", fmt.Errorf("creating content store: %w", err)
	}

	localRef := ref.Context().RepositoryStr() + separator(ref) + ref.Identifier()
	if !force {
		if meta, metaErr := store.GetArtifactMetadata(localRef); metaErr == nil {
			if meta.Digest == remoteDigest {
				if !hasCagentAnnotation(meta.Annotations) {
					return "", fmt.Errorf("artifact %s found in store wasn't created by `cagent share push`\nTry to push again with `cagent share push` (cagent >= v1.10.0)", localRef)
				}
				return meta.Digest, nil
			}
		}
	}

	img, err := crane.Pull(ref.String(), opts...)
	if err != nil {
		return "", fmt.Errorf("pulling image from registry %s: %w", registryRef, err)
	}

	manifest, err := img.Manifest()
	if err != nil {
		return "", fmt.Errorf("getting manifest from pulled image: %w", err)
	}
	if !hasCagentAnnotation(manifest.Annotations) {
		return "", fmt.Errorf("artifact %s wasn't created by `cagent share push`\nTry to push again with `cagent share push` (cagent >= v1.10.0)", localRef)
	}

	digest, err := store.StoreArtifact(img, localRef)
	if err != nil {
		return "", fmt.Errorf("storing artifact in content store: %w", err)
	}

	return digest, nil
}

func hasCagentAnnotation(annotations map[string]string) bool {
	_, exists := annotations["io.docker.agent.version"]
	if !exists {
		_, exists = annotations["io.docker.cagent.version"]
	}
	return exists
}

// separator returns the separator used between repository and identifier.
// For digests it returns "@", for tags it returns ":".
func separator(ref name.Reference) string {
	if _, ok := ref.(name.Digest); ok {
		return "@"
	}
	return ":"
}
