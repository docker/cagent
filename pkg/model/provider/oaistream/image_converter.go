package oaistream

import (
	"encoding/base64"
	"log/slog"
	"os"

	"github.com/openai/openai-go/v3"

	"github.com/docker/cagent/pkg/chat"
)

// convertImageURLToOpenAI converts a MessageImageURL to an OpenAI image content part.
// It handles file references (converting to base64), base64 data URLs, and HTTP(S) URLs.
func convertImageURLToOpenAI(imageURL *chat.MessageImageURL) *openai.ChatCompletionContentPartUnionParam {
	if imageURL == nil {
		return nil
	}

	var url string
	detail := string(imageURL.Detail)

	// Handle file reference (from /attach command)
	if imageURL.FileRef != nil {
		url = convertFileRefToDataURL(imageURL.FileRef)
		if url == "" {
			return nil
		}
	} else {
		url = imageURL.URL
	}

	// Empty URL means we couldn't convert
	if url == "" {
		return nil
	}

	result := openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
		URL:    url,
		Detail: detail,
	})
	return &result
}

// convertFileRefToDataURL handles file references, converting to base64 data URL
func convertFileRefToDataURL(fileRef *chat.FileReference) string {
	if fileRef == nil {
		return ""
	}

	switch fileRef.SourceType {
	case chat.FileSourceTypeFileID, chat.FileSourceTypeFileURI:
		// File IDs from other providers need to be re-read from disk
		// This shouldn't happen in normal flow since we store local paths
		slog.Warn("File ID/URI from another provider not supported for OpenAI, skipping",
			"file_id", fileRef.FileID,
			"source_type", fileRef.SourceType)
		return ""

	case chat.FileSourceTypeLocalPath:
		return convertLocalFileToDataURL(fileRef.LocalPath, fileRef.MimeType)

	default:
		slog.Warn("Unknown file source type", "type", fileRef.SourceType)
		return ""
	}
}

// convertLocalFileToDataURL reads a local file and converts it to a base64 data URL
func convertLocalFileToDataURL(localPath, mimeType string) string {
	data, err := os.ReadFile(localPath)
	if err != nil {
		slog.Warn("Failed to read local file", "path", localPath, "error", err)
		return ""
	}

	if mimeType == "" {
		mimeType = "image/jpeg" // Default
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	slog.Debug("Converted local file to base64 data URL", "path", localPath, "size", len(data))

	return "data:" + mimeType + ";base64," + encoded
}

// HasFileRef checks if any message part has a file reference that needs processing
func HasFileRef(multiContent []chat.MessagePart) bool {
	for _, part := range multiContent {
		if part.Type == chat.MessagePartTypeImageURL && part.ImageURL != nil && part.ImageURL.FileRef != nil {
			return true
		}
	}
	return false
}

// ConvertMultiContentWithFileSupport converts chat.MessagePart slices to OpenAI content parts,
// handling file references properly.
func ConvertMultiContentWithFileSupport(multiContent []chat.MessagePart) []openai.ChatCompletionContentPartUnionParam {
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(multiContent))
	for _, part := range multiContent {
		switch part.Type {
		case chat.MessagePartTypeText:
			parts = append(parts, openai.TextContentPart(part.Text))
		case chat.MessagePartTypeImageURL:
			if part.ImageURL != nil {
				// Use the file-aware converter
				if imgPart := convertImageURLToOpenAI(part.ImageURL); imgPart != nil {
					parts = append(parts, *imgPart)
				}
			}
		}
	}
	return parts
}
