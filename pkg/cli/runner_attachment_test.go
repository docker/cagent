package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
)

func TestCreateUserMessageWithAttachment(t *testing.T) {
	t.Parallel()

	// Create a temporary test image file
	tmpDir := t.TempDir()
	jpegPath := filepath.Join(tmpDir, "test.jpg")
	pngPath := filepath.Join(tmpDir, "test.png")
	gifPath := filepath.Join(tmpDir, "test.gif")
	webpPath := filepath.Join(tmpDir, "test.webp")
	pdfPath := filepath.Join(tmpDir, "test.pdf")
	unsupportedPath := filepath.Join(tmpDir, "test.xyz")

	// Create test files
	for _, path := range []string{jpegPath, pngPath, gifPath, webpPath, pdfPath, unsupportedPath} {
		err := os.WriteFile(path, []byte("test data"), 0o644)
		require.NoError(t, err)
	}

	tests := []struct {
		name              string
		userContent       string
		attachmentPath    string
		wantMultiContent  bool
		wantFileRef       bool
		wantMimeType      string
		wantDefaultPrompt bool
	}{
		{
			name:             "no attachment",
			userContent:      "Hello world",
			attachmentPath:   "",
			wantMultiContent: false,
		},
		{
			name:             "jpeg attachment",
			userContent:      "Check this image",
			attachmentPath:   jpegPath,
			wantMultiContent: true,
			wantFileRef:      true,
			wantMimeType:     "image/jpeg",
		},
		{
			name:             "png attachment",
			userContent:      "Analyze this",
			attachmentPath:   pngPath,
			wantMultiContent: true,
			wantFileRef:      true,
			wantMimeType:     "image/png",
		},
		{
			name:             "gif attachment",
			userContent:      "What's in this gif?",
			attachmentPath:   gifPath,
			wantMultiContent: true,
			wantFileRef:      true,
			wantMimeType:     "image/gif",
		},
		{
			name:             "webp attachment",
			userContent:      "Describe this",
			attachmentPath:   webpPath,
			wantMultiContent: true,
			wantFileRef:      true,
			wantMimeType:     "image/webp",
		},
		{
			name:             "pdf attachment",
			userContent:      "Summarize this PDF",
			attachmentPath:   pdfPath,
			wantMultiContent: true,
			wantFileRef:      true,
			wantMimeType:     "application/pdf",
		},
		{
			name:              "attachment with empty content gets default prompt",
			userContent:       "",
			attachmentPath:    jpegPath,
			wantMultiContent:  true,
			wantFileRef:       true,
			wantMimeType:      "image/jpeg",
			wantDefaultPrompt: true,
		},
		{
			name:              "attachment with whitespace content gets default prompt",
			userContent:       "   ",
			attachmentPath:    jpegPath,
			wantMultiContent:  true,
			wantFileRef:       true,
			wantMimeType:      "image/jpeg",
			wantDefaultPrompt: true,
		},
		{
			name:             "non-existent file falls back to text only",
			userContent:      "Hello",
			attachmentPath:   "/non/existent/file.jpg",
			wantMultiContent: false,
		},
		{
			name:             "unsupported format falls back to text only",
			userContent:      "Hello",
			attachmentPath:   unsupportedPath,
			wantMultiContent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := CreateUserMessageWithAttachment(tt.userContent, tt.attachmentPath)

			require.NotNil(t, msg)
			assert.Equal(t, chat.MessageRoleUser, msg.Message.Role)

			if tt.wantMultiContent {
				assert.NotEmpty(t, msg.Message.MultiContent)
				assert.Len(t, msg.Message.MultiContent, 2) // text + image

				// Check text part
				textPart := msg.Message.MultiContent[0]
				assert.Equal(t, chat.MessagePartTypeText, textPart.Type)
				if tt.wantDefaultPrompt {
					assert.Equal(t, "Please analyze this attached file.", textPart.Text)
				} else {
					assert.Equal(t, tt.userContent, textPart.Text)
				}

				// Check image part
				imagePart := msg.Message.MultiContent[1]
				assert.Equal(t, chat.MessagePartTypeImageURL, imagePart.Type)
				assert.NotNil(t, imagePart.ImageURL)

				if tt.wantFileRef {
					assert.NotNil(t, imagePart.ImageURL.FileRef)
					assert.Equal(t, chat.FileSourceTypeLocalPath, imagePart.ImageURL.FileRef.SourceType)
					assert.NotEmpty(t, imagePart.ImageURL.FileRef.LocalPath)
					assert.Equal(t, tt.wantMimeType, imagePart.ImageURL.FileRef.MimeType)
				}
			} else {
				assert.Empty(t, msg.Message.MultiContent)
				assert.Equal(t, tt.userContent, msg.Message.Content)
			}
		})
	}
}

func TestParseAttachCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantText       string
		wantAttachPath string
	}{
		{
			name:           "no attach command",
			input:          "Hello world",
			wantText:       "Hello world",
			wantAttachPath: "",
		},
		{
			name:           "attach at start",
			input:          "/attach image.png describe this",
			wantText:       "describe this",
			wantAttachPath: "image.png",
		},
		{
			name:           "attach in middle",
			input:          "please /attach photo.jpg analyze it",
			wantText:       "please analyze it",
			wantAttachPath: "photo.jpg",
		},
		{
			name:           "attach only",
			input:          "/attach test.gif",
			wantText:       "",
			wantAttachPath: "test.gif",
		},
		{
			name:           "attach with path containing spaces handled",
			input:          "/attach my_image.png what is this?",
			wantText:       "what is this?",
			wantAttachPath: "my_image.png",
		},
		{
			name:           "multiline with attach",
			input:          "First line\n/attach image.jpg second part\nThird line",
			wantText:       "First line\nsecond part\nThird line",
			wantAttachPath: "image.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, path := ParseAttachCommand(tt.input)
			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantAttachPath, path)
		})
	}
}
