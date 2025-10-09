package v2

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolset_ValidateIgnoreVCS(t *testing.T) {
	tests := []struct {
		name        string
		toolset     Toolset
		expectError bool
		errorMsg    string
	}{
		{
			name: "ignore_vcs with filesystem toolset - valid",
			toolset: Toolset{
				Type:      "filesystem",
				IgnoreVCS: true,
			},
			expectError: false,
		},
		{
			name: "ignore_vcs with non-filesystem toolset - invalid",
			toolset: Toolset{
				Type:      "memory",
				IgnoreVCS: true,
			},
			expectError: true,
			errorMsg:    "ignore_vcs can only be used with type 'filesystem'",
		},
		{
			name: "ignore_vcs false with memory toolset - valid",
			toolset: Toolset{
				Type:      "memory",
				Path:      "test.db",
				IgnoreVCS: false,
			},
			expectError: false,
		},
		{
			name: "filesystem toolset without ignore_vcs - valid",
			toolset: Toolset{
				Type: "filesystem",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.toolset.validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}