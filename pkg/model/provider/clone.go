package provider

import (
	"context"
	"log/slog"
	"strings"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// CloneWithOptions returns a new Provider instance using the same provider/model
// as the base provider, applying the provided options. If cloning fails, the
// original base provider is returned.
func CloneWithOptions(ctx context.Context, base Provider, env environment.Provider, opts ...options.Opt) Provider {
	if base == nil {
		return nil
	}

	id := strings.TrimSpace(base.ID())
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return base
	}

	cfg := &latest.ModelConfig{Provider: parts[0], Model: parts[1]}
	if env == nil {
		env = environment.NewDefaultProvider(ctx)
	}

	// Preserve existing options, then apply overrides. Later opts take precedence.
	baseOpts := options.FromModelOptions(base.Options())
	mergedOpts := append(baseOpts, opts...)

	cloned, err := New(ctx, cfg, env, mergedOpts...)
	if err != nil {
		slog.Debug("Failed to clone provider; using base provider", "error", err, "id", id)
		return base
	}
	return cloned
}
