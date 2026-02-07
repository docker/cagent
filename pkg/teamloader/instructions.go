package teamloader

import (
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

func WithInstructions(inner tools.ToolSet, instruction string) tools.ToolSet {
	if instruction == "" {
		return inner
	}

	return &replaceInstruction{
		ToolSet:     inner,
		instruction: instruction,
	}
}

type replaceInstruction struct {
	tools.ToolSet
	instruction string
}

// Verify interface compliance
var _ tools.Instructable = (*replaceInstruction)(nil)

func (a replaceInstruction) Instructions() string {
	original := tools.GetInstructions(a.ToolSet)
	return strings.Replace(a.instruction, "{ORIGINAL_INSTRUCTIONS}", original, 1)
}
