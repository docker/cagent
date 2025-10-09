package attachment

import (
	"os"
	"strings"
	"testing"
)

func TestParseAttachCommand(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedText   string
		expectedAttach string
	}{
		{
			name:           "no attach command",
			input:          "Hello world",
			expectedText:   "Hello world",
			expectedAttach: "",
		},
		{
			name:           "attach at beginning",
			input:          "/attach test.png analyze this image",
			expectedText:   "analyze this image",
			expectedAttach: "test.png",
		},
		{
			name:           "attach in middle",
			input:          "Please /attach image.jpg describe what you see",
			expectedText:   "Please describe what you see",
			expectedAttach: "image.jpg",
		},
		{
			name:           "attach at end",
			input:          "Look at this /attach photo.svg",
			expectedText:   "Look at this",
			expectedAttach: "photo.svg",
		},
		{
			name:           "multiline with attach",
			input:          "First line\n/attach test.png\nLast line",
			expectedText:   "First line\nLast line",
			expectedAttach: "test.png",
		},
		{
			name:           "only attach command",
			input:          "/attach image.png",
			expectedText:   "",
			expectedAttach: "image.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messageText, attachPath := ParseAttachCommand(tt.input)
			if messageText != tt.expectedText {
				t.Errorf("ParseAttachCommand() messageText = %q, want %q", messageText, tt.expectedText)
			}
			if attachPath != tt.expectedAttach {
				t.Errorf("ParseAttachCommand() attachPath = %q, want %q", attachPath, tt.expectedAttach)
			}
		})
	}
}

func TestFileToDataURL(t *testing.T) {
	// Create a temporary SVG file for testing
	svgContent := `<svg width="10" height="10" xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="red"/></svg>`
	tmpFile, err := os.CreateTemp("", "test*.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(svgContent); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	tests := []struct {
		name       string
		filePath   string
		wantErr    bool
		wantPrefix string
	}{
		{
			name:       "valid SVG file",
			filePath:   tmpFile.Name(),
			wantErr:    false,
			wantPrefix: "data:image/svg+xml;base64,",
		},
		{
			name:     "non-existent file",
			filePath: "/non/existent/file.png",
			wantErr:  true,
		},
		{
			name:     "unsupported file type",
			filePath: "test.txt",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "unsupported file type" {
				// Create temporary txt file
				txtFile, err := os.CreateTemp("", "test*.txt")
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(txtFile.Name())
				txtFile.WriteString("test")
				txtFile.Close()
				tt.filePath = txtFile.Name()
			}

			dataURL, err := FileToDataURL(tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("FileToDataURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !strings.HasPrefix(dataURL, tt.wantPrefix) {
				t.Errorf("FileToDataURL() = %q, want prefix %q", dataURL, tt.wantPrefix)
			}
		})
	}
}
