package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/tools"
)

// Limits applied to inbound sampling requests to keep a misbehaving or
// malicious MCP server from inflating host memory / token spend without
// any natural backpressure.
const (
	// maxSamplingMessages caps the number of conversation turns we accept
	// from a single sampling/createMessage request.
	maxSamplingMessages = 256
	// maxSamplingTextBytes caps the size of an individual text block
	// (including the system prompt) before we refuse the request.
	maxSamplingTextBytes = 1 << 20 // 1 MiB
	// maxSamplingBinaryBytes caps the size of an individual image/audio
	// block before we refuse to inline it as a data URL.
	maxSamplingBinaryBytes = 8 << 20 // 8 MiB
	// maxSamplingTools caps the number of tool definitions a server can
	// inject into a single sampling-with-tools request.
	maxSamplingTools = 64
	// maxSamplingToolCalls caps the number of tool calls we will return
	// from a single sampling-with-tools completion. Per-call argument size
	// is bounded by maxSamplingTextBytes.
	maxSamplingToolCalls = 32
)

// samplingHandler is the MCP-toolset-side hook that satisfies an inbound
// sampling/createMessage request from a server by driving the host agent's
// own model and returning the resulting message.
//
// The host always remains in control: the request is mapped to the agent's
// configured model (server-supplied ModelPreferences are advisory only),
// only one round-trip is performed (the model's response is returned
// verbatim, not fed back into the loop), and tool use is intentionally
// disabled — sampling is for plain text/image/audio completions, not
// nested agent runs. Per-block size and per-request message-count limits
// keep an unbounded server response from pinning host memory.
func (r *LocalRuntime) samplingHandler(ctx context.Context, req *mcp.CreateMessageParams) (*mcp.CreateMessageResult, error) {
	if req == nil {
		return nil, errors.New("sampling request is nil")
	}

	slog.InfoContext(ctx, "Sampling request received from MCP server",
		"messages", len(req.Messages),
		"max_tokens", req.MaxTokens,
		"system_prompt", req.SystemPrompt != "",
	)

	a := r.CurrentAgent()
	if a == nil {
		return nil, errors.New("no current agent available to handle sampling request")
	}

	messages, err := samplingMessagesToChat(req)
	if err != nil {
		return nil, fmt.Errorf("converting sampling messages: %w", err)
	}

	baseModel := a.Model(ctx)
	if baseModel == nil {
		return nil, errors.New("current agent has no model configured")
	}

	model, err := provider.Clone(ctx, baseModel, samplingModelOptions(req)...)
	if err != nil {
		return nil, fmt.Errorf("cloning model for sampling: %w", err)
	}

	stream, err := model.CreateChatCompletionStream(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("creating sampling completion stream: %w", err)
	}

	content, finishReason, err := drainSamplingStream(stream)
	if err != nil {
		return nil, fmt.Errorf("reading sampling completion stream: %w", err)
	}

	slog.DebugContext(ctx, "Sampling request completed",
		"agent", a.Name(),
		"model", model.ID().String(),
		"finish_reason", finishReason,
		"content_bytes", len(content),
	)

	return &mcp.CreateMessageResult{
		Role:       mcp.Role("assistant"),
		Model:      model.ID().String(),
		Content:    &mcp.TextContent{Text: content},
		StopReason: stopReason(finishReason),
	}, nil
}

// samplingMessagesToChat converts an MCP CreateMessageParams into the
// host's chat.Message slice. The optional system prompt is prepended;
// per-message Content is mapped from the supported MCP block types.
// Oversized payloads and nil/unsupported entries surface as errors so
// the request is rejected rather than silently truncated.
func samplingMessagesToChat(req *mcp.CreateMessageParams) ([]chat.Message, error) {
	if len(req.Messages) == 0 {
		return nil, errors.New("sampling request contains no messages")
	}
	if len(req.Messages) > maxSamplingMessages {
		return nil, fmt.Errorf("sampling request contains %d messages (limit %d)",
			len(req.Messages), maxSamplingMessages)
	}

	messages := make([]chat.Message, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		if len(req.SystemPrompt) > maxSamplingTextBytes {
			return nil, fmt.Errorf("sampling system prompt is too large (%d bytes, limit %d)",
				len(req.SystemPrompt), maxSamplingTextBytes)
		}
		messages = append(messages, chat.Message{
			Role:    chat.MessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}
	for i, m := range req.Messages {
		if m == nil {
			return nil, fmt.Errorf("sampling message at index %d is nil", i)
		}
		role, err := samplingRoleToChat(m.Role)
		if err != nil {
			return nil, err
		}
		text, parts, err := samplingContentToChat(m.Content)
		if err != nil {
			return nil, fmt.Errorf("sampling message at index %d: %w", i, err)
		}
		messages = append(messages, chat.Message{
			Role:         role,
			Content:      text,
			MultiContent: parts,
		})
	}
	return messages, nil
}

func samplingRoleToChat(r mcp.Role) (chat.MessageRole, error) {
	switch string(r) {
	case "user":
		return chat.MessageRoleUser, nil
	case "assistant":
		return chat.MessageRoleAssistant, nil
	case "":
		// Some servers omit the role for the lone user turn; default to user
		// rather than refuse the request, matching most other MCP hosts.
		return chat.MessageRoleUser, nil
	default:
		return "", fmt.Errorf("unsupported sampling role %q", r)
	}
}

// samplingContentToChat maps a single MCP content block to the host's
// chat representation. Text blocks return a Content string; image blocks
// return a MultiContent entry with a data URL the model can consume.
// Audio blocks fall back to a textual placeholder because chat.Message
// does not currently model raw audio; this lets models acknowledge the
// attachment instead of failing the request outright. Oversized blocks
// are rejected so a malicious or buggy server can't pin large blobs in
// host memory.
func samplingContentToChat(c mcp.Content) (string, []chat.MessagePart, error) {
	switch v := c.(type) {
	case *mcp.TextContent:
		if len(v.Text) > maxSamplingTextBytes {
			return "", nil, fmt.Errorf("text block too large (%d bytes, limit %d)",
				len(v.Text), maxSamplingTextBytes)
		}
		return v.Text, nil, nil
	case *mcp.ImageContent:
		if len(v.Data) > maxSamplingBinaryBytes {
			return "", nil, fmt.Errorf("image block too large (%d bytes, limit %d)",
				len(v.Data), maxSamplingBinaryBytes)
		}
		return "", []chat.MessagePart{{
			Type: chat.MessagePartTypeImageURL,
			ImageURL: &chat.MessageImageURL{
				URL: dataURL(v.MIMEType, v.Data),
			},
		}}, nil
	case *mcp.AudioContent:
		if len(v.Data) > maxSamplingBinaryBytes {
			return "", nil, fmt.Errorf("audio block too large (%d bytes, limit %d)",
				len(v.Data), maxSamplingBinaryBytes)
		}
		return fmt.Sprintf("[audio attachment (%s, %d bytes) — not inlined]",
			v.MIMEType, len(v.Data)), nil, nil
	case nil:
		return "", nil, nil
	default:
		return fmt.Sprintf("[unsupported content type %T]", v), nil, nil
	}
}

func dataURL(mimeType string, data []byte) string {
	mt := mimeType
	if mt == "" {
		mt = "application/octet-stream"
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// samplingModelOptions translates the server's advisory preferences into
// the host's model options. Only MaxTokens is honoured today (with an
// upper bound enforced by the underlying provider); temperature, stop
// sequences, and ModelPreferences are intentionally left to the host's
// configuration. Structured output is explicitly cleared so a request
// cannot inherit the agent's JSON-schema response format and silently
// reshape the model's reply into something the MCP server didn't ask
// for.
func samplingModelOptions(req *mcp.CreateMessageParams) []options.Opt {
	return samplingModelOptionsFor(req.MaxTokens)
}

// samplingModelOptionsFor returns the per-request model options shared by the
// basic and with-tools sampling handlers. Structured output is cleared so a
// request cannot inherit the agent's JSON-schema response format; thinking is
// disabled because sampling is a delegated one-shot call rather than an agent
// turn; MaxTokens is honoured when non-zero.
func samplingModelOptionsFor(maxTokens int64) []options.Opt {
	opts := []options.Opt{
		options.WithStructuredOutput(nil),
		options.WithNoThinking(),
	}
	if maxTokens > 0 {
		opts = append(opts, options.WithMaxTokens(maxTokens))
	}
	return opts
}

// drainSamplingStream reads a chat completion stream to completion and
// returns the concatenated assistant content alongside the final finish
// reason. The stream is always closed before returning.
func drainSamplingStream(stream chat.MessageStream) (string, chat.FinishReason, error) {
	defer stream.Close()

	var content strings.Builder
	var finishReason chat.FinishReason
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return content.String(), finishReason, nil
		}
		if err != nil {
			return "", "", err
		}
		if len(response.Choices) > 0 {
			choice := response.Choices[0]
			content.WriteString(choice.Delta.Content)
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
	}
}

// stopReason maps a chat finish reason into the MCP stopReason vocabulary
// used in CreateMessageResult. Unknown values fall back to "endTurn",
// which is the protocol's default for a normal assistant turn.
func stopReason(fr chat.FinishReason) string {
	switch fr {
	case chat.FinishReasonStop:
		return "endTurn"
	case chat.FinishReasonLength:
		return "maxTokens"
	case chat.FinishReasonToolCalls:
		return "toolUse"
	default:
		return "endTurn"
	}
}

// samplingWithToolsHandler is the MCP-toolset-side hook that satisfies an
// inbound sampling/createMessage request that carries a tools array. It
// forwards the server-supplied tool definitions to the host's model and
// returns any tool_use blocks the model emits; the requesting MCP server
// then executes the tool and continues the loop in a follow-up sampling
// request with tool_result blocks added.
//
// The host never executes the server-supplied tools itself — they exist
// only to inform the model's response. The placeholder handler attached to
// each converted tool surfaces an error if a downstream call site mistakes
// these for ordinary agent tools.
func (r *LocalRuntime) samplingWithToolsHandler(ctx context.Context, req *mcp.CreateMessageWithToolsParams) (*mcp.CreateMessageWithToolsResult, error) {
	if req == nil {
		return nil, errors.New("sampling request is nil")
	}
	if len(req.Tools) > maxSamplingTools {
		return nil, fmt.Errorf("sampling request includes %d tools (limit %d)",
			len(req.Tools), maxSamplingTools)
	}

	slog.InfoContext(ctx, "Sampling-with-tools request received from MCP server",
		"messages", len(req.Messages),
		"tools", len(req.Tools),
		"max_tokens", req.MaxTokens,
		"system_prompt", req.SystemPrompt != "",
	)

	a := r.CurrentAgent()
	if a == nil {
		return nil, errors.New("no current agent available to handle sampling request")
	}

	messages, err := samplingMessagesV2ToChat(req)
	if err != nil {
		return nil, fmt.Errorf("converting sampling messages: %w", err)
	}

	chatTools := samplingToolsToChat(req.Tools)

	baseModel := a.Model(ctx)
	if baseModel == nil {
		return nil, errors.New("current agent has no model configured")
	}

	model, err := provider.Clone(ctx, baseModel, samplingModelOptionsFor(req.MaxTokens)...)
	if err != nil {
		return nil, fmt.Errorf("cloning model for sampling: %w", err)
	}

	stream, err := model.CreateChatCompletionStream(ctx, messages, chatTools)
	if err != nil {
		return nil, fmt.Errorf("creating sampling completion stream: %w", err)
	}

	text, toolCalls, finishReason, err := drainSamplingStreamWithTools(stream)
	if err != nil {
		return nil, fmt.Errorf("reading sampling completion stream: %w", err)
	}

	if len(toolCalls) > maxSamplingToolCalls {
		return nil, fmt.Errorf("model emitted %d tool calls (limit %d)",
			len(toolCalls), maxSamplingToolCalls)
	}

	sr := stopReason(finishReason)
	if len(toolCalls) > 0 {
		sr = "toolUse"
	}

	slog.DebugContext(ctx, "Sampling-with-tools request completed",
		"agent", a.Name(),
		"model", model.ID().String(),
		"finish_reason", finishReason,
		"stop_reason", sr,
		"tool_calls", len(toolCalls),
		"content_bytes", len(text),
	)

	return &mcp.CreateMessageWithToolsResult{
		Role:       mcp.Role("assistant"),
		Model:      model.ID().String(),
		Content:    buildSamplingWithToolsContent(text, toolCalls),
		StopReason: sr,
	}, nil
}

// samplingMessagesV2ToChat converts a CreateMessageWithToolsParams (V2
// messages with multi-block content) into chat.Messages. The optional system
// prompt is prepended; per-message blocks are folded into one or more chat
// messages depending on which content types are present.
func samplingMessagesV2ToChat(req *mcp.CreateMessageWithToolsParams) ([]chat.Message, error) {
	if len(req.Messages) == 0 {
		return nil, errors.New("sampling request contains no messages")
	}
	if len(req.Messages) > maxSamplingMessages {
		return nil, fmt.Errorf("sampling request contains %d messages (limit %d)",
			len(req.Messages), maxSamplingMessages)
	}

	messages := make([]chat.Message, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		if len(req.SystemPrompt) > maxSamplingTextBytes {
			return nil, fmt.Errorf("sampling system prompt is too large (%d bytes, limit %d)",
				len(req.SystemPrompt), maxSamplingTextBytes)
		}
		messages = append(messages, chat.Message{
			Role:    chat.MessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}
	for i, m := range req.Messages {
		if m == nil {
			return nil, fmt.Errorf("sampling message at index %d is nil", i)
		}
		role, err := samplingRoleToChat(m.Role)
		if err != nil {
			return nil, err
		}
		converted, err := samplingV2BlocksToMessages(role, m.Content)
		if err != nil {
			return nil, fmt.Errorf("sampling message at index %d: %w", i, err)
		}
		messages = append(messages, converted...)
	}
	return messages, nil
}

// samplingV2BlocksToMessages converts a single V2 message's content blocks
// into one or more chat.Messages. Plain blocks (text, image, audio) collapse
// into a single message at the supplied role; tool_use blocks attach as
// ToolCalls on an assistant message; tool_result blocks expand into one
// MessageRoleTool row per result (matching how chat history represents
// parallel tool calls).
func samplingV2BlocksToMessages(role chat.MessageRole, blocks []mcp.Content) ([]chat.Message, error) {
	var text strings.Builder
	var parts []chat.MessagePart
	var toolCalls []tools.ToolCall
	var toolResults []chat.Message

	for _, c := range blocks {
		switch v := c.(type) {
		case nil:
			continue
		case *mcp.ToolUseContent:
			args, err := json.Marshal(v.Input)
			if err != nil {
				args = []byte("{}")
			}
			toolCalls = append(toolCalls, tools.ToolCall{
				ID:   v.ID,
				Type: "function",
				Function: tools.FunctionCall{
					Name:      v.Name,
					Arguments: string(args),
				},
			})
		case *mcp.ToolResultContent:
			resultText, err := samplingToolResultText(v.Content)
			if err != nil {
				return nil, fmt.Errorf("tool_result content: %w", err)
			}
			toolResults = append(toolResults, chat.Message{
				Role:       chat.MessageRoleTool,
				Content:    resultText,
				ToolCallID: v.ToolUseID,
				IsError:    v.IsError,
			})
		default:
			t, p, err := samplingContentToChat(c)
			if err != nil {
				return nil, err
			}
			if t != "" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(t)
			}
			parts = append(parts, p...)
		}
	}

	// The MCP spec places tool_use blocks on assistant turns. A malformed
	// server sending them under any other role would otherwise be silently
	// dropped here (the output condition below would skip the message),
	// leaving the chat history shorter than the server expects.
	if len(toolCalls) > 0 && role != chat.MessageRoleAssistant {
		return nil, fmt.Errorf("tool_use blocks in non-assistant message (role=%s)", role)
	}

	var out []chat.Message
	if text.Len() > 0 || len(parts) > 0 || len(toolCalls) > 0 {
		msg := chat.Message{
			Role:    role,
			Content: text.String(),
		}
		if len(parts) > 0 {
			msg.MultiContent = parts
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		out = append(out, msg)
	}
	out = append(out, toolResults...)
	return out, nil
}

// samplingToolResultText flattens the nested content of a tool_result block
// into a single text string. chat.MessageRoleTool messages don't carry
// multi-part content, so non-text blocks render as a placeholder.
func samplingToolResultText(blocks []mcp.Content) (string, error) {
	var b strings.Builder
	var nonText int
	for _, c := range blocks {
		t, parts, err := samplingContentToChat(c)
		if err != nil {
			return "", err
		}
		if t != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(t)
		}
		nonText += len(parts)
	}
	if b.Len() == 0 && nonText > 0 {
		b.WriteString("[tool returned non-text content]")
	}
	return b.String(), nil
}

// samplingToolsToChat converts the server-supplied MCP tool definitions into
// the host's tools.Tool representation so the model can be told which tools
// it may call. The Handler is a no-op: the LLM's tool_use response is sent
// back to the requesting MCP server for execution, never invoked here.
func samplingToolsToChat(mcpTools []*mcp.Tool) []tools.Tool {
	if len(mcpTools) == 0 {
		return nil
	}
	out := make([]tools.Tool, 0, len(mcpTools))
	for _, t := range mcpTools {
		if t == nil {
			continue
		}
		out = append(out, tools.Tool{
			Name:         t.Name,
			Category:     "mcp-sampling",
			Description:  t.Description,
			Parameters:   t.InputSchema,
			OutputSchema: t.OutputSchema,
			Handler:      noOpSamplingToolHandler,
		})
	}
	return out
}

func noOpSamplingToolHandler(_ context.Context, _ tools.ToolCall, _ tools.Runtime) (*tools.ToolCallResult, error) {
	return tools.ResultError("sampling tool execution belongs to the requesting MCP server"), nil
}

// drainSamplingStreamWithTools reads a chat completion stream to completion
// and returns the concatenated assistant text, aggregated tool calls, and
// the final finish reason. It mirrors the tool-call aggregation in
// pkg/runtime/streaming.go::handleStream but omits agent events, telemetry,
// session bookkeeping, and the XML fallback — none of which apply to a
// one-shot delegated completion.
func drainSamplingStreamWithTools(stream chat.MessageStream) (string, []tools.ToolCall, chat.FinishReason, error) {
	defer stream.Close()

	var text strings.Builder
	var toolCalls []tools.ToolCall
	toolIndex := make(map[string]int)
	var providerFinishReason chat.FinishReason

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, "", err
		}
		if len(response.Choices) == 0 {
			continue
		}
		choice := response.Choices[0]

		if choice.Delta.Content != "" {
			text.WriteString(choice.Delta.Content)
		}

		if len(choice.Delta.ToolCalls) > 0 {
			for _, delta := range choice.Delta.ToolCalls {
				idx, ok := toolIndex[delta.ID]
				if !ok {
					idx = len(toolCalls)
					toolIndex[delta.ID] = idx
					toolCalls = append(toolCalls, tools.ToolCall{
						ID:   delta.ID,
						Type: delta.Type,
					})
				}
				tc := &toolCalls[idx]
				if delta.Type != "" {
					tc.Type = delta.Type
				}
				if delta.Function.Name != "" {
					tc.Function.Name = delta.Function.Name
				}
				if delta.Function.Arguments != "" {
					tc.Function.Arguments += delta.Function.Arguments
				}
			}
			// Short-circuit only when the chunk carried nothing but tool-call
			// deltas. If it also carries a terminal finish reason, fall through
			// so the break below sees the freshly accumulated call. Mirrors
			// the equivalent guard in handleStream (streaming.go).
			if choice.FinishReason == "" {
				continue
			}
		}

		if choice.FinishReason != "" {
			providerFinishReason = choice.FinishReason
		}
		if choice.FinishReason == chat.FinishReasonStop || choice.FinishReason == chat.FinishReasonLength {
			break
		}
	}

	finishReason := providerFinishReason
	if finishReason == "" {
		switch {
		case len(toolCalls) > 0:
			finishReason = chat.FinishReasonToolCalls
		case text.Len() > 0:
			finishReason = chat.FinishReasonStop
		default:
			finishReason = chat.FinishReasonNull
		}
	}
	switch {
	case finishReason == chat.FinishReasonToolCalls && len(toolCalls) == 0:
		finishReason = chat.FinishReasonNull
	case finishReason == chat.FinishReasonStop && len(toolCalls) > 0:
		finishReason = chat.FinishReasonToolCalls
	}

	return text.String(), toolCalls, finishReason, nil
}

// buildSamplingWithToolsContent assembles the assistant response Content
// slice. Any leading text becomes a TextContent block; each tool call
// becomes a ToolUseContent block with the function arguments parsed as a
// JSON object. Malformed arguments fall back to an empty input map so the
// server still sees the call (and can report a tool-side validation error)
// rather than the loop terminating on the client.
func buildSamplingWithToolsContent(text string, toolCalls []tools.ToolCall) []mcp.Content {
	var blocks []mcp.Content
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, &mcp.TextContent{Text: text})
	}
	for _, tc := range toolCalls {
		input := map[string]any{}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		blocks = append(blocks, &mcp.ToolUseContent{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	return blocks
}
