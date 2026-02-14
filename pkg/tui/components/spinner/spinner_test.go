package spinner

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tui/animation"
)

func TestSpinnerCopyDoesNotLeakAnimationSubscription(t *testing.T) {
	s1 := New(ModeSpinnerOnly, lipgloss.NewStyle())
	cmd := s1.Init()
	require.NotNil(t, cmd)
	require.True(t, animation.HasActive())

	// Copy the spinner value and stop via the copy; should still stop the shared subscription.
	s2 := s1
	s2.Stop()
	require.False(t, animation.HasActive())
}

func BenchmarkSpinner_ModeSpinnerOnly(b *testing.B) {
	s := New(ModeSpinnerOnly, lipgloss.NewStyle())
	for b.Loop() {
		_ = s.View()
	}
}

func BenchmarkSpinner_ModeBoth(b *testing.B) {
	s := New(ModeBoth, lipgloss.NewStyle())
	for b.Loop() {
		_ = s.View()
	}
}
