package teamloader

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/tools"
)

type toolSet struct {
	tools.ToolSet
	instruction string
}

func (t toolSet) Instructions() string {
	return t.instruction
}

func TestWithInstructions(t *testing.T) {
	inner := &toolSet{}

	wrapped := WithInstructions(inner, "Manual instructions")

	assert.Equal(t, "Manual instructions", wrapped.Instructions())
}

func TestWithEmptyInstructions(t *testing.T) {
	inner := &toolSet{}

	wrapped := WithInstructions(inner, "")

	assert.Empty(t, wrapped.Instructions())
}

func TestWithAddInstructions(t *testing.T) {
	inner := &toolSet{
		instruction: "Existing instructions",
	}

	wrapped := WithInstructions(inner, "Manual instructions")

	assert.Equal(t, "Existing instructions\n\nManual instructions", wrapped.Instructions())
}

func TestWithAddEmptyInstructions(t *testing.T) {
	inner := &toolSet{
		instruction: "Existing instructions",
	}

	wrapped := WithInstructions(inner, "")

	assert.Equal(t, "Existing instructions", wrapped.Instructions())
}
