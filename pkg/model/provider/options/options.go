package options

import (
	latest "github.com/docker/cagent/pkg/config/v2"
)

type ModelOptions struct {
	gateway          string
	structuredOutput *latest.StructuredOutput
}

func (c *ModelOptions) Gateway() string {
	return c.gateway
}

func (c *ModelOptions) StructuredOutput() *latest.StructuredOutput {
	return c.structuredOutput
}

type Opt func(*ModelOptions)

func WithGateway(gateway string) Opt {
	return func(cfg *ModelOptions) {
		cfg.gateway = gateway
	}
}

func WithStructuredOutput(structuredOutput *latest.StructuredOutput) Opt {
	return func(cfg *ModelOptions) {
		cfg.structuredOutput = structuredOutput
	}
}

// FromModelOptions converts a concrete ModelOptions value into a slice of
// Opt configuration functions. Later Opts override earlier ones when applied.
func FromModelOptions(m ModelOptions) []Opt {
	var out []Opt
	if g := m.Gateway(); g != "" {
		out = append(out, WithGateway(g))
	}
	if m.structuredOutput != nil {
		out = append(out, WithStructuredOutput(m.structuredOutput))
	}
	return out
}
