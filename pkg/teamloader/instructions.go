package teamloader

import (
	"github.com/docker/cagent/pkg/tools"
)

func WithInstructions(inner tools.ToolSet, instruction string) tools.ToolSet {
	if instruction == "" {
		return inner
	}

	return &addInstruction{
		ToolSet:     inner,
		instruction: instruction,
	}
}

type addInstruction struct {
	tools.ToolSet
	instruction string
}

func (a addInstruction) Instructions() string {
	innerInstructions := a.ToolSet.Instructions()
	if innerInstructions != "" {
		return innerInstructions + "\n\n" + a.instruction
	}
	return a.instruction
}
