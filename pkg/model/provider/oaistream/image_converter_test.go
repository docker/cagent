package oaistream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
)

func TestConvertImageURLToOpenAI(t *testing.T) {
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
				URL:    "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
				Detail: chat.ImageURLDetailAuto,
			},
			wantNil: false,
		},
		{
			name: "http URL",
			imageURL: &chat.MessageImageURL{
				URL:    "http://example.com/image.png",
				Detail: chat.ImageURLDetailHigh,
			},
			wantNil: false,
		},
		{
			name: "https URL",
			imageURL: &chat.MessageImageURL{
				URL:    "https://example.com/image.jpg",
				Detail: chat.ImageURLDetailLow,
			},
			wantNil: false,
		},
		{
			name: "local file path",
			imageURL: &chat.MessageImageURL{
				Detail: chat.ImageURLDetailAuto,
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
			name: "file ID from other provider",
			imageURL: &chat.MessageImageURL{
				FileRef: &chat.FileReference{
					SourceType: chat.FileSourceTypeFileID,
					FileID:     "file-abc123",
					MimeType:   "image/jpeg",
				},
			},
			wantNil: true, // OpenAI doesn't support file IDs for chat completions
		},
		{
			name: "empty URL no file ref",
			imageURL: &chat.MessageImageURL{
				URL: "",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertImageURLToOpenAI(tt.imageURL)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestConvertFileRefToDataURL(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.png")
	testImageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	tests := []struct {
		name     string
		fileRef  *chat.FileReference
		wantData bool
	}{
		{
			name:     "nil fileRef",
			fileRef:  nil,
			wantData: false,
		},
		{
			name: "local file path",
			fileRef: &chat.FileReference{
				SourceType: chat.FileSourceTypeLocalPath,
				LocalPath:  testImagePath,
				MimeType:   "image/png",
			},
			wantData: true,
		},
		{
			name: "non-existent file",
			fileRef: &chat.FileReference{
				SourceType: chat.FileSourceTypeLocalPath,
				LocalPath:  "/non/existent/file.png",
				MimeType:   "image/png",
			},
			wantData: false,
		},
		{
			name: "file ID not supported",
			fileRef: &chat.FileReference{
				SourceType: chat.FileSourceTypeFileID,
				FileID:     "file-123",
			},
			wantData: false,
		},
		{
			name: "file URI not supported",
			fileRef: &chat.FileReference{
				SourceType: chat.FileSourceTypeFileURI,
				FileURI:    "https://example.com/file",
			},
			wantData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertFileRefToDataURL(tt.fileRef)
			if tt.wantData {
				assert.NotEmpty(t, result)
				assert.True(t, strings.HasPrefix(result, "data:"))
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestConvertLocalFileToDataURL(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.gif")
	testImageData := []byte{0x47, 0x49, 0x46, 0x38} // GIF header
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		result := convertLocalFileToDataURL(testImagePath, "image/gif")
		assert.True(t, strings.HasPrefix(result, "data:image/gif;base64,"))
	})

	t.Run("default mime type", func(t *testing.T) {
		t.Parallel()
		result := convertLocalFileToDataURL(testImagePath, "")
		assert.True(t, strings.HasPrefix(result, "data:image/jpeg;base64,")) // default
	})

	t.Run("non-existent file", func(t *testing.T) {
		t.Parallel()
		result := convertLocalFileToDataURL("/non/existent/file.jpg", "image/jpeg")
		assert.Empty(t, result)
	})
}

func TestConvertMultiContentWithFileSupport(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.jpg")
	testImageData := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	err := os.WriteFile(testImagePath, testImageData, 0o644)
	require.NoError(t, err)

	tests := []struct {
		name         string
		multiContent []chat.MessagePart
		wantCount    int
	}{
		{
			name:         "empty",
			multiContent: []chat.MessagePart{},
			wantCount:    0,
		},
		{
			name: "text only",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Hello"},
			},
			wantCount: 1,
		},
		{
			name: "text and URL image",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Check this"},
				{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{URL: "https://example.com/img.png"}},
			},
			wantCount: 2,
		},
		{
			name: "text and local file image",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Check this"},
				{
					Type: chat.MessagePartTypeImageURL,
					ImageURL: &chat.MessageImageURL{
						FileRef: &chat.FileReference{
							SourceType: chat.FileSourceTypeLocalPath,
							LocalPath:  testImagePath,
							MimeType:   "image/jpeg",
						},
					},
				},
			},
			wantCount: 2,
		},
		{
			name: "skip nil imageURL",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Hello"},
				{Type: chat.MessagePartTypeImageURL, ImageURL: nil},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertMultiContentWithFileSupport(tt.multiContent)
			assert.Len(t, result, tt.wantCount)
		})
	}
}
