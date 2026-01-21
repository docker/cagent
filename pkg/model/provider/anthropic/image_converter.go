package anthropic

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/docker/cagent/pkg/chat"
)

// fileUploadCache caches file IDs to avoid re-uploading the same file
var fileUploadCache = struct {
	sync.RWMutex
	cache map[string]string // localPath -> fileID
}{cache: make(map[string]string)}

// convertImagePart converts a MessageImageURL to an Anthropic image block.
// It handles file references (uploading via Files API if possible),
// base64 data URLs, and HTTP(S) URLs.
func convertImagePart(ctx context.Context, client *anthropic.Client, imageURL *chat.MessageImageURL) *anthropic.ContentBlockParamUnion {
	if imageURL == nil {
		return nil
	}

	// Handle file reference (from /attach command)
	if imageURL.FileRef != nil {
		return convertFileRefToImageBlock(ctx, client, imageURL.FileRef)
	}

	// Handle data URL (base64)
	if strings.HasPrefix(imageURL.URL, "data:") {
		return convertDataURLToImageBlock(imageURL.URL)
	}

	// Handle HTTP(S) URL
	if strings.HasPrefix(imageURL.URL, "http://") || strings.HasPrefix(imageURL.URL, "https://") {
		return &anthropic.ContentBlockParamUnion{
			OfImage: &anthropic.ImageBlockParam{
				Source: anthropic.ImageBlockParamSourceUnion{
					OfURL: &anthropic.URLImageSourceParam{
						URL: imageURL.URL,
					},
				},
			},
		}
	}

	return nil
}

// convertFileRefToImageBlock handles file references, uploading via Files API if available
func convertFileRefToImageBlock(ctx context.Context, client *anthropic.Client, fileRef *chat.FileReference) *anthropic.ContentBlockParamUnion {
	if fileRef == nil {
		return nil
	}

	switch fileRef.SourceType {
	case chat.FileSourceTypeFileID:
		// Already uploaded to Anthropic, use file ID directly
		// Note: File ID support is only available in the Beta API
		// The standard API will fall back to base64
		slog.Debug("Using existing file ID", "file_id", fileRef.FileID)
		// Standard API doesn't support file IDs, so we need to fall back
		// For now, log a warning and return nil (the beta converter handles this)
		slog.Warn("File ID references not supported in standard Anthropic API, skipping")
		return nil

	case chat.FileSourceTypeLocalPath:
		// Try to upload via Files API, fall back to base64
		return uploadOrConvertLocalFile(ctx, client, fileRef.LocalPath, fileRef.MimeType)

	default:
		slog.Warn("Unknown file source type", "type", fileRef.SourceType)
		return nil
	}
}

// uploadOrConvertLocalFile attempts to upload a local file via Files API.
// If that fails or no client is provided, falls back to base64 encoding.
func uploadOrConvertLocalFile(_ context.Context, _ *anthropic.Client, localPath, mimeType string) *anthropic.ContentBlockParamUnion {
	// For standard API, we always use base64 since it doesn't support file IDs
	// The Files API upload would be wasted since we can't reference it
	return convertLocalFileToBase64Block(localPath, mimeType)
}

// convertLocalFileToBase64Block reads a local file and converts it to a base64 image block
func convertLocalFileToBase64Block(localPath, mimeType string) *anthropic.ContentBlockParamUnion {
	data, err := os.ReadFile(localPath)
	if err != nil {
		slog.Warn("Failed to read local file", "path", localPath, "error", err)
		return nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	if mimeType == "" {
		mimeType = "image/jpeg" // Default
	}

	slog.Debug("Converted local file to base64", "path", localPath, "size", len(data))

	return &anthropic.ContentBlockParamUnion{
		OfImage: &anthropic.ImageBlockParam{
			Source: anthropic.ImageBlockParamSourceUnion{
				OfBase64: &anthropic.Base64ImageSourceParam{
					Data:      encoded,
					MediaType: anthropic.Base64ImageSourceMediaType(mimeType),
				},
			},
		},
	}
}

// convertDataURLToImageBlock parses a data URL and converts it to an image block
func convertDataURLToImageBlock(dataURL string) *anthropic.ContentBlockParamUnion {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil
	}

	mediaTypePart := parts[0]
	base64Data := parts[1]

	var mediaType string
	switch {
	case strings.Contains(mediaTypePart, "image/jpeg"):
		mediaType = "image/jpeg"
	case strings.Contains(mediaTypePart, "image/png"):
		mediaType = "image/png"
	case strings.Contains(mediaTypePart, "image/gif"):
		mediaType = "image/gif"
	case strings.Contains(mediaTypePart, "image/webp"):
		mediaType = "image/webp"
	default:
		mediaType = "image/jpeg"
	}

	return &anthropic.ContentBlockParamUnion{
		OfImage: &anthropic.ImageBlockParam{
			Source: anthropic.ImageBlockParamSourceUnion{
				OfBase64: &anthropic.Base64ImageSourceParam{
					Data:      base64Data,
					MediaType: anthropic.Base64ImageSourceMediaType(mediaType),
				},
			},
		},
	}
}

// Beta API versions that support file references

// convertBetaImagePart converts a MessageImageURL to a Beta API image block.
// It handles file references (uploading via Files API), base64 data URLs, and HTTP(S) URLs.
func convertBetaImagePart(ctx context.Context, client *anthropic.Client, imageURL *chat.MessageImageURL) *anthropic.BetaContentBlockParamUnion {
	if imageURL == nil {
		return nil
	}

	// Handle file reference (from /attach command)
	if imageURL.FileRef != nil {
		return convertBetaFileRefToImageBlock(ctx, client, imageURL.FileRef)
	}

	// Handle data URL (base64)
	if strings.HasPrefix(imageURL.URL, "data:") {
		return convertBetaDataURLToImageBlock(imageURL.URL)
	}

	// Handle HTTP(S) URL
	if strings.HasPrefix(imageURL.URL, "http://") || strings.HasPrefix(imageURL.URL, "https://") {
		return &anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfURL: &anthropic.BetaURLImageSourceParam{
						URL: imageURL.URL,
					},
				},
			},
		}
	}

	return nil
}

// convertBetaFileRefToImageBlock handles file references for the Beta API
func convertBetaFileRefToImageBlock(ctx context.Context, client *anthropic.Client, fileRef *chat.FileReference) *anthropic.BetaContentBlockParamUnion {
	if fileRef == nil {
		return nil
	}

	switch fileRef.SourceType {
	case chat.FileSourceTypeFileID:
		// Already uploaded, use file ID directly
		slog.Debug("Using existing file ID for Beta API", "file_id", fileRef.FileID)
		return &anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfFile: &anthropic.BetaFileImageSourceParam{
						FileID: fileRef.FileID,
					},
				},
			},
		}

	case chat.FileSourceTypeLocalPath:
		// Try to upload via Files API, fall back to base64
		return uploadOrConvertBetaLocalFile(ctx, client, fileRef.LocalPath, fileRef.MimeType)

	default:
		slog.Warn("Unknown file source type", "type", fileRef.SourceType)
		return nil
	}
}

// uploadOrConvertBetaLocalFile attempts to upload a local file via Files API for Beta API.
// If that fails, falls back to base64 encoding.
func uploadOrConvertBetaLocalFile(ctx context.Context, client *anthropic.Client, localPath, mimeType string) *anthropic.BetaContentBlockParamUnion {
	// Check cache first
	fileUploadCache.RLock()
	if fileID, ok := fileUploadCache.cache[localPath]; ok {
		fileUploadCache.RUnlock()
		slog.Debug("Using cached file ID", "path", localPath, "file_id", fileID)
		return &anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfFile: &anthropic.BetaFileImageSourceParam{
						FileID: fileID,
					},
				},
			},
		}
	}
	fileUploadCache.RUnlock()

	// Try to upload via Files API
	if client != nil {
		fileID, err := uploadFileToAnthropic(ctx, client, localPath)
		if err == nil {
			// Cache the file ID
			fileUploadCache.Lock()
			fileUploadCache.cache[localPath] = fileID
			fileUploadCache.Unlock()

			slog.Debug("Uploaded file to Anthropic Files API", "path", localPath, "file_id", fileID)
			return &anthropic.BetaContentBlockParamUnion{
				OfImage: &anthropic.BetaImageBlockParam{
					Source: anthropic.BetaImageBlockParamSourceUnion{
						OfFile: &anthropic.BetaFileImageSourceParam{
							FileID: fileID,
						},
					},
				},
			}
		}
		slog.Warn("Failed to upload file to Anthropic, falling back to base64", "path", localPath, "error", err)
	}

	// Fall back to base64
	return convertBetaLocalFileToBase64Block(localPath, mimeType)
}

// uploadFileToAnthropic uploads a file to Anthropic's Files API and returns the file ID
func uploadFileToAnthropic(ctx context.Context, client *anthropic.Client, localPath string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	params := anthropic.BetaFileUploadParams{
		File:  file,
		Betas: []anthropic.AnthropicBeta{anthropic.AnthropicBetaFilesAPI2025_04_14},
	}

	result, err := client.Beta.Files.Upload(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	return result.ID, nil
}

// convertBetaLocalFileToBase64Block reads a local file and converts it to a Beta API base64 image block
func convertBetaLocalFileToBase64Block(localPath, mimeType string) *anthropic.BetaContentBlockParamUnion {
	data, err := os.ReadFile(localPath)
	if err != nil {
		slog.Warn("Failed to read local file", "path", localPath, "error", err)
		return nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	if mimeType == "" {
		mimeType = "image/jpeg" // Default
	}

	slog.Debug("Converted local file to base64 (Beta API)", "path", localPath, "size", len(data))

	return &anthropic.BetaContentBlockParamUnion{
		OfImage: &anthropic.BetaImageBlockParam{
			Source: anthropic.BetaImageBlockParamSourceUnion{
				OfBase64: &anthropic.BetaBase64ImageSourceParam{
					Data:      encoded,
					MediaType: anthropic.BetaBase64ImageSourceMediaType(mimeType),
				},
			},
		},
	}
}

// convertBetaDataURLToImageBlock parses a data URL and converts it to a Beta API image block
func convertBetaDataURLToImageBlock(dataURL string) *anthropic.BetaContentBlockParamUnion {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil
	}

	mediaTypePart := parts[0]
	base64Data := parts[1]

	var mediaType string
	switch {
	case strings.Contains(mediaTypePart, "image/jpeg"):
		mediaType = "image/jpeg"
	case strings.Contains(mediaTypePart, "image/png"):
		mediaType = "image/png"
	case strings.Contains(mediaTypePart, "image/gif"):
		mediaType = "image/gif"
	case strings.Contains(mediaTypePart, "image/webp"):
		mediaType = "image/webp"
	default:
		mediaType = "image/jpeg"
	}

	return &anthropic.BetaContentBlockParamUnion{
		OfImage: &anthropic.BetaImageBlockParam{
			Source: anthropic.BetaImageBlockParamSourceUnion{
				OfBase64: &anthropic.BetaBase64ImageSourceParam{
					Data:      base64Data,
					MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
				},
			},
		},
	}
}
