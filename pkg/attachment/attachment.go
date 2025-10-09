package attachment

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
)

// ParseAttachCommand parses user input for /attach commands
// Returns the message text (with /attach commands removed) and the attachment path
func ParseAttachCommand(input string) (messageText, attachPath string) {
	lines := strings.Split(input, "\n")
	var messageLines []string

	for _, line := range lines {
		// Look for /attach anywhere in the line
		attachIndex := strings.Index(line, "/attach ")
		if attachIndex != -1 {
			// Extract the part before /attach
			beforeAttach := line[:attachIndex]

			// Extract the part after /attach (starting after "/attach ")
			afterAttachStart := attachIndex + 8 // Length of "/attach "
			if afterAttachStart < len(line) {
				afterAttach := line[afterAttachStart:]
				// Parse the file path (first token)
				tokens := strings.Fields(afterAttach)
				if len(tokens) > 0 {
					attachPath = tokens[0]

					// Reconstruct the line with /attach and file path removed
					var remainingText string
					if len(tokens) > 1 {
						remainingText = strings.Join(tokens[1:], " ")
					}

					// Combine the text before /attach and any text after the file path
					var parts []string
					if strings.TrimSpace(beforeAttach) != "" {
						parts = append(parts, strings.TrimSpace(beforeAttach))
					}
					if remainingText != "" {
						parts = append(parts, remainingText)
					}
					reconstructedLine := strings.Join(parts, " ")
					if reconstructedLine != "" {
						messageLines = append(messageLines, reconstructedLine)
					}
				}
			}
		} else {
			// Keep lines without /attach commands
			messageLines = append(messageLines, line)
		}
	}

	// Join the message lines back together
	messageText = strings.TrimSpace(strings.Join(messageLines, "\n"))
	return messageText, attachPath
}

// CreateUserMessageWithAttachment creates a user message with optional image attachment
func CreateUserMessageWithAttachment(agentFilename, userContent, attachmentPath string) *session.Message {
	if attachmentPath == "" {
		return session.UserMessage(agentFilename, userContent)
	}

	// Convert file to data URL
	dataURL, err := FileToDataURL(attachmentPath)
	if err != nil {
		// Return a regular message with error info in content instead of failing silently
		errorContent := userContent
		if errorContent == "" {
			errorContent = fmt.Sprintf("Failed to attach file %s: %v", attachmentPath, err)
		} else {
			errorContent = fmt.Sprintf("%s\n\n[Attachment Error: Failed to attach file %s: %v]", userContent, attachmentPath, err)
		}
		return session.UserMessage(agentFilename, errorContent)
	}

	// Ensure we have some text content when attaching a file
	textContent := userContent
	if strings.TrimSpace(textContent) == "" {
		textContent = "Please analyze this attached file."
	}

	// Create message with multi-content including text and image
	multiContent := []chat.MessagePart{
		{
			Type: chat.MessagePartTypeText,
			Text: textContent,
		},
		{
			Type: chat.MessagePartTypeImageURL,
			ImageURL: &chat.MessageImageURL{
				URL:    dataURL,
				Detail: chat.ImageURLDetailAuto,
			},
		},
	}

	return &session.Message{
		AgentFilename: agentFilename,
		AgentName:     "",
		Message: chat.Message{
			Role:         chat.MessageRoleUser,
			MultiContent: multiContent,
			CreatedAt:    time.Now().Format(time.RFC3339),
		},
	}
}

// FileToDataURL converts a file to a data URL
func FileToDataURL(filePath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}

	// Read file content
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Determine MIME type based on file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	var mimeType string
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	case ".bmp":
		mimeType = "image/bmp"
	case ".svg":
		mimeType = "image/svg+xml"
	default:
		return "", fmt.Errorf("unsupported image format: %s", ext)
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(fileBytes)

	// Create data URL
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	return dataURL, nil
}
