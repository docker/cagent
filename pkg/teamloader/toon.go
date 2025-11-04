package teamloader

import (
	"github.com/docker/cagent/pkg/tools"
)

// WithToon is a no-op implementation that returns the toolset unchanged.
// The gotoon dependency was removed due to Go version compatibility issues.
// This function is kept for backward compatibility but does not transform output.
func WithToon(inner tools.ToolSet, toon string) tools.ToolSet {
	// Simply return the inner toolset without any transformation
	return inner
}
