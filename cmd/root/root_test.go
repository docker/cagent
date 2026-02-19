package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultToRun(t *testing.T) {
	t.Parallel()

	rootCmd := NewRootCmd()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no args defaults to run",
			args: []string{},
			want: []string{"run"},
		},
		{
			name: "nil args defaults to run",
			args: nil,
			want: []string{"run"},
		},
		{
			name: "known subcommand kept as-is",
			args: []string{"version"},
			want: []string{"version"},
		},
		{
			name: "run subcommand kept as-is",
			args: []string{"run", "./agent.yaml"},
			want: []string{"run", "./agent.yaml"},
		},
		{
			name: "help subcommand kept as-is",
			args: []string{"help"},
			want: []string{"help"},
		},
		{
			name: "--help flag kept as-is",
			args: []string{"--help"},
			want: []string{"--help"},
		},
		{
			name: "-h flag kept as-is",
			args: []string{"-h"},
			want: []string{"-h"},
		},
		{
			name: "only flags defaults to run",
			args: []string{"--debug"},
			want: []string{"run", "--debug"},
		},
		{
			name: "flags with agent file defaults to run",
			args: []string{"--debug", "./agent.yaml"},
			want: []string{"run", "--debug", "./agent.yaml"},
		},
		{
			name: "agent file without subcommand defaults to run",
			args: []string{"./agent.yaml"},
			want: []string{"run", "./agent.yaml"},
		},
		{
			name: "new subcommand kept as-is",
			args: []string{"new"},
			want: []string{"new"},
		},
		{
			name: "serve subcommand kept as-is",
			args: []string{"serve", "mcp", "./agent.yaml"},
			want: []string{"serve", "mcp", "./agent.yaml"},
		},
		{
			name: "debug and help still shows help",
			args: []string{"--debug", "--help"},
			want: []string{"--debug", "--help"},
		},
		{
			name: "agent file with flags defaults to run",
			args: []string{"./agent.yaml", "--yolo"},
			want: []string{"run", "./agent.yaml", "--yolo"},
		},
		{
			name: "__complete kept as-is for shell completion",
			args: []string{"__complete", "run", ""},
			want: []string{"__complete", "run", ""},
		},
		{
			name: "__completeNoDesc kept as-is for shell completion",
			args: []string{"__completeNoDesc", "run", ""},
			want: []string{"__completeNoDesc", "run", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := defaultToRun(rootCmd, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}
