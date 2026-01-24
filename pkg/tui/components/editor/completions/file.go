package completions

import (
	"sync"

	"github.com/docker/cagent/pkg/fsx"
	"github.com/docker/cagent/pkg/tui/components/completion"
)

type fileCompletion struct {
	mu     sync.Mutex
	items  []completion.Item
	loaded bool
}

func NewFileCompletion() Completion {
	return &fileCompletion{}
}

func (c *fileCompletion) AutoSubmit() bool {
	return false
}

func (c *fileCompletion) RequiresEmptyEditor() bool {
	return false
}

func (c *fileCompletion) Trigger() string {
	return "@"
}

func (c *fileCompletion) Items() []completion.Item {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached items if already loaded
	if c.loaded {
		return c.items
	}

	// Try to create VCS matcher for current directory
	vcsMatcher, err := fsx.NewVCSMatcher(".")

	// Prepare shouldIgnore function
	var shouldIgnore func(string) bool
	if err == nil && vcsMatcher != nil {
		shouldIgnore = vcsMatcher.ShouldIgnore
	}

	files, err := fsx.ListDirectory(".", shouldIgnore)
	if err != nil {
		// Do not mark as loaded on error, allow retry
		return nil
	}

	items := make([]completion.Item, len(files))
	for i, f := range files {
		items[i] = completion.Item{
			Label: f,
			Value: "@" + f,
		}
	}

	c.items = items
	c.loaded = true

	return c.items
}

func (c *fileCompletion) MatchMode() completion.MatchMode {
	return completion.MatchFuzzy
}
