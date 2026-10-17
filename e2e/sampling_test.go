package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/testutil/geminiintegration"
)

// TestExec_Gemini_SamplingWithTools exercises the MCP sampling-with-tools
// round-trip end-to-end. An in-process gomcp.NewServer is mounted on an
// httptest server via StreamableHTTPHandler. It exposes one tool,
// ask_with_calculator, whose handler runs a sampling loop: it sends
// sampling/createMessage with a tools array, gets a tool_use back from the
// host LLM, "executes" the calculator, sends a follow-up sampling request
// carrying the tool_result, and returns the final text.
func TestExec_Gemini_SamplingWithTools(t *testing.T) {
	geminiintegration.Require(t)
	mcpURL := startSamplingToolsServer(t)
	yamlPath := writeSamplingToolsAgent(t, mcpURL)

	out := runCLI(t, "run", "--exec", "--yolo", yamlPath, "--model=google/gemini-2.5-flash", "What is 17 plus 25?")

	require.Contains(t, out, "ask_with_calculator")
	require.Contains(t, out, "42")
}

// startSamplingToolsServer mounts an MCP server on an httptest server and
// returns its URL. The server exposes a single tool whose handler drives a
// sampling-with-tools loop against the connecting client.
func startSamplingToolsServer(t *testing.T) string {
	t.Helper()

	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    "sampling-tools-test",
		Version: "0.0.1",
	}, nil)

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "ask_with_calculator",
		Description: "Answer a math word problem by asking the host LLM for help, with access to a calculator tool the server provides.",
	}, askWithCalculator)

	handler := gomcp.NewStreamableHTTPHandler(
		func(*http.Request) *gomcp.Server { return server },
		nil,
	)
	httpSrv := httptest.NewServer(handler)
	t.Cleanup(httpSrv.Close)

	return httpSrv.URL
}

func writeSamplingToolsAgent(t *testing.T, mcpURL string) string {
	t.Helper()
	yamlPath := filepath.Join(t.TempDir(), "agent.yaml")
	agentYAML := fmt.Appendf(nil, `agents:
  root:
    model: google/gemini-2.5-flash
    description: "Test agent for MCP sampling-with-tools end-to-end verification"
    instruction: |
      You have access to one tool: ask_with_calculator. Whenever the user asks
      a math word problem, call ask_with_calculator with the user's question.
      Then report its answer verbatim to the user.
    toolsets:
      - type: mcp
        allow_private_ips: true
        remote:
          url: %s
          transport_type: streamable
`, mcpURL)
	require.NoError(t, os.WriteFile(yamlPath, agentYAML, 0o644))
	return yamlPath
}

type askInput struct {
	Question string `json:"question" jsonschema:"the natural-language question to answer with help of the calculator"`
}

func askWithCalculator(ctx context.Context, req *gomcp.CallToolRequest, in askInput) (*gomcp.CallToolResult, any, error) {
	calculator := &gomcp.Tool{
		Name:        "calculator",
		Description: "Add two integers. Returns the sum.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"x": map[string]any{"type": "integer"},
				"y": map[string]any{"type": "integer"},
			},
			"required": []string{"x", "y"},
		},
	}

	messages := []*gomcp.SamplingMessageV2{{
		Role:    "user",
		Content: []gomcp.Content{&gomcp.TextContent{Text: in.Question}},
	}}

	for round := 1; round <= 4; round++ {
		res, err := req.Session.CreateMessageWithTools(ctx, &gomcp.CreateMessageWithToolsParams{
			MaxTokens:    1024,
			Messages:     messages,
			Tools:        []*gomcp.Tool{calculator},
			SystemPrompt: "You are a careful assistant. Use the calculator tool whenever you need to add two integers. After you have the sum, answer the user's question in one short sentence.",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("sampling round %d: %w", round, err)
		}

		messages = append(messages, &gomcp.SamplingMessageV2{
			Role:    res.Role,
			Content: res.Content,
		})

		var toolUses []*gomcp.ToolUseContent
		var finalText strings.Builder
		for _, c := range res.Content {
			switch v := c.(type) {
			case *gomcp.ToolUseContent:
				toolUses = append(toolUses, v)
			case *gomcp.TextContent:
				finalText.WriteString(v.Text)
			}
		}

		if len(toolUses) == 0 {
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: finalText.String()}},
			}, nil, nil
		}

		toolResults := make([]gomcp.Content, 0, len(toolUses))
		for _, tu := range toolUses {
			result, err := runCalculator(tu)
			if err != nil {
				toolResults = append(toolResults, &gomcp.ToolResultContent{
					ToolUseID: tu.ID,
					Content:   []gomcp.Content{&gomcp.TextContent{Text: err.Error()}},
					IsError:   true,
				})
				continue
			}
			toolResults = append(toolResults, &gomcp.ToolResultContent{
				ToolUseID: tu.ID,
				Content:   []gomcp.Content{&gomcp.TextContent{Text: result}},
			})
		}

		messages = append(messages, &gomcp.SamplingMessageV2{
			Role:    "user",
			Content: toolResults,
		})
	}

	return nil, nil, errors.New("sampling loop did not terminate within 4 rounds")
}

func runCalculator(tu *gomcp.ToolUseContent) (string, error) {
	if tu.Name != "calculator" {
		return "", fmt.Errorf("unknown tool: %s", tu.Name)
	}
	x, errX := toInt(tu.Input["x"])
	y, errY := toInt(tu.Input["y"])
	if errX != nil || errY != nil {
		raw, _ := json.Marshal(tu.Input)
		return "", fmt.Errorf("calculator expects integer x and y, got %s", raw)
	}
	return strconv.FormatInt(x+y, 10), nil
}

func toInt(v any) (int64, error) {
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case json.Number:
		return n.Int64()
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}
