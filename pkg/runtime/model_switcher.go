package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/modelsdev"
)

// ModelChoice represents a model available for selection in the TUI picker.
type ModelChoice struct {
	// Name is the display name (config key)
	Name string
	// Ref is the model reference used internally (e.g., "my_model" or "openai/gpt-4o")
	Ref string
	// Provider is the provider name (e.g., "openai", "anthropic")
	Provider string
	// Model is the specific model name (e.g., "gpt-4o", "claude-sonnet-4-0")
	Model string
	// IsDefault indicates this is the agent's configured default model
	IsDefault bool
	// IsCurrent indicates this is the currently active model for the agent
	IsCurrent bool
	// IsCustom indicates this is a custom model from the session history (not from config)
	IsCustom bool
	// IsCatalog indicates this is a model from the models.dev catalog
	IsCatalog bool
}

// ModelSwitcher is an optional interface for runtimes that support changing the model
// for the current agent at runtime. This is used by the TUI for model switching.
type ModelSwitcher interface {
	// SetAgentModel sets a model override for the specified agent.
	// modelRef can be:
	// - "" (empty) to clear the override and use the agent's default model
	// - A model name from the config (e.g., "my_fast_model")
	// - An inline model spec (e.g., "openai/gpt-4o")
	SetAgentModel(ctx context.Context, agentName, modelRef string) error

	// AvailableModels returns the list of models available for selection.
	// This includes all models defined in the config, with the current agent's
	// default model marked as IsDefault.
	AvailableModels(ctx context.Context) []ModelChoice
}

// ModelSwitcherConfig holds the configuration needed for model switching.
// This is populated by the app layer when creating the runtime.
type ModelSwitcherConfig struct {
	// Models is the map of model names to configurations from the loaded config
	Models map[string]latest.ModelConfig
	// Providers is the map of custom provider configurations
	Providers map[string]latest.ProviderConfig
	// ModelsGateway is the gateway URL if configured
	ModelsGateway string
	// EnvProvider provides access to environment variables
	EnvProvider environment.Provider
	// AgentDefaultModels maps agent names to their configured default model references
	AgentDefaultModels map[string]string
}

// SetAgentModel implements ModelSwitcher for LocalRuntime.
func (r *LocalRuntime) SetAgentModel(ctx context.Context, agentName, modelRef string) error {
	if r.modelSwitcherCfg == nil {
		return fmt.Errorf("model switching not configured for this runtime")
	}

	a, err := r.team.Agent(agentName)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	// Empty modelRef means clear the override (use agent's default)
	if modelRef == "" {
		a.SetModelOverride()
		slog.Info("Cleared agent model override (using default)", "agent", agentName)
		return nil
	}

	// Parse model references (handles both single and comma-separated refs)
	refs := splitModelRefs(modelRef)

	// Single reference - could be a config name or inline "provider/model"
	if len(refs) == 1 {
		prov, err := r.resolveModelRef(ctx, refs[0])
		if err != nil {
			return err
		}
		a.SetModelOverride(prov)
		slog.Info("Set agent model override", "agent", agentName, "model", prov.ID())
		return nil
	}

	// Multiple references - create an alloy (multiple providers)
	providers, err := r.resolveModelRefs(ctx, refs)
	if err != nil {
		return fmt.Errorf("failed to create alloy model: %w", err)
	}
	a.SetModelOverride(providers...)
	slog.Info("Set agent model override (alloy)", "agent", agentName, "model_count", len(providers))
	return nil
}

// splitModelRefs splits a comma-separated model reference string into individual refs.
func splitModelRefs(refs string) []string {
	var result []string
	for ref := range strings.SplitSeq(refs, ",") {
		if ref = strings.TrimSpace(ref); ref != "" {
			result = append(result, ref)
		}
	}
	return result
}

// isInlineAlloySpec checks if a model reference is an inline alloy specification.
// An inline alloy is comma-separated provider/model specs like "openai/gpt-4o,anthropic/claude-sonnet-4-0".
// All parts must contain a "/" (i.e., be inline specs) for it to be considered an inline alloy.
func isInlineAlloySpec(modelRef string) bool {
	refs := splitModelRefs(modelRef)
	if len(refs) < 2 {
		return false
	}
	for _, ref := range refs {
		if !strings.Contains(ref, "/") {
			return false
		}
	}
	return true
}

// resolveModelRef resolves a single model reference to a provider.
// The ref can be either a named model from config or an inline "provider/model" spec.
func (r *LocalRuntime) resolveModelRef(ctx context.Context, ref string) (provider.Provider, error) {
	// Check if it's a named model from config
	if modelCfg, exists := r.modelSwitcherCfg.Models[ref]; exists {
		// If the config itself contains comma-separated models, expand it
		if modelCfg.Provider == "" && strings.Contains(modelCfg.Model, ",") {
			// This is an alloy config - but we're resolving a single ref, so this is an error
			// unless the caller handles alloys explicitly
			providers, err := r.resolveModelRefs(ctx, splitModelRefs(modelCfg.Model))
			if err != nil {
				return nil, err
			}
			if len(providers) == 1 {
				return providers[0], nil
			}
			return nil, fmt.Errorf("model %q resolves to multiple providers; use resolveModelRefs instead", ref)
		}
		return r.createProviderFromConfig(ctx, &modelCfg)
	}

	// Try to parse as inline "provider/model" spec
	providerName, modelName, ok := strings.Cut(ref, "/")
	if !ok {
		return nil, fmt.Errorf("invalid model reference %q: expected a model name from config or 'provider/model' format", ref)
	}

	cfg := &latest.ModelConfig{
		Provider: providerName,
		Model:    modelName,
	}
	return r.createProviderFromConfig(ctx, cfg)
}

// resolveModelRefs resolves multiple model references to providers.
func (r *LocalRuntime) resolveModelRefs(ctx context.Context, refs []string) ([]provider.Provider, error) {
	providers := make([]provider.Provider, 0, len(refs))
	for _, ref := range refs {
		prov, err := r.resolveModelRef(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", ref, err)
		}
		providers = append(providers, prov)
	}
	return providers, nil
}

// AvailableModels implements ModelSwitcher for LocalRuntime.
func (r *LocalRuntime) AvailableModels(ctx context.Context) []ModelChoice {
	var choices []ModelChoice

	if r.modelSwitcherCfg == nil {
		return choices
	}

	// Get the current agent's default model reference
	currentAgentDefault := ""
	if r.modelSwitcherCfg.AgentDefaultModels != nil {
		currentAgentDefault = r.modelSwitcherCfg.AgentDefaultModels[r.currentAgent]
	}

	// Add all configured models, marking the current agent's default
	for name, cfg := range r.modelSwitcherCfg.Models {
		choices = append(choices, ModelChoice{
			Name:      name,
			Ref:       name,
			Provider:  cfg.Provider,
			Model:     cfg.Model,
			IsDefault: name == currentAgentDefault,
		})
	}

	// Append models.dev catalog entries filtered by available credentials
	catalogChoices := r.buildCatalogChoices(ctx)
	choices = append(choices, catalogChoices...)

	return choices
}

// CatalogStore is an extended interface for model stores that support fetching the full database.
type CatalogStore interface {
	ModelStore
	GetDatabase(ctx context.Context) (*modelsdev.Database, error)
}

// buildCatalogChoices builds ModelChoice entries from the models.dev catalog,
// filtered by supported providers and available credentials.
func (r *LocalRuntime) buildCatalogChoices(ctx context.Context) []ModelChoice {
	// Check if modelsStore supports GetDatabase
	catalogStore, ok := r.modelsStore.(CatalogStore)
	if !ok {
		slog.Debug("Models store does not support GetDatabase, skipping catalog")
		return nil
	}

	db, err := catalogStore.GetDatabase(ctx)
	if err != nil {
		slog.Debug("Failed to get models.dev database for catalog", "error", err)
		return nil
	}

	// Build set of existing model refs to avoid duplicates
	existingRefs := make(map[string]bool)
	for name, cfg := range r.modelSwitcherCfg.Models {
		existingRefs[name] = true
		if cfg.Provider != "" && cfg.Model != "" {
			existingRefs[cfg.Provider+"/"+cfg.Model] = true
		}
	}

	// Check which providers the user has credentials for
	availableProviders := r.getAvailableProviders(ctx)
	if len(availableProviders) == 0 {
		slog.Debug("No provider credentials available, skipping catalog")
		return nil
	}

	var choices []ModelChoice
	for providerID, prov := range db.Providers {
		// Check if this provider is supported and user has credentials
		cagentProvider, supported := mapModelsDevProvider(providerID)
		if !supported {
			continue
		}
		if !availableProviders[cagentProvider] {
			continue
		}

		for modelID, model := range prov.Models {
			// Skip models that don't output text (not suitable for chat)
			if !slices.Contains(model.Modalities.Output, "text") {
				continue
			}
			// Skip embedding models (not suitable for chat)
			if isEmbeddingModel(model.Family, model.Name) {
				continue
			}

			ref := cagentProvider + "/" + modelID
			if existingRefs[ref] {
				continue
			}
			existingRefs[ref] = true

			choices = append(choices, ModelChoice{
				Name:      model.Name,
				Ref:       ref,
				Provider:  cagentProvider,
				Model:     modelID,
				IsCatalog: true,
			})
		}
	}

	slog.Debug("Built catalog choices", "count", len(choices), "available_providers", len(availableProviders))
	return choices
}

// mapModelsDevProvider maps a models.dev provider ID to a cagent provider name.
// Returns the cagent provider name and whether it's supported.
// Uses provider.IsCatalogProvider to dynamically include all core providers
// and aliases with defined base URLs.
func mapModelsDevProvider(providerID string) (string, bool) {
	if provider.IsCatalogProvider(providerID) {
		return providerID, true
	}
	return "", false
}

// isEmbeddingModel returns true if the model is an embedding model
// based on its family or name fields from models.dev.
func isEmbeddingModel(family, name string) bool {
	familyLower := strings.ToLower(family)
	nameLower := strings.ToLower(name)
	return strings.Contains(familyLower, "embed") || strings.Contains(nameLower, "embed")
}

// getAvailableProviders returns a map of provider names that the user has credentials for.
func (r *LocalRuntime) getAvailableProviders(ctx context.Context) map[string]bool {
	available := make(map[string]bool)
	env := r.modelSwitcherCfg.EnvProvider

	// If using a models gateway, check for Docker token
	if r.modelSwitcherCfg.ModelsGateway != "" {
		if token, _ := env.Get(ctx, environment.DockerDesktopTokenEnv); token != "" {
			// Gateway supports all providers
			available["openai"] = true
			available["anthropic"] = true
			available["google"] = true
			available["mistral"] = true
			available["xai"] = true
		}
		return available
	}

	// Check credentials for each provider
	providerEnvVars := map[string]string{
		"openai":    "OPENAI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"google":    "GOOGLE_API_KEY",
		"mistral":   "MISTRAL_API_KEY",
		"xai":       "XAI_API_KEY",
		"nebius":    "NEBIUS_API_KEY",
		"requesty":  "REQUESTY_API_KEY",
		"azure":     "AZURE_API_KEY",
	}

	for providerName, envVar := range providerEnvVars {
		if key, _ := env.Get(ctx, envVar); key != "" {
			available[providerName] = true
		}
	}

	// DMR and ollama don't require credentials (local models)
	available["dmr"] = true
	available["ollama"] = true

	// Amazon Bedrock uses AWS credentials which can come from many sources.
	// We do a quick heuristic check for common indicators without blocking:
	// - AWS_ACCESS_KEY_ID: explicit access key
	// - AWS_PROFILE / AWS_DEFAULT_PROFILE: named profile (credentials in ~/.aws/)
	// - AWS_WEB_IDENTITY_TOKEN_FILE: EKS/IRSA web identity
	// - AWS_CONTAINER_CREDENTIALS_RELATIVE_URI: ECS task role
	// - AWS_ROLE_ARN: assumed role
	// Note: This won't catch all cases (e.g., EC2 instance profiles, SSO) but
	// those require network calls which would block the UI.
	awsCredentialIndicators := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_PROFILE",
		"AWS_DEFAULT_PROFILE",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_ROLE_ARN",
	}
	for _, indicator := range awsCredentialIndicators {
		if val, _ := env.Get(ctx, indicator); val != "" {
			available["amazon-bedrock"] = true
			break
		}
	}

	return available
}

// createProviderFromConfig creates a provider from a ModelConfig using the runtime's configuration.
func (r *LocalRuntime) createProviderFromConfig(ctx context.Context, cfg *latest.ModelConfig) (provider.Provider, error) {
	opts := []options.Opt{
		options.WithGateway(r.modelSwitcherCfg.ModelsGateway),
		options.WithProviders(r.modelSwitcherCfg.Providers),
	}

	// Look up max tokens from models.dev if not specified in config
	var maxTokens *int64
	if cfg.MaxTokens != nil {
		maxTokens = cfg.MaxTokens
	} else {
		defaultMaxTokens := int64(32000)
		maxTokens = &defaultMaxTokens
		if r.modelsStore != nil {
			m, err := r.modelsStore.GetModel(ctx, cfg.Provider+"/"+cfg.Model)
			if err == nil && m != nil {
				maxTokens = &m.Limit.Output
			}
		}
	}
	if maxTokens != nil {
		opts = append(opts, options.WithMaxTokens(*maxTokens))
	}

	return provider.NewWithModels(ctx,
		cfg,
		r.modelSwitcherCfg.Models,
		r.modelSwitcherCfg.EnvProvider,
		opts...,
	)
}

// WithModelSwitcherConfig sets the model switcher configuration for the runtime.
func WithModelSwitcherConfig(cfg *ModelSwitcherConfig) Opt {
	return func(r *LocalRuntime) {
		r.modelSwitcherCfg = cfg
	}
}

// Ensure LocalRuntime implements ModelSwitcher
var _ ModelSwitcher = (*LocalRuntime)(nil)
