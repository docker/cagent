package tool

import (
	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/tool/defaulttool"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/components/tool/handoff"
	"github.com/docker/cagent/pkg/tui/components/tool/listdirectory"
	"github.com/docker/cagent/pkg/tui/components/tool/readfile"
	"github.com/docker/cagent/pkg/tui/components/tool/readmultiplefiles"
	"github.com/docker/cagent/pkg/tui/components/tool/shell"
	"github.com/docker/cagent/pkg/tui/components/tool/todotool"
	"github.com/docker/cagent/pkg/tui/components/tool/transfertask"
	"github.com/docker/cagent/pkg/tui/components/tool/webtool"
	"github.com/docker/cagent/pkg/tui/components/tool/writefile"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

// Factory creates tool components using the registry.
// It looks up registered component builders and falls back to a default component
// if no specific builder is registered for a tool.
type Factory struct {
	registry *Registry
}

func NewFactory(registry *Registry) *Factory {
	return &Factory{
		registry: registry,
	}
}

func (f *Factory) Create(msg *types.Message, sessionState *service.SessionState) layout.Model {
	toolName := msg.ToolCall.Function.Name

	// First try to match by exact tool name
	if builder, ok := f.registry.Get(toolName); ok {
		return builder(msg, sessionState)
	}

	// Then try to match by category
	if msg.ToolDefinition.Category != "" {
		if builder, ok := f.registry.Get("category:" + msg.ToolDefinition.Category); ok {
			return builder(msg, sessionState)
		}
	}

	return defaulttool.New(msg, sessionState)
}

var (
	defaultRegistry = newDefaultRegistry()
	defaultFactory  = NewFactory(defaultRegistry)
)

func newDefaultRegistry() *Registry {
	registry := NewRegistry()

	registry.Register(builtin.ToolNameTransferTask, transfertask.New)
	registry.Register(builtin.ToolNameHandoff, handoff.New)
	registry.Register(builtin.ToolNameEditFile, editfile.New)
	registry.Register(builtin.ToolNameWriteFile, writefile.New)
	registry.Register(builtin.ToolNameReadFile, readfile.New)
	registry.Register(builtin.ToolNameReadMultipleFiles, readmultiplefiles.New)
	registry.Register(builtin.ToolNameListDirectory, listdirectory.New)
	registry.Register(builtin.ToolNameCreateTodo, todotool.New)
	registry.Register(builtin.ToolNameCreateTodos, todotool.New)
	registry.Register(builtin.ToolNameUpdateTodo, todotool.New)
	registry.Register(builtin.ToolNameListTodos, todotool.New)
	registry.Register(builtin.ToolNameShell, shell.New)

	// Register category-based handlers
	registry.Register("category:api", webtool.New)
	registry.Register(builtin.ToolNameFetch, webtool.New)

	return registry
}

func New(msg *types.Message, sessionState *service.SessionState) layout.Model {
	return defaultFactory.Create(msg, sessionState)
}
