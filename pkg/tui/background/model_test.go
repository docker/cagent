package background

import (
	"testing"
)

func TestGetEditorDisplayNameFromEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		visual    string
		editorEnv string
		want      string
	}{
		{
			name:      "VSCode",
			visual:    "",
			editorEnv: "code",
			want:      "VSCode",
		},
		{
			name:      "VSCode with args",
			visual:    "",
			editorEnv: "code --wait",
			want:      "VSCode",
		},
		{
			name:      "VSCode with full path",
			visual:    "",
			editorEnv: "/usr/local/bin/code --wait",
			want:      "VSCode",
		},
		{
			name:      "Vim",
			visual:    "",
			editorEnv: "vim",
			want:      "Vim",
		},
		{
			name:      "Neovim",
			visual:    "",
			editorEnv: "nvim",
			want:      "Neovim",
		},
		{
			name:      "Cursor",
			visual:    "",
			editorEnv: "cursor",
			want:      "Cursor",
		},
		{
			name:      "Unknown editor",
			visual:    "",
			editorEnv: "myeditor",
			want:      "Myeditor",
		},
		{
			name:      "Unknown editor with full path",
			visual:    "",
			editorEnv: "/opt/bin/myeditor",
			want:      "Myeditor",
		},
		{
			name:      "Empty (uses platform default)",
			visual:    "",
			editorEnv: "",
			want:      "Vi", // On non-Windows platforms, falls back to vi
		},
		{
			name:      "VSCode Insiders",
			visual:    "",
			editorEnv: "code-insiders",
			want:      "VSCode",
		},
		{
			name:      "Neovim Qt",
			visual:    "",
			editorEnv: "nvim-qt",
			want:      "Neovim",
		},
		{
			name:      "Vim GTK",
			visual:    "",
			editorEnv: "vim-gtk3",
			want:      "Vim",
		},
		{
			name:      "VISUAL takes precedence over EDITOR",
			visual:    "code",
			editorEnv: "vim",
			want:      "VSCode",
		},
		{
			name:      "VISUAL with args takes precedence",
			visual:    "code --wait",
			editorEnv: "vim",
			want:      "VSCode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := getEditorDisplayNameFromEnv(tt.visual, tt.editorEnv)
			if got != tt.want {
				t.Errorf("getEditorDisplayNameFromEnv(%q, %q) = %v, want %v", tt.visual, tt.editorEnv, got, tt.want)
			}
		})
	}
}
