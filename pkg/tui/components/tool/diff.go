package tool

import (
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

func computeDiff(path, oldText, newText string) []*udiff.Hunk {
	currentContent, err := os.ReadFile(path)
	if err != nil {
		return []*udiff.Hunk{}
	}

	// Generate the old contents by applying inverse diff, the current file has
	// newText applied, so we need to reverse it
	oldContent := strings.Replace(string(currentContent), newText, oldText, 1)

	// Now compute diff between old (reconstructed) and new (complete file)
	edits := udiff.Strings(oldContent, string(currentContent))

	diff, err := udiff.ToUnifiedDiff("old", "new", oldContent, edits, 3)
	if err != nil {
		return []*udiff.Hunk{}
	}

	return normalizeDiff(diff.Hunks)
}

func normalizeDiff(diff []*udiff.Hunk) []*udiff.Hunk {
	for _, hunk := range diff {
		if len(hunk.Lines) == 0 {
			continue
		}

		normalized := make([]udiff.Line, 0, len(hunk.Lines))
		for i := 0; i < len(hunk.Lines); i++ {
			line := hunk.Lines[i]

			if line.Kind == udiff.Delete && i+1 < len(hunk.Lines) {
				next := hunk.Lines[i+1]
				if next.Kind == udiff.Insert && line.Content == next.Content {
					normalized = append(normalized, udiff.Line{
						Kind:    udiff.Equal,
						Content: line.Content,
					})
					i++
					continue
				}
			}

			normalized = append(normalized, line)
		}

		hunk.Lines = normalized
	}

	return diff
}
