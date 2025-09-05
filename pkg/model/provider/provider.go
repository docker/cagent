package provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/anthropic"
	"github.com/docker/cagent/pkg/model/provider/dmr"
	"github.com/docker/cagent/pkg/model/provider/gemini"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Provider defines the interface for model providers
type Provider interface {
	// ID returns the model provider ID
	ID() string
	// CreateChatCompletionStream creates a streaming chat completion request
	// It returns a stream that can be iterated over to get completion chunks
	CreateChatCompletionStream(
		ctx context.Context,
		messages []chat.Message,
		tools []tools.Tool,
	) (chat.MessageStream, error)

	CreateChatCompletion(
		ctx context.Context,
		messages []chat.Message,
	) (string, error)
}

func New(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (Provider, error) {
	slog.Debug("Creating model provider", "type", cfg.Provider, "model", cfg.Model)

	switch cfg.Provider {
	case "openai":
		return openai.NewClient(ctx, cfg, env, opts...)

	case "anthropic":
		return anthropic.NewClient(ctx, cfg, env, opts...)

	case "google":
		return gemini.NewClient(ctx, cfg, env, opts...)

	case "dmr":
		return dmr.NewClient(ctx, cfg, opts...)

	default:
		slog.Error("Unknown provider type", "type", cfg.Provider)
		return nil, fmt.Errorf("unknown provider type: %s", cfg.Provider)
	}
}
