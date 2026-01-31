package bedrock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
)

func TestConvertImageURL(t *testing.T) {
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
			name: "data URL jpeg",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
			},
			wantNil: false,
		},
		{
			name: "data URL png",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/png;base64,iVBORw0KGgo=",
			},
			wantNil: false,
		},
		{
			name: "data URL gif",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/gif;base64,R0lGODlh",
			},
			wantNil: false,
		},
		{
			name: "data URL webp",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/webp;base64,UklGRlYAAABXRUJQ",
			},
			wantNil: false,
		},
		{
			name: "http URL not supported",
			imageURL: &chat.MessageImageURL{
				URL: "http://example.com/image.png",
			},
			wantNil: true, // Bedrock doesn't support URL-based images
		},
		{
			name: "https URL not supported",
			imageURL: &chat.MessageImageURL{
				URL: "https://example.com/image.jpg",
			},
			wantNil: true, // Bedrock doesn't support URL-based images
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
			name: "file ID not supported",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileID,
					FileID:     "file-abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true,
		},
		{
			name: "file URI not supported",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileURI,
					FileURI:    "https://example.com/file",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true,
		},
		{
			name: "invalid data URL format",
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
			result := convertImageURL(tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertUserContent(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.png")
	testImageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       *chat.Message
		wantCount int
	}{
		{
			name: "text only",
			msg: &chat.Message{
				Content: "Hello world",
			},
			wantCount: 1,
		},
		{
			name: "empty content",
			msg: &chat.Message{
				Content: "   ",
			},
			wantCount: 0,
		},
		{
			name: "multi-content text only",
			msg: &chat.Message{
				MultiContent: []chat.MessagePart{
					{Type: chat.MessagePartTypeText, Text: "Hello"},
					{Type: chat.MessagePartTypeText, Text: "World"},
				},
			},
			wantCount: 2,
		},
		{
			name: "multi-content with image",
			msg: &chat.Message{
				MultiContent: []chat.MessagePart{
					{Type: chat.MessagePartTypeText, Text: "Check this"},
					{
						Type: chat.MessagePartTypeImageURL,
						ImageURL: &chat.MessageImageURL{
							URL: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
						},
					},
				},
			},
			wantCount: 2,
		},
		{
			name: "multi-content with local file",
			msg: &chat.Message{
				MultiContent: []chat.MessagePart{
					{Type: chat.MessagePartTypeText, Text: "Check this"},
					{
						Type: chat.MessagePartTypeImageURL,
						ImageURL: &chat.MessageImageURL{
							FileRef: &chat.FileReference{
								SourceType: chat.FileSourceTypeLocalPath,
								LocalPath:  testImagePath,
								MimeType:   "image/png",
							},
						},
					},
				},
			},
			wantCount: 2,
		},
		{
			name: "skip nil imageURL",
			msg: &chat.Message{
				MultiContent: []chat.MessagePart{
					{Type: chat.MessagePartTypeText, Text: "Hello"},
					{Type: chat.MessagePartTypeImageURL, ImageURL: nil},
				},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertUserContent(tt.msg)
			assert.Len(t, result, tt.wantCount)
		})
	}
}
