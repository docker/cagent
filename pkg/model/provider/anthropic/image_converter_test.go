package anthropic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
)

func TestConvertImagePart(t *testing.T) {
	t.Parallel()

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
			result := convertImagePart(t.Context(), nil, tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertImagePartWithFileRef(t *testing.T) {
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
			name: "file ID (standard API doesn't support)",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileID,
					FileID:     "file-abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true, // Standard API doesn't support file IDs
		},
		{
			name: "unknown source type",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: "unknown",
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertImagePart(t.Context(), nil, tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertBetaImagePartWithFileRef(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.png")
	testImageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	tests := []struct {
		name     string
		imageURL *chat.MessageImageURL
		wantNil  bool
	}{
		{
			name: "local file path",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeLocalPath,
					LocalPath:  testImagePath,
					MimeType:   "image/png",
				},
			},
			wantNil: false,
		},
		{
			name: "file ID reference",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileID,
					FileID:     "file-abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: false, // Beta API supports file IDs
		},
		{
			name: "data URL",
			imageURL: &chat.MessageImageURL{
				URL: "data:image/png;base64,iVBORw0KGgo=",
			},
			wantNil: false,
		},
		{
			name: "https URL",
			imageURL: &chat.MessageImageURL{
				URL: "https://example.com/image.png",
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertBetaImagePart(t.Context(), nil, tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertDataURLToImageBlock(t *testing.T) {
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
			name:    "valid gif",
			dataURL: "data:image/gif;base64,R0lGODlh",
			wantNil: false,
		},
		{
			name:    "valid webp",
			dataURL: "data:image/webp;base64,UklGR",
			wantNil: false,
		},
		{
			name:    "missing comma",
			dataURL: "data:image/jpeg;base64",
			wantNil: true,
		},
		{
			name:    "empty data",
			dataURL: "data:image/jpeg;base64,",
			wantNil: false, // Empty base64 is technically valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertDataURLToImageBlock(tt.dataURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}
