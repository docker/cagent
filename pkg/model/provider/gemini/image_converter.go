package gemini

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/docker/cagent/pkg/chat"
)

const cacheExpiration = 2 * time.Hour // Gemini files expire after 48h server-side

type cacheEntry struct {
	fileURI    string
	uploadedAt time.Time
}

// fileUploadCache caches file URIs to avoid re-uploading the same file.
// Entries expire after 24 hours since Gemini auto-deletes files after 48 hours.
var fileUploadCache = struct {
	sync.RWMutex
	cache map[string]cacheEntry
}{cache: make(map[string]cacheEntry)}

// convertImageURLToPart converts an image URL to a Gemini Part.
// It handles file references (uploading via Files API if possible),
// base64 data URLs, and local files.
func convertImageURLToPartWithClient(ctx context.Context, client *genai.Client, imageURL *chat.MessageImageURL) *genai.Part {
	if imageURL == nil {
		return nil
	}

	// Handle file reference (from /attach command)
	if imageURL.FileRef != nil {
		return convertFileRefToPart(ctx, client, imageURL.FileRef)
	}

	// Handle data URL (base64)
	if strings.HasPrefix(imageURL.URL, "data:") {
		return convertDataURLToPart(imageURL.URL)
	}

	// Handle HTTP(S) URL - Gemini can fetch from URLs
	if strings.HasPrefix(imageURL.URL, "http://") || strings.HasPrefix(imageURL.URL, "https://") {
		return genai.NewPartFromURI(imageURL.URL, extractMimeTypeFromURL(imageURL.URL))
	}

	return nil
}

// convertFileRefToPart handles file references, uploading via Files API if available
func convertFileRefToPart(ctx context.Context, client *genai.Client, fileRef *chat.FileReference) *genai.Part {
	if fileRef == nil {
		return nil
	}

	switch fileRef.SourceType {
	case chat.FileSourceTypeFileURI:
		// Already uploaded to Gemini, use URI directly
		slog.Debug("Using existing file URI", "uri", fileRef.FileURI)
		return genai.NewPartFromURI(fileRef.FileURI, fileRef.MimeType)

	case chat.FileSourceTypeFileID:
		// File ID from another provider - need to upload to Gemini
		slog.Warn("File ID from another provider not supported for Gemini, skipping", "file_id", fileRef.FileID)
		return nil

	case chat.FileSourceTypeLocalPath:
		// Try to upload via Files API, fall back to base64
		return uploadOrConvertLocalFile(ctx, client, fileRef.LocalPath, fileRef.MimeType)

	default:
		slog.Warn("Unknown file source type", "type", fileRef.SourceType)
		return nil
	}
}

// uploadOrConvertLocalFile attempts to upload a local file via Gemini Files API.
// If that fails, falls back to reading the file and sending as bytes.
func uploadOrConvertLocalFile(ctx context.Context, client *genai.Client, localPath, mimeType string) *genai.Part {
	// Check cache first
	fileUploadCache.RLock()
	if entry, ok := fileUploadCache.cache[localPath]; ok && time.Since(entry.uploadedAt) < cacheExpiration {
		fileUploadCache.RUnlock()
		slog.Debug("Using cached file URI", "path", localPath, "uri", entry.fileURI)
		return genai.NewPartFromURI(entry.fileURI, mimeType)
	}
	fileUploadCache.RUnlock()

	// Try to upload via Files API
	if client != nil {
		fileURI, err := uploadFileToGemini(ctx, client, localPath, mimeType)
		if err == nil {
			fileUploadCache.Lock()
			fileUploadCache.cache[localPath] = cacheEntry{fileURI: fileURI, uploadedAt: time.Now()}
			fileUploadCache.Unlock()

			slog.Debug("Uploaded file to Gemini Files API", "path", localPath, "uri", fileURI)
			return genai.NewPartFromURI(fileURI, mimeType)
		}
		slog.Warn("Failed to upload file to Gemini, falling back to bytes", "path", localPath, "error", err)
	}

	// Fall back to reading file and sending as bytes
	return convertLocalFileToBytesPart(localPath, mimeType)
}

// uploadFileToGemini uploads a file to Gemini's Files API and returns the file URI
func uploadFileToGemini(ctx context.Context, client *genai.Client, localPath, mimeType string) (string, error) {
	config := &genai.UploadFileConfig{
		MIMEType: mimeType,
	}

	file, err := client.Files.UploadFromPath(ctx, localPath, config)
	if err != nil {
		return "", err
	}

	return file.URI, nil
}

// convertLocalFileToBytesPart reads a local file and converts it to a bytes Part
func convertLocalFileToBytesPart(localPath, mimeType string) *genai.Part {
	data, err := os.ReadFile(localPath)
	if err != nil {
		slog.Warn("Failed to read local file", "path", localPath, "error", err)
		return nil
	}

	if mimeType == "" {
		mimeType = "image/jpeg" // Default
	}

	slog.Debug("Converted local file to bytes", "path", localPath, "size", len(data))
	return genai.NewPartFromBytes(data, mimeType)
}

// convertDataURLToPart parses a data URL and converts it to a Gemini Part
func convertDataURLToPart(dataURL string) *genai.Part {
	// Parse data URL format: data:[<mediatype>][;base64],<data>
	urlParts := strings.SplitN(dataURL, ",", 2)
	if len(urlParts) != 2 {
		return nil
	}

	imageData, err := base64.StdEncoding.DecodeString(urlParts[1])
	if err != nil {
		return nil
	}

	mimeType := extractMimeType(urlParts[0])
	return genai.NewPartFromBytes(imageData, mimeType)
}

// extractMimeTypeFromURL tries to determine MIME type from a URL
func extractMimeTypeFromURL(url string) string {
	lowerURL := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lowerURL, ".jpg"), strings.HasSuffix(lowerURL, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lowerURL, ".png"):
		return "image/png"
	case strings.HasSuffix(lowerURL, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lowerURL, ".webp"):
		return "image/webp"
	default:
		return "image/jpeg" // Default
	}
}
