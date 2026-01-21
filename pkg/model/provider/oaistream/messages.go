package oaistream

/*
This file contains shared message conversion utilities for OpenAI-compatible providers.
*/

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"

	"github.com/docker/cagent/pkg/chat"
)

// JSONSchema is a helper type that implements json.Marshaler for map[string]any.
// This allows us to pass schema maps to the OpenAI library which expects json.Marshaler.
type JSONSchema map[string]any

// MarshalJSON implements json.Marshaler for JSONSchema.
func (j JSONSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any(j))
}

// ConvertMultiContent converts chat.MessagePart slices to OpenAI content parts.
// This now handles file references properly by using the file-aware converter.
func ConvertMultiContent(multiContent []chat.MessagePart) []openai.ChatCompletionContentPartUnionParam {
	return ConvertMultiContentWithFileSupport(multiContent)
}

// ConvertMessages converts chat.Message slices to OpenAI message params.
// This is the base conversion without any provider-specific post-processing.
func ConvertMessages(messages []chat.Message) []openai.ChatCompletionMessageParamUnion {
	openaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for i := range messages {
		msg := &messages[i]

		// Skip invalid assistant messages upfront. This can happen if the model is out of tokens (max_tokens reached)
		if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) == 0 && len(msg.MultiContent) == 0 && strings.TrimSpace(msg.Content) == "" {
			continue
		}

		var openaiMessage openai.ChatCompletionMessageParamUnion

		switch msg.Role {
		case chat.MessageRoleSystem:
			if len(msg.MultiContent) == 0 {
				openaiMessage = openai.SystemMessage(msg.Content)
			} else {
				// Convert multi-content for system messages
				textParts := make([]openai.ChatCompletionContentPartTextParam, 0)
				for _, part := range msg.MultiContent {
					if part.Type == chat.MessagePartTypeText {
						textParts = append(textParts, openai.ChatCompletionContentPartTextParam{
							Text: part.Text,
						})
					}
				}
				openaiMessage = openai.SystemMessage(textParts)
			}

		case chat.MessageRoleUser:
			if len(msg.MultiContent) == 0 {
				openaiMessage = openai.UserMessage(msg.Content)
			} else {
				openaiMessage = openai.UserMessage(ConvertMultiContent(msg.MultiContent))
			}

		case chat.MessageRoleAssistant:
			assistantParam := openai.ChatCompletionAssistantMessageParam{}

			if len(msg.MultiContent) == 0 {
				if msg.Content != "" {
					assistantParam.Content.OfString = param.NewOpt(msg.Content)
				}
			} else {
				// Convert multi-content for assistant messages
				contentParts := make([]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion, 0)
				for _, part := range msg.MultiContent {
					if part.Type == chat.MessagePartTypeText {
						contentParts = append(contentParts, openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion{
							OfText: &openai.ChatCompletionContentPartTextParam{
								Text: part.Text,
							},
						})
					}
				}
				if len(contentParts) > 0 {
					assistantParam.Content.OfArrayOfContentParts = contentParts
				}
			}

			if msg.FunctionCall != nil {
				assistantParam.FunctionCall.Name = msg.FunctionCall.Name           //nolint:staticcheck // deprecated but still needed for compatibility
				assistantParam.FunctionCall.Arguments = msg.FunctionCall.Arguments //nolint:staticcheck // deprecated but still needed for compatibility
			}

			if len(msg.ToolCalls) > 0 {
				toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, len(msg.ToolCalls))
				for j, toolCall := range msg.ToolCalls {
					toolCalls[j] = openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: toolCall.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      toolCall.Function.Name,
								Arguments: toolCall.Function.Arguments,
							},
						},
					}
				}
				assistantParam.ToolCalls = toolCalls
			}

			openaiMessage.OfAssistant = &assistantParam

		case chat.MessageRoleTool:
			toolParam := openai.ChatCompletionToolMessageParam{
				ToolCallID: msg.ToolCallID,
			}

			if len(msg.MultiContent) == 0 {
				toolParam.Content.OfString = param.NewOpt(msg.Content)
			} else {
				// Convert multi-content for tool messages
				textParts := make([]openai.ChatCompletionContentPartTextParam, 0)
				for _, part := range msg.MultiContent {
					if part.Type == chat.MessagePartTypeText {
						textParts = append(textParts, openai.ChatCompletionContentPartTextParam{
							Text: part.Text,
						})
					}
				}
				toolParam.Content.OfArrayOfContentParts = textParts
			}

			openaiMessage.OfTool = &toolParam
		}

		openaiMessages = append(openaiMessages, openaiMessage)
	}
	return openaiMessages
}

// getMessageRole returns the role of a message as a string.
// Returns empty string if role cannot be determined.
func getMessageRole(msg openai.ChatCompletionMessageParamUnion) string {
	if msg.OfSystem != nil {
		return "system"
	}
	if msg.OfUser != nil {
		return "user"
	}
	if msg.OfAssistant != nil {
		return "assistant"
	}
	if msg.OfTool != nil {
		return "tool"
	}
	return ""
}

// getStringContent extracts string content from a message, if available.
// Returns empty string if content is multi-part or not a string.
func getStringContent(msg openai.ChatCompletionMessageParamUnion) (string, bool) {
	if msg.OfSystem != nil {
		if str := msg.OfSystem.Content.OfString.Value; str != "" {
			return str, true
		}
	}
	if msg.OfUser != nil {
		if str := msg.OfUser.Content.OfString.Value; str != "" {
			return str, true
		}
	}
	return "", false
}

// getMultiContent extracts multi-part content from a message, if available.
func getMultiContent(msg openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionContentPartUnionParam {
	if msg.OfUser != nil && len(msg.OfUser.Content.OfArrayOfContentParts) > 0 {
		return msg.OfUser.Content.OfArrayOfContentParts
	}
	return nil
}

// getSystemTextParts extracts text parts from a system message.
func getSystemTextParts(msg openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionContentPartTextParam {
	if msg.OfSystem != nil && len(msg.OfSystem.Content.OfArrayOfContentParts) > 0 {
		return msg.OfSystem.Content.OfArrayOfContentParts
	}
	return nil
}

// MergeConsecutiveMessages merges consecutive system or user messages into single messages.
// This is needed by some local models (like those run by DMR) that don't handle
// consecutive same-role messages well.
func MergeConsecutiveMessages(openaiMessages []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	var mergedMessages []openai.ChatCompletionMessageParamUnion

	for i := 0; i < len(openaiMessages); i++ {
		currentMsg := openaiMessages[i]
		currentRole := getMessageRole(currentMsg)

		// Only merge system or user messages
		if currentRole == "system" || currentRole == "user" {
			var mergedContent string
			var mergedMultiContent []openai.ChatCompletionContentPartUnionParam
			j := i

			// Collect all consecutive messages with the same role
			for j < len(openaiMessages) {
				msgToMerge := openaiMessages[j]
				msgRole := getMessageRole(msgToMerge)
				if msgRole != currentRole {
					break
				}

				// Extract content
				if str, ok := getStringContent(msgToMerge); ok {
					if mergedContent != "" {
						mergedContent += "\n"
					}
					mergedContent += str
				} else if parts := getMultiContent(msgToMerge); parts != nil {
					mergedMultiContent = append(mergedMultiContent, parts...)
				} else if textParts := getSystemTextParts(msgToMerge); textParts != nil {
					// Convert text parts to union params
					for _, textPart := range textParts {
						mergedMultiContent = append(mergedMultiContent, openai.ChatCompletionContentPartUnionParam{
							OfText: &openai.ChatCompletionContentPartTextParam{
								Text: textPart.Text,
							},
						})
					}
				}
				j++
			}

			// Create the merged message
			var mergedMessage openai.ChatCompletionMessageParamUnion
			if currentRole == "system" {
				if len(mergedMultiContent) == 0 {
					mergedMessage = openai.SystemMessage(mergedContent)
				} else {
					textParts := make([]openai.ChatCompletionContentPartTextParam, 0)
					for _, part := range mergedMultiContent {
						if part.OfText != nil {
							textParts = append(textParts, *part.OfText)
						}
					}
					mergedMessage = openai.SystemMessage(textParts)
				}
			} else {
				if len(mergedMultiContent) == 0 {
					mergedMessage = openai.UserMessage(mergedContent)
				} else {
					mergedMessage = openai.UserMessage(mergedMultiContent)
				}
			}

			mergedMessages = append(mergedMessages, mergedMessage)
			i = j - 1
		} else {
			mergedMessages = append(mergedMessages, currentMsg)
		}
	}

	return mergedMessages
}
