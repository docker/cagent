package tool

import (
	"github.com/charmbracelet/glamour/v2"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/tool/defaulttool"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/components/tool/readfile"
	"github.com/docker/cagent/pkg/tui/components/tool/shell"
	"github.com/docker/cagent/pkg/tui/components/tool/todotool"
	"github.com/docker/cagent/pkg/tui/components/tool/transfertask"
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

func (f *Factory) Create(
	msg *types.Message,
	renderer *glamour.TermRenderer,
	sessionState *service.SessionState,
) layout.Model {
	toolName := msg.ToolCall.Function.Name

	if builder, ok := f.registry.Get(toolName); ok {
		return builder(msg, sessionState)
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
	registry.Register(builtin.ToolNameEditFile, editfile.New)
	registry.Register(builtin.ToolNameWriteFile, writefile.New)
	registry.Register(builtin.ToolNameReadFile, readfile.New)
	registry.Register(builtin.ToolNameCreateTodo, todotool.New)
	registry.Register(builtin.ToolNameCreateTodos, todotool.New)
	registry.Register(builtin.ToolNameUpdateTodo, todotool.New)
	registry.Register(builtin.ToolNameListTodos, todotool.New)
	registry.Register(builtin.ToolNameShell, shell.New)

	return registry
}

func New(
	msg *types.Message,
	renderer *glamour.TermRenderer,
	sessionState *service.SessionState,
) layout.Model {
	return defaultFactory.Create(msg, renderer, sessionState)
}
