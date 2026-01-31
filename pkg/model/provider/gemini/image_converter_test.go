package gemini

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
)

func TestConvertImageURLToPartWithClient(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.jpg")
	testImageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // Minimal JPEG header
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	tests := []struct {
		name     string
		imageURL *chat.MessageImageURL
		wantNil  bool
	}{
		{
			name:     "nil imageURL",
			imageURL: nil,
			wantNil:  true,
		},
		{
			name: "data URL",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
			},
			wantNil: false,
		},
		{
			name: "http URL",
			imageURL: &chat.MessageImageURL{
				URL: "http://example.com/image.png",
			},
			wantNil: false,
		},
		{
			name: "https URL",
			imageURL: &chat.MessageImageURL{
				URL: "https://example.com/image.jpg",
			},
			wantNil: false,
		},
		{
			name: "local file path",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeLocalPath,
					LocalPath:  testImagePath,
					MimeType:   "image/jpeg",
				},
			},
			wantNil: false,
		},
		{
			name: "non-existent local file",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeLocalPath,
					LocalPath:  "/non/existent/path.jpg",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true,
		},
		{
			name: "file URI reference",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileURI,
					FileURI:    "https://generativelanguage.googleapis.com/v1/files/abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: false,
		},
		{
			name: "file ID from other provider",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileID,
					FileID:     "file-abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true, // Gemini doesn't support file IDs from other providers
		},
		{
			name: "invalid data URL",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/jpeg", // missing comma and data
			},
			wantNil: true,
		},
		{
			name: "empty URL",
			imageURL: &chat.MessageImageURL{
				URL: "",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertImageURLToPartWithClient(t.Context(), nil, tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertDataURLToPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dataURL string
		wantNil bool
	}{
		{
			name:    "valid jpeg",
			dataURL: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
			wantNil: false,
		},
		{
			name:    "valid png",
			dataURL: "data:image/png;base64,iVBORw0KGgo=",
			wantNil: false,
		},
		{
			name:    "missing comma",
			dataURL: "data:image/jpeg;base64",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertDataURLToPart(tt.dataURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExtractMimeTypeFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url      string
		expected string
	}{
		{"https://example.com/image.jpg", "image/jpeg"},
		{"https://example.com/image.jpeg", "image/jpeg"},
		{"https://example.com/image.png", "image/png"},
		{"https://example.com/image.gif", "image/gif"},
		{"https://example.com/image.webp", "image/webp"},
		{"https://example.com/image.unknown", "image/jpeg"}, // default
		{"https://example.com/IMAGE.PNG", "image/png"},      // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			result := extractMimeTypeFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}
