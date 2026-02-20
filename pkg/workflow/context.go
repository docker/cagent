package workflow

import (
	"encoding/json"
	"strings"
	"sync"
)

// StepContext holds outputs from executed steps for template evaluation and propagation.
// Keys are step IDs; values are either a single StepOutput (sequential/agent) or ParallelOutputs (parallel block).
// Safe for concurrent use (e.g. parallel steps writing different keys).
type StepContext struct {
	mu   sync.RWMutex
	data map[string]any
}

// NewStepContext returns a new StepContext.
func NewStepContext() StepContext {
	return StepContext{data: make(map[string]any)}
}

// Snapshot returns a shallow copy of the internal data map for serialization/debugging.
func (c *StepContext) Snapshot() map[string]any {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]any, len(c.data))
	for k, v := range c.data {
		out[k] = v
	}
	return out
}

// SetAgentOutput records the output of a single agent step by ID.
func (c *StepContext) SetAgentOutput(stepID, output, agentName string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]any)
	}
	c.data[stepID] = StepOutput{Output: output, Agent: agentName}
}

// SetParallelOutput records the outputs of a parallel block by its step ID.
func (c *StepContext) SetParallelOutput(stepID string, out *ParallelOutputs) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]any)
	}
	c.data[stepID] = out
}

// GetOutput returns the StepOutput for a step ID if it is a single agent output.
func (c *StepContext) GetOutput(stepID string) (StepOutput, bool) {
	if c == nil {
		return StepOutput{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[stepID]
	if !ok {
		return StepOutput{}, false
	}
	so, ok := v.(StepOutput)
	return so, ok
}

// GetParallelOutput returns the ParallelOutputs for a step ID if it is a parallel block.
func (c *StepContext) GetParallelOutput(stepID string) (*ParallelOutputs, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[stepID]
	if !ok {
		return nil, false
	}
	po, ok := v.(*ParallelOutputs)
	return po, ok
}

// EvalCondition evaluates a condition string against this context.
// Supports simple template form: {{ $steps.<id>.output }} or {{ $steps.<id>.output.path }}.
// Returns (value, true) if the expression resolves to a boolean; otherwise (nil, false).
// Full implementation would use a proper expression evaluator; this provides the contract.
func (c *StepContext) EvalCondition(condition string) (bool, bool) {
	expr := strings.TrimSpace(condition)
	expr = trimTemplateBraces(expr)
	if !strings.HasPrefix(expr, "$steps.") {
		return false, false
	}
	// Minimal path: $steps.<id>.output or $steps.<id>.outputs.<stepId>.output
	parts := strings.Split(expr, ".")
	if len(parts) < 3 {
		return false, false
	}
	stepID := parts[1]
	if len(parts) >= 5 && parts[2] == "outputs" {
		// $steps.par_id.outputs.step_id.output
		parID := parts[1]
		po, ok := c.GetParallelOutput(parID)
		if !ok {
			return false, false
		}
		subID := parts[3]
		so, ok := po.Steps[subID]
		if !ok {
			return false, false
		}
		if len(parts) == 5 && parts[4] == "output" {
			return parseBool(so.Output), true
		}
		return false, false
	}
	so, ok := c.GetOutput(stepID)
	if !ok {
		return false, false
	}
	// $steps.<id>.output or $steps.<id>.output.path (e.g. is_approved)
	if len(parts) == 3 && parts[2] == "output" {
		return parseBool(so.Output), true
	}
	if len(parts) >= 4 && parts[2] == "output" {
		// Try to parse so.Output as JSON and read path (e.g. is_approved)
		var m map[string]any
		if err := json.Unmarshal([]byte(so.Output), &m); err != nil {
			return parseBool(so.Output), true
		}
		v := getPath(m, parts[3:])
		return boolFromAny(v), true
	}
	return false, false
}

func trimTemplateBraces(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{{") {
		s = strings.TrimPrefix(s, "{{")
	}
	if strings.HasSuffix(s, "}}") {
		s = strings.TrimSuffix(s, "}}")
	}
	return strings.TrimSpace(s)
}

func getPath(m map[string]any, path []string) any {
	var v any = m
	for _, key := range path {
		if v == nil {
			return nil
		}
		mp, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v, ok = mp[key]
		if !ok {
			return nil
		}
	}
	return v
}

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes"
}

func boolFromAny(v any) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return parseBool(b)
	default:
		return false
	}
}
