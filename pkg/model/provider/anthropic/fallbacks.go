package anthropic

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/model/provider/providerutil"
	"github.com/docker/docker-agent/pkg/modelinfo"
)

// serverSideFallbackBeta enables the `fallbacks` request parameter, which
// lets Anthropic reroute refused requests to backup models in a single round
// trip.
const serverSideFallbackBeta anthropic.AnthropicBeta = "server-side-fallback-2026-06-01"

// maxFallbacks is the longest list the API accepts.
const maxFallbacks = 3

// fallbackSpec is the parsed `fallbacks` provider_opt: the server's default
// routing, or an explicit list of models tried in order.
type fallbackSpec struct {
	Default bool
	Models  []string
}

// parseFallbacks reads the `fallbacks` provider_opt: "default", or a list of
// at most three distinct model IDs. An absent or empty value disables
// fallbacks.
func parseFallbacks(opts map[string]any) (fallbackSpec, error) {
	var entries []any
	switch v := opts["fallbacks"].(type) {
	case nil:
		return fallbackSpec{}, nil
	case string:
		if v != "default" {
			return fallbackSpec{}, fmt.Errorf(`anthropic: fallbacks must be "default" or a list of model IDs, got %q`, v)
		}
		return fallbackSpec{Default: true}, nil
	case []string:
		for _, m := range v {
			entries = append(entries, m)
		}
	case []any:
		entries = v
	default:
		return fallbackSpec{}, fmt.Errorf(`anthropic: fallbacks must be "default" or a list of model IDs, got %T`, v)
	}
	if len(entries) > maxFallbacks {
		return fallbackSpec{}, fmt.Errorf("anthropic: fallbacks accepts at most %d models, got %d", maxFallbacks, len(entries))
	}
	var models []string
	for _, e := range entries {
		m, ok := e.(string)
		if !ok || m == "" {
			return fallbackSpec{}, fmt.Errorf("anthropic: fallbacks entries must be model IDs, got %v", e)
		}
		if slices.Contains(models, m) {
			return fallbackSpec{}, fmt.Errorf("anthropic: fallbacks lists %q twice", m)
		}
		models = append(models, m)
	}
	return fallbackSpec{Models: models}, nil
}

// validateFallbackSpec checks the rules the API enforces with a 400, so a bad
// config fails at client construction.
func validateFallbackSpec(cfg *latest.ModelConfig) error {
	spec, err := parseFallbacks(cfg.ProviderOpts)
	if err != nil {
		return err
	}
	if slices.Contains(spec.Models, cfg.Model) {
		return fmt.Errorf("anthropic: fallback %q is the same as the model", cfg.Model)
	}
	if m, ok := modelinfo.LookupClaude5(cfg.Model); ok && m.NoServerSideFallback {
		if len(spec.Models) > 0 {
			return fmt.Errorf("anthropic: %s has no server-side fallback; remove fallbacks or use \"default\"", cfg.Model)
		}
		if spec.Default {
			slog.Warn("Anthropic: fallbacks: default has no effect on this model", "model", cfg.Model)
		}
	}
	return nil
}

// fallbackModels extracts the `fallbacks` provider_opt: a list of model IDs,
// in priority order. Returns nil when the option is absent or malformed.
//
// Fallback models receive the exact same request as the primary model
// (thinking configuration, task budget, beta features, ...), so they must
// accept the same request shape — Anthropic rejects the fallback attempt
// otherwise.
func fallbackModels(opts map[string]any) []string {
	models, ok := providerutil.GetProviderOptStringSlice(opts, "fallbacks")
	if !ok || len(models) == 0 {
		return nil
	}
	slog.Debug("Anthropic provider_opts: enabling server-side fallbacks", "models", models)
	return models
}

// fallbacksBody returns the request option that injects the `fallbacks` body
// field. The wire shape is [{"model": "..."}, ...].
func fallbacksBody(models []string) option.RequestOption {
	fallbacks := make([]map[string]string, len(models))
	for i, m := range models {
		fallbacks[i] = map[string]string{"model": m}
	}
	return option.WithJSONSet("fallbacks", fallbacks)
}

// validateFallbacks rejects server-side fallback when any model in the chain
// is Claude 5.x: a fallback can change the serving model mid-response, which
// the client does not handle yet. Malformed lists stay ignored, as in
// [fallbackModels].
func validateFallbacks(cfg *latest.ModelConfig) error {
	fallbacks, ok := providerutil.GetProviderOptStringSlice(cfg.ProviderOpts, "fallbacks")
	if !ok || len(fallbacks) == 0 {
		return nil
	}
	for _, model := range append([]string{cfg.Model}, fallbacks...) {
		if _, ok := modelinfo.LookupClaude5(model); ok {
			return fmt.Errorf("anthropic: server-side fallback involving Claude 5.x model %q is not supported yet (see https://github.com/docker/docker-agent/issues/4584)", model)
		}
	}
	return nil
}
