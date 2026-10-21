package anthropic

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/modelinfo"
)

// serverSideFallbackBeta enables the `fallbacks` request parameter. The July
// date accepts both "default" and explicit lists.
const serverSideFallbackBeta = anthropic.AnthropicBetaServerSideFallback2026_07_01

// maxFallbacks is the longest list the API accepts.
const maxFallbacks = 3

// fallbackSpec is the parsed `fallbacks` provider_opt: the server's default
// routing, or an explicit list of models tried in order.
type fallbackSpec struct {
	Default bool
	Models  []string
}

func (s fallbackSpec) enabled() bool { return s.Default || len(s.Models) > 0 }

func (s fallbackSpec) param() anthropic.BetaFallbacksParamUnion {
	if s.Default {
		return anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}
	list := make([]anthropic.BetaFallbackParam, len(s.Models))
	for i, m := range s.Models {
		list[i] = anthropic.BetaFallbackParam{Model: m}
	}
	return anthropic.BetaFallbacksParamUnion{OfBetaFallbackArray: list}
}

func fallbacksEnabled(opts map[string]any) bool {
	spec, _ := parseFallbacks(opts)
	return spec.enabled()
}

// claudeAPIHost is the only host that serves server-side fallback.
const claudeAPIHost = "api.anthropic.com"

// validateFallbackHost rejects fallbacks unless requests go straight to the
// Claude API: other platforms and proxies don't support the parameter.
func validateFallbackHost(cfg *latest.ModelConfig, gateway string) error {
	if spec, _ := parseFallbacks(cfg.ProviderOpts); !spec.enabled() {
		return nil
	}
	if gateway != "" {
		return errors.New("anthropic: fallbacks requires the Claude API and is not available through a gateway")
	}
	// Without base_url, the SDK falls back to ANTHROPIC_BASE_URL.
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("ANTHROPIC_BASE_URL")
	}
	if baseURL != "" {
		if u, err := url.Parse(baseURL); err != nil || !strings.EqualFold(u.Hostname(), claudeAPIHost) {
			return fmt.Errorf("anthropic: fallbacks requires the Claude API and is not available through base URL %q", baseURL)
		}
	}
	return nil
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

// fallbackTargets returns the explicit fallback models; none for "default",
// whose targets the server picks. Construction has already validated the
// option, so a malformed value reads as none.
func fallbackTargets(opts map[string]any) []string {
	spec, _ := parseFallbacks(opts)
	return spec.Models
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
			return fmt.Errorf("anthropic: %s has no server-side fallback; remove the fallbacks list", cfg.Model)
		}
		if spec.Default {
			slog.Warn("Anthropic: fallbacks: default has no effect on this model", "model", cfg.Model)
		}
	}
	return nil
}

// validateFallbacks rejects server-side fallback when any model in the chain
// is Claude 5.x: a fallback can change the serving model mid-response, which
// the client does not handle yet. Malformed values are reported by
// [validateFallbackSpec].
func validateFallbacks(cfg *latest.ModelConfig) error {
	spec, _ := parseFallbacks(cfg.ProviderOpts)
	if !spec.Default && len(spec.Models) == 0 {
		return nil
	}
	for _, model := range append([]string{cfg.Model}, spec.Models...) {
		if _, ok := modelinfo.LookupClaude5(model); ok {
			return fmt.Errorf("anthropic: server-side fallback involving Claude 5.x model %q is not supported yet (see https://github.com/docker/docker-agent/issues/4584)", model)
		}
	}
	return nil
}
