package openai

import (
	"context"
	"strings"
	"testing"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
)

type mapEnv struct{ m map[string]string }

func (e mapEnv) Get(ctx context.Context, name string) string { return e.m[name] }

func TestNewClient_OpenAI_Succeeds(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4o",
	}
	env := mapEnv{m: map[string]string{
		"OPENAI_API_KEY": "openai-key",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err != nil || c == nil {
		t.Fatalf("expected client without error, got client=%v, err=%v", c, err)
	}
}

func TestNewClient_AzureOptsWithoutAPIVersion_RequiresAPIVersion(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://myres.openai.azure.com/",
		ProviderOpts: map[string]any{
			"azure_deployment_name": "my-deployment",
		},
	}
	env := mapEnv{m: map[string]string{
		"AZURE_OPENAI_API_KEY": "azure-key",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to missing azure_api_version, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "azure_api_version is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClient_AzureWithAPIVersionSucceeds(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://myres.openai.azure.com/",
		ProviderOpts: map[string]any{
			"azure_api_version": "2024-10-21",
		},
	}
	env := mapEnv{m: map[string]string{
		"AZURE_OPENAI_API_KEY": "azure-key",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err != nil || c == nil {
		t.Fatalf("expected client without error, got client=%v, err=%v", c, err)
	}
}

func TestNewClient_AzureOptsRequireBaseURL(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		// BaseURL intentionally missing
		ProviderOpts: map[string]any{
			"azure_api_version": "2024-10-21",
		},
	}
	env := mapEnv{m: map[string]string{
		"AZURE_OPENAI_API_KEY": "azure-key",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to missing base_url, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "base_url is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClient_AzureWithEmptyAPIVersion_Fails(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://myres.openai.azure.com/",
		ProviderOpts: map[string]any{
			"azure_api_version": "",
		},
	}
	env := mapEnv{m: map[string]string{
		"AZURE_OPENAI_API_KEY": "azure-key",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to empty azure_api_version, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "azure_api_version is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClient_AzureMissingApiKey_ReturnsAzureKeyError(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://myres.openai.azure.com/",
		ProviderOpts: map[string]any{
			"azure_api_version": "2024-10-21",
		},
	}
	env := mapEnv{m: map[string]string{}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to missing AZURE_OPENAI_API_KEY, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(err.Error(), "AZURE_OPENAI_API_KEY") {
		t.Fatalf("expected AZURE_OPENAI_API_KEY error, got: %v", err)
	}
}

func TestNewClient_AzureWithCustomTokenKey_MissingEnv_ReturnsCustomKeyError(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://some.base.url/",
		TokenKey: "CUSTOM_AZURE_TOKEN",
		ProviderOpts: map[string]any{
			"azure_api_version": "2024-10-21",
		},
	}
	env := mapEnv{m: map[string]string{}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to missing CUSTOM_AZURE_TOKEN, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(err.Error(), "CUSTOM_AZURE_TOKEN key configured in model config is required") {
		t.Fatalf("expected custom token key error, got: %v", err)
	}
}

func TestNewClient_AzureWithCustomTokenKey_Succeeds(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://some.base.url/",
		TokenKey: "CUSTOM_AZURE_TOKEN",
		ProviderOpts: map[string]any{
			"azure_api_version": "2024-10-21",
		},
	}
	env := mapEnv{m: map[string]string{
		"CUSTOM_AZURE_TOKEN": "secret",
	}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err != nil || c == nil {
		t.Fatalf("expected client without error, got client=%v, err=%v", c, err)
	}
}

func TestNewClient_BaseURLAzureWithoutAzureOpts_RequiresOPENAIKey(t *testing.T) {
	ctx := context.Background()
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "my-deployment",
		BaseURL:  "https://some.base.url/",
		// No provider_opts with azure_* keys
	}
	env := mapEnv{m: map[string]string{}}
	c, err := NewClient(ctx, cfg, environment.Provider(env))
	if err == nil || c != nil {
		t.Fatalf("expected error due to missing OPENAI_API_KEY, got client=%v, err=%v", c, err)
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected OPENAI_API_KEY error, got: %v", err)
	}
}
