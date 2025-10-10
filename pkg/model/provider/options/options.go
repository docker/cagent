package options

import (
	latest "github.com/docker/cagent/pkg/config/v2"
)

type ModelOptions struct {
	gateway          string
	StructuredOutput *latest.StructuredOutput
}

func (c *ModelOptions) Gateway() string {
	return c.gateway
}

type Opt func(*ModelOptions)

func WithGateway(gateway string) Opt {
	return func(cfg *ModelOptions) {
		cfg.gateway = gateway
	}
}

func WithStructuredOutput(output *latest.StructuredOutput) Opt {
	return func(cfg *ModelOptions) {
		cfg.StructuredOutput = output
	}
}

// FromModelOptions converts a concrete ModelOptions value into a slice of
// Opt configuration functions. Later Opts override earlier ones when applied.
func FromModelOptions(m ModelOptions) []Opt {
	var out []Opt
	if g := m.Gateway(); g != "" {
		out = append(out, WithGateway(g))
	}
	if m.StructuredOutput != nil {
		out = append(out, WithStructuredOutput(m.StructuredOutput))
	}
	return out
}
