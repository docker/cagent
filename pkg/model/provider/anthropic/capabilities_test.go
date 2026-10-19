package anthropic

import "testing"

func TestRejectsSampling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  bool
	}{
		{model: "claude-haiku-4-5", want: false},
		{model: "claude-haiku-5", want: true},
		{model: "claude-haiku-5-5", want: true},
		{model: "claude-haiku-5-5-20251001", want: true},
		{model: "vendor/claude-haiku-5", want: true},
		{model: "us.anthropic.claude-haiku-5-5", want: true},
		{model: "claude-sonnet-4-5", want: false},
		{model: "claude-sonnet-5", want: true},
		{model: "claude-opus-4-6", want: false},
		{model: "claude-opus-4-7", want: true},
		{model: "claude-fable-5", want: true},
		{model: "claude-mythos-5", want: true},
		{model: "not-a-claude-model", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			if got := rejectsSampling(tc.model); got != tc.want {
				t.Fatalf("rejectsSampling(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
