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

	// Apply max_tokens override if present in options
	// We need to apply it to the ModelConfig itself since that's what providers use
	// Only update MaxTokens if an option explicitly sets it (non-zero value)
	modelConfig := config.ModelConfig
	for _, opt := range mergedOpts {
		tempOpts := &options.ModelOptions{}
		opt(tempOpts)
		if mt := tempOpts.MaxTokens(); mt != 0 {
			modelConfig.MaxTokens = &mt
		}
	}

	// Use NewWithModels to support cloning routers that reference other models.
	// config.Models is populated by routers; for other providers it's nil (which is fine).
	clone, err := NewWithModels(ctx, &modelConfig, config.Models, config.Env, mergedOpts...)
	if err != nil {
		slog.Debug("Failed to clone provider; using base provider", "error", err, "id", base.ID())
		return base
	}

	return clone
}
