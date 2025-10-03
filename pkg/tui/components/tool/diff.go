package tool

import (
	"github.com/aymanbagabas/go-udiff"
)

func computeDiff(oldText, newText string) []*udiff.Hunk {
	edits := udiff.Strings(oldText, newText)

	diff, err := udiff.ToUnifiedDiff("old", "new", oldText, edits, 3)
	if err != nil {
		return []*udiff.Hunk{}
	}

	return diff.Hunks
}
