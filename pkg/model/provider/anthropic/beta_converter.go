package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

// convertBetaMessages converts chat messages to Anthropic Beta API format
// Following Anthropic's extended thinking documentation with interleaved thinking enabled:
// - Thinking blocks can appear anywhere in the conversation (not required to be first)
// - Always include the complete, unmodified thinking block from previous assistant turns
// - interleaved parameter is kept for API compatibility but always true
//
// Important: Anthropic API requires that all tool_result blocks corresponding to tool_use
// blocks from the same assistant message MUST be grouped into a single user message.
func (c *Client) convertBetaMessages(ctx context.Context, messages []chat.Message) ([]anthropic.BetaMessageParam, error) {
	var betaMessages []anthropic.BetaMessageParam

	for i := 0; i < len(messages); i++ {
		msg := &messages[i]
		if msg.Role == chat.MessageRoleSystem {
			// System messages handled separately
			continue
		}
		if msg.Role == chat.MessageRoleUser {
			// Handle user messages (including images, files, and tool results)
			if len(msg.MultiContent) > 0 {
				contentBlocks, err := c.convertBetaUserMultiContent(ctx, msg.MultiContent)
				if err != nil {
					return nil, err
				}
				if len(contentBlocks) > 0 {
					betaMessages = append(betaMessages, anthropic.BetaMessageParam{
						Role:    anthropic.BetaMessageParamRoleUser,
						Content: contentBlocks,
					})
				}
			} else if txt := strings.TrimSpace(msg.Content); txt != "" {
				betaMessages = append(betaMessages, anthropic.BetaMessageParam{
					Role: anthropic.BetaMessageParamRoleUser,
					Content: []anthropic.BetaContentBlockParamUnion{
						{OfText: &anthropic.BetaTextBlockParam{Text: txt}},
					},
				})
			}
			continue
		}
		if msg.Role == chat.MessageRoleAssistant {
			contentBlocks := make([]anthropic.BetaContentBlockParamUnion, 0)

			// With interleaved thinking, we can include thinking blocks anywhere
			// If we have thinking content, include it first (conventional order)
			if msg.ReasoningContent != "" && msg.ThinkingSignature != "" {
				contentBlocks = append(contentBlocks,
					anthropic.NewBetaThinkingBlock(msg.ThinkingSignature, msg.ReasoningContent))
			} else if msg.ThinkingSignature != "" {
				// Include redacted thinking placeholder using the original signature
				contentBlocks = append(contentBlocks,
					anthropic.NewBetaRedactedThinkingBlock(msg.ThinkingSignature))
			}

			// Add text content if present
			if txt := strings.TrimSpace(msg.Content); txt != "" {
				contentBlocks = append(contentBlocks, anthropic.BetaContentBlockParamUnion{
					OfText: &anthropic.BetaTextBlockParam{Text: txt},
				})
			}

			// Add tool calls
			if len(msg.ToolCalls) > 0 {
				for _, toolCall := range msg.ToolCalls {
					var inpts map[string]any
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &inpts); err != nil {
						inpts = map[string]any{}
					}
					contentBlocks = append(contentBlocks, anthropic.BetaContentBlockParamUnion{
						OfToolUse: &anthropic.BetaToolUseBlockParam{
							ID:    toolCall.ID,
							Input: inpts,
							Name:  toolCall.Function.Name,
						},
					})
				}
			}

			if len(contentBlocks) > 0 {
				betaMessages = append(betaMessages, anthropic.BetaMessageParam{
					Role:    anthropic.BetaMessageParamRoleAssistant,
					Content: contentBlocks,
				})
			}
			continue
		}
		if msg.Role == chat.MessageRoleTool {
			// Collect consecutive tool messages and merge them into a single user message
			// This is required by Anthropic API: all tool_result blocks for tool_use blocks
			// from the same assistant message must be in the same user message
			toolResultBlocks := []anthropic.BetaContentBlockParamUnion{
				{
					OfToolResult: &anthropic.BetaToolResultBlockParam{
						ToolUseID: msg.ToolCallID,
						Content: []anthropic.BetaToolResultBlockParamContentUnion{
							{OfText: &anthropic.BetaTextBlockParam{Text: strings.TrimSpace(msg.Content)}},
						},
					},
				},
			}

			// Look ahead for consecutive tool messages and merge them
			j := i + 1
			for j < len(messages) && messages[j].Role == chat.MessageRoleTool {
				toolResultBlocks = append(toolResultBlocks, anthropic.BetaContentBlockParamUnion{
					OfToolResult: &anthropic.BetaToolResultBlockParam{
						ToolUseID: messages[j].ToolCallID,
						Content: []anthropic.BetaToolResultBlockParamContentUnion{
							{OfText: &anthropic.BetaTextBlockParam{Text: strings.TrimSpace(messages[j].Content)}},
						},
					},
				})
				j++
			}

			// Add the merged user message with all tool results
			betaMessages = append(betaMessages, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleUser,
				Content: toolResultBlocks,
			})

			// Skip the messages we've already processed
			i = j - 1
			continue
		}
	}

	// Add ephemeral cache to last 2 messages' last content block
	applyBetaMessageCacheControl(betaMessages)

	return betaMessages, nil
}

// convertBetaUserMultiContent converts user message multi-content parts to Beta API content blocks.
// It handles text, images (base64 and URL), and file uploads via the Files API.
func (c *Client) convertBetaUserMultiContent(ctx context.Context, parts []chat.MessagePart) ([]anthropic.BetaContentBlockParamUnion, error) {
	contentBlocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(parts))

	for _, part := range parts {
		switch part.Type {
		case chat.MessagePartTypeText:
			if txt := strings.TrimSpace(part.Text); txt != "" {
				contentBlocks = append(contentBlocks, anthropic.BetaContentBlockParamUnion{
					OfText: &anthropic.BetaTextBlockParam{Text: txt},
				})
			}

		case chat.MessagePartTypeImageURL:
			if part.ImageURL == nil {
				continue
			}
			// Handle base64 data URLs (legacy format)
			if strings.HasPrefix(part.ImageURL.URL, "data:") {
				urlParts := strings.SplitN(part.ImageURL.URL, ",", 2)
				if len(urlParts) == 2 {
					mediaTypePart := urlParts[0]
					base64Data := urlParts[1]

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

					contentBlocks = append(contentBlocks, anthropic.BetaContentBlockParamUnion{
						OfImage: &anthropic.BetaImageBlockParam{
							Source: anthropic.BetaImageBlockParamSourceUnion{
								OfBase64: &anthropic.BetaBase64ImageSourceParam{
									Data:      base64Data,
									MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
								},
							},
						},
					})
				}
			} else if strings.HasPrefix(part.ImageURL.URL, "http://") || strings.HasPrefix(part.ImageURL.URL, "https://") {
				// URL-based images
				contentBlocks = append(contentBlocks, anthropic.BetaContentBlockParamUnion{
					OfImage: &anthropic.BetaImageBlockParam{
						Source: anthropic.BetaImageBlockParamSourceUnion{
							OfURL: &anthropic.BetaURLImageSourceParam{
								URL: part.ImageURL.URL,
							},
						},
					},
				})
			}

		case chat.MessagePartTypeFile:
			if part.File == nil {
				continue
			}

			switch {
			case part.File.Path != "" && part.File.FileID == "":
				// Upload the file if we have a path but no file ID
				if c.fileManager == nil {
					return nil, fmt.Errorf("%w: cannot upload file %s", ErrFileManagerNotInitialized, part.File.Path)
				}

				uploaded, err := c.fileManager.GetOrUpload(ctx, part.File.Path)
				if err != nil {
					return nil, fmt.Errorf("failed to upload file %s: %w", part.File.Path, err)
				}

				block, err := createBetaFileContentBlock(uploaded.FileID, uploaded.MimeType)
				if err != nil {
					return nil, err
				}
				contentBlocks = append(contentBlocks, block)

			case part.File.FileID != "":
				// File already uploaded, use the ID directly
				block, err := createBetaFileContentBlock(part.File.FileID, part.File.MimeType)
				if err != nil {
					return nil, err
				}
				contentBlocks = append(contentBlocks, block)

			default:
				// File part has neither path nor file ID - this is invalid
				return nil, fmt.Errorf("invalid file attachment: neither path nor file_id provided")
			}
		}
	}

	return contentBlocks, nil
}

// createBetaFileContentBlock creates the appropriate Beta API content block for a file.
func createBetaFileContentBlock(fileID, mimeType string) (anthropic.BetaContentBlockParamUnion, error) {
	if IsImageMime(mimeType) {
		return anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfFile: &anthropic.BetaFileImageSourceParam{
						FileID: fileID,
					},
				},
			},
		}, nil
	}

	if IsAnthropicDocumentMime(mimeType) {
		return anthropic.BetaContentBlockParamUnion{
			OfDocument: &anthropic.BetaRequestDocumentBlockParam{
				Source: anthropic.BetaRequestDocumentBlockSourceUnionParam{
					OfFile: &anthropic.BetaFileDocumentSourceParam{
						FileID: fileID,
					},
				},
			},
		}, nil
	}

	return anthropic.BetaContentBlockParamUnion{}, fmt.Errorf("%w: %s", ErrUnsupportedFileType, mimeType)
}

// extractBetaSystemBlocks extracts system messages for Beta API format
func extractBetaSystemBlocks(messages []chat.Message) []anthropic.BetaTextBlockParam {
	regularBlocks := extractSystemBlocks(messages)

	betaBlocks := make([]anthropic.BetaTextBlockParam, len(regularBlocks))
	for i, block := range regularBlocks {
		betaBlocks[i] = anthropic.BetaTextBlockParam{Text: block.Text}

		// Copy over cache control from regular blocks (already set on first 2)
		if block.CacheControl.Type != "" {
			betaBlocks[i].CacheControl = anthropic.BetaCacheControlEphemeralParam{
				Type: block.CacheControl.Type,
				TTL:  anthropic.BetaCacheControlEphemeralTTL(block.CacheControl.TTL),
			}
		}
	}

	return betaBlocks
}

// convertBetaTools converts tools to Beta API format
func convertBetaTools(t []tools.Tool) ([]anthropic.BetaToolUnionParam, error) {
	betaTools := make([]anthropic.BetaToolUnionParam, len(t))

	for i, tool := range t {
		inputSchema, err := ConvertParametersToSchema(tool.Parameters)
		if err != nil {
			return nil, err
		}

		// Convert to BetaToolInputSchemaParam
		var betaInputSchema anthropic.BetaToolInputSchemaParam
		if err := tools.ConvertSchema(inputSchema, &betaInputSchema); err != nil {
			return nil, err
		}

		// Create BetaToolParam and wrap it in BetaToolUnionParam
		betaTools[i] = anthropic.BetaToolUnionParam{
			OfTool: &anthropic.BetaToolParam{
				Name:        tool.Name,
				Description: anthropic.String(tool.Description),
				InputSchema: betaInputSchema,
			},
		}
	}

	return betaTools, nil
}

// applyBetaMessageCacheControl adds ephemeral cache control to the last content block
// of the last 2 messages for prompt caching.
func applyBetaMessageCacheControl(messages []anthropic.BetaMessageParam) {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-2; i-- {
		msg := &messages[i]
		if len(msg.Content) == 0 {
			continue
		}
		lastIdx := len(msg.Content) - 1
		block := &msg.Content[lastIdx]
		cacheCtrl := anthropic.NewBetaCacheControlEphemeralParam()
		switch {
		case block.OfText != nil:
			block.OfText.CacheControl = cacheCtrl
		case block.OfToolUse != nil:
			block.OfToolUse.CacheControl = cacheCtrl
		case block.OfToolResult != nil:
			block.OfToolResult.CacheControl = cacheCtrl
		case block.OfImage != nil:
			block.OfImage.CacheControl = cacheCtrl
		case block.OfDocument != nil:
			block.OfDocument.CacheControl = cacheCtrl
		}
	}
}
