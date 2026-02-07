package spinner

import (
	"testing"

	"charm.land/lipgloss/v2"
)

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
