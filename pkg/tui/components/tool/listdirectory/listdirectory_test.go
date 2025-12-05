package listdirectory

import (
	"testing"
)

func TestFormatSummary(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "empty directory",
			content:  "",
			expected: "empty directory",
		},
		{
			name:     "only files",
			content:  "FILE .dockerignore\nFILE Dockerfile\nFILE README.md\n",
			expected: "found 3 files",
		},
		{
			name:     "only one file",
			content:  "FILE Dockerfile\n",
			expected: "found 1 file",
		},
		{
			name:     "only directories",
			content:  "DIR  .docker\nDIR  .github\n",
			expected: "found 2 directories",
		},
		{
			name:     "only one directory",
			content:  "DIR  .docker\n",
			expected: "found 1 directory",
		},
		{
			name:     "mixed files and directories",
			content:  "DIR  .docker\nDIR  .github\nFILE .dockerignore\nFILE Dockerfile\nFILE README.md\n",
			expected: "found 3 files and 2 directories",
		},
		{
			name:     "truncated output",
			content:  "DIR  .docker\nFILE .dockerignore\nFILE Dockerfile\n...output truncated due to file limit...\n",
			expected: "found 2 files and 1 directory (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSummary(tt.content)
			if result != tt.expected {
				t.Errorf("formatSummary() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := pluralize(tt.count)
			if result != tt.expected {
				t.Errorf("pluralize(%d) = %q, want %q", tt.count, result, tt.expected)
			}
		})
	}
}

func TestPluralizeDirectory(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "ies"},
		{1, "y"},
		{2, "ies"},
		{100, "ies"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := pluralizeDirectory(tt.count)
			if result != tt.expected {
				t.Errorf("pluralizeDirectory(%d) = %q, want %q", tt.count, result, tt.expected)
			}
		})
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "current directory",
			path:     ".",
			expected: ".",
		},
		{
			name:     "absolute path",
			path:     "/usr/local/bin",
			expected: "/usr/local/bin",
		},
		{
			name:     "relative path",
			path:     "src/components",
			expected: "src/components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortenPath(tt.path)
			if result != tt.expected {
				t.Errorf("shortenPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}
