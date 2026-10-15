package think

import (
	"context"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/tools"
)

const ToolNameThink = "think"

// CreateToolSet is used by the tools registry.
func CreateToolSet() (tools.ToolSet, error) {
	return New(), nil
}

type ToolSet struct {
	mu       sync.Mutex
	thoughts []string
}

type Args struct {
	Thought string `json:"thought" jsonschema:"The thought to think about"`
}

func (t *ToolSet) callTool(_ context.Context, params Args) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.thoughts = append(t.thoughts, params.Thought)
	return tools.ResultSuccess("Thoughts:\n" + strings.Join(t.thoughts, "\n")), nil
}

func New() *ToolSet {
	return &ToolSet{}
}

func (t *ToolSet) Instructions() string {
	return `## Think Tool

Use the think tool as a scratchpad before acting. Think to:
- Check which rules or policies apply
- Verify you have all required information
- Validate planned actions before executing
- Reason through complex multi-step problems`
}

func (t *ToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         ToolNameThink,
			Category:     "think",
			Description:  "Use the tool to think about something. It will not obtain new information or change the database, but just append the thought to the log. Use it when complex reasoning or some cache memory is needed.",
			Parameters:   tools.MustSchemaFor[Args](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.callTool),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Think",
			},
		},
	}, nil
}
