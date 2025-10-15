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

func (a replaceInstruction) Instructions() string {
	return strings.Replace(a.instruction, "{ORIGINAL_INSTRUCTIONS}", a.ToolSet.Instructions(), 1)
}
