package provider

import (
	"context"
	"log/slog"

	"github.com/docker/cagent/pkg/model/provider/options"
)

// CloneWithOptions returns a new Provider instance using the same provider/model
// as the base provider, applying the provided options. If cloning fails, the
// original base provider is returned.
func CloneWithOptions(ctx context.Context, base Provider, opts ...options.Opt) Provider {
	config := base.BaseConfig()

	// Preserve existing options, then apply overrides. Later opts take precedence.
	baseOpts := options.FromModelOptions(config.ModelOptions)
	mergedOpts := append(baseOpts, opts...)

	clone, err := New(ctx, &config.ModelConfig, config.Env, mergedOpts...)
	if err != nil {
		slog.Debug("Failed to clone provider; using base provider", "error", err, "id", base.ID())
		return base
	}

	return clone
}
