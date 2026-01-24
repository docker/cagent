package styles

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

func TestRenderComposite(t *testing.T) {
	// Define a parent style with a background color and Bold
	// Blue background, White text, Bold
	parent := lipgloss.NewStyle().
		Background(lipgloss.Color("#0000FF")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)

	// Define a child content with some style and a reset
	// Red text
	childStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	content := childStyle.Render("Hello") + " World"

	// Expected behavior:
	// "Hello" is Red on Blue.
	// " World" is White on Blue (inherits parent colors), BUT should NOT be bold because we only cascade colors.

	output := RenderComposite(parent, content)

	// Verify "World" is present
	assert.Contains(t, output, "World")

	// The output should look like:
	// <ParentBold+Colors> <ChildRed> Hello <Reset> <ParentColorsOnly> World <Reset>
	//
	// We verify that the second sequence (after Reset) does NOT contain the bold code "1;".
	// The first sequence MUST contain it.

	// 1. Check start has bold
	assert.Contains(t, output, "\x1b[1;", "Output start should contain bold code")

	// 2. Check the injected sequence (after \x1b[0m) does NOT have bold
	// We split by the reset code to find the injected part
	parts := strings.Split(output, resetFull)
	if len(parts) >= 2 {
		// parts[0] is everything before first reset.
		// parts[1] starts with the injected sequence for " World"
		// The injected sequence is immediately at the start of parts[1]
		// It should be the color codes.

		// We expect NO "1;" in the sequence immediately following the reset.
		// However, we need to be careful about false positives if the color codes contained "1;" (unlikely for 24bit, but possible in 256 colors).
		// In 24-bit (TrueColor), numbers are usually `38;2;R;G;B` or `48;2;R;G;B`.
		// `1;` is specifically Bold.

		// Let's just assert it doesn't start with `\x1b[1;` or contain `;1;` close to the start.
		// Or simpler: compare it against a generated "color only" string.

		colorOnly := parent.
			UnsetBold().
			UnsetPadding().
			UnsetMargins().
			UnsetWidth().
			UnsetHeight()

		expectedSeq := colorOnly.Render("")
		// Remove trailing reset
		expectedSeq = strings.TrimSuffix(expectedSeq, resetFull)
		expectedSeq = strings.TrimSuffix(expectedSeq, resetShort)

		// The injected part should start with this expectedSeq
		assert.True(t, strings.HasPrefix(parts[1], expectedSeq), "Injected sequence should match color-only style")

		// And ensure expectedSeq is different from full parent seq (which has bold)
		fullSeq := parent.UnsetPadding().UnsetMargins().UnsetWidth().UnsetHeight().Render("")
		fullSeq = strings.TrimSuffix(fullSeq, resetFull)
		fullSeq = strings.TrimSuffix(fullSeq, resetShort)

		assert.NotEqual(t, expectedSeq, fullSeq, "Color-only sequence should differ from full sequence (bold)")
	}
}

func TestRenderComposite_FastPath(t *testing.T) {
	// Test fast path when content has no ANSI sequences
	parent := lipgloss.NewStyle().
		Background(lipgloss.Color("#0000FF")).
		Foreground(lipgloss.Color("#FFFFFF"))

	content := "Hello World"
	output := RenderComposite(parent, content)

	// Should contain the content
	assert.Contains(t, output, "Hello World")

	// Should have ANSI sequences from the parent style
	assert.Contains(t, output, "\x1b[")
}
