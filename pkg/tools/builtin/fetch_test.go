package builtin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestFetchToolWithOptions(t *testing.T) {
	customTimeout := 60 * time.Second

	tool := NewFetchTool(WithTimeout(customTimeout))

	assert.Equal(t, customTimeout, tool.handler.timeout)
}

func TestFetchTool_Tools(t *testing.T) {
	tool := NewFetchTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 1)
	for _, tool := range allTools {
		assert.NotNil(t, tool.Handler)
		assert.Equal(t, "fetch", tool.Category)
	}

	fetchTool := allTools[0]
	assert.Equal(t, "fetch", fetchTool.Name)

	schema, err := json.Marshal(fetchTool.Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"format": {
			"description": "The format to return the content in (text, markdown, or html)",
			"enum": [
				"text",
				"markdown",
				"html"
			],
			"type": "string"
		},
		"timeout": {
			"description": "Request timeout in seconds (default: 30)",
			"maximum": 300,
			"minimum": 1,
			"type": "integer"
		},
		"urls": {
			"description": "Array of URLs to fetch",
			"items": {
				"type": "string"
			},
			"minItems": 1,
			"type": "array"
		}
	},
	"required": [
		"urls",
		"format"
	]
}`, string(schema))
}

func TestFetchTool_Instructions(t *testing.T) {
	tool := NewFetchTool()

	instructions := tools.GetInstructions(tool)

	assert.Contains(t, instructions, `"fetch" tool instructions`)
}

func TestFetchTool_StartStop(t *testing.T) {
	// FetchTool doesn't need to implement Startable -
	// it has no initialization or cleanup requirements
	tool := NewFetchTool()

	// Verify it implements ToolSet but not necessarily Startable
	_, ok := any(tool).(tools.Startable)
	assert.False(t, ok, "FetchTool should not implement Startable")
}

func TestFetch_Call_Success(t *testing.T) {
	url := runHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Hello, World!")
	})

	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{URLs: []string{url}})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "Successfully fetched")
	assert.Contains(t, result.Output, "Status: 200")
	assert.Contains(t, result.Output, "Length: 13 bytes")
	assert.Contains(t, result.Output, "Hello, World!")
}

func TestFetch_Call_MultipleURLs(t *testing.T) {
	url1 := runHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "Server 1")
	})
	url2 := runHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "Server 2")
	})

	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{URLs: []string{url1, url2}})
	require.NoError(t, err)

	var results []FetchResult
	err = json.Unmarshal([]byte(result.Output), &results)
	require.NoError(t, err)

	require.Len(t, results, 2)
	assert.Equal(t, "Server 1", results[0].Body)
	assert.Equal(t, "Server 2", results[1].Body)
}

func TestFetch_Call_InvalidURL(t *testing.T) {
	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs: []string{
			"invalid-url",
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Error fetching")
}

func TestFetch_Call_UnsupportedProtocol(t *testing.T) {
	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs: []string{
			"ftp://example.com",
		},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Error fetching")
	assert.Contains(t, result.Output, "only HTTP and HTTPS URLs are supported")
}

func TestFetch_Call_NoURLs(t *testing.T) {
	tool := NewFetchTool()

	_, err := tool.handler.CallTool(t.Context(), FetchToolArgs{})
	require.ErrorContains(t, err, "at least one URL is required")
}

func TestFetch_Markdown(t *testing.T) {
	url := runHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<h1>Hello cagent</h1>")
	})

	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs:   []string{url},
		Format: "markdown",
	})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "Successfully fetched")
	assert.Contains(t, result.Output, "Status: 200")
	assert.Contains(t, result.Output, "Length: 14 bytes")
	assert.Contains(t, result.Output, "# Hello cagent")
}

func TestFetch_Text(t *testing.T) {
	url := runHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<h1>Hello cagent</h1>")
	})

	tool := NewFetchTool()

	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs:   []string{url},
		Format: "text",
	})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "Successfully fetched")
	assert.Contains(t, result.Output, "Status: 200")
	assert.Contains(t, result.Output, "Length: 12 bytes")
	assert.Contains(t, result.Output, "Hello cagent")
}

func runHTTPServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server.URL
}

func TestFetch_RobotsAllowed(t *testing.T) {
	robotsContent := "User-agent: *\nAllow: /"

	url := runHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, robotsContent)
			return
		}
		if r.URL.Path == "/allowed" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Content allowed by robots")
			return
		}
		http.NotFound(w, r)
	})

	tool := NewFetchTool()
	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs:   []string{url + "/allowed"},
		Format: "text",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Successfully fetched")
	assert.Contains(t, result.Output, "Content allowed by robots")
}

func TestFetch_RobotsBlocked(t *testing.T) {
	robotsContent := "User-agent: *\nDisallow: /blocked"

	url := runHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, robotsContent)
			return
		}
		if r.URL.Path == "/blocked" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "This should not be fetched")
			return
		}
		http.NotFound(w, r)
	})

	tool := NewFetchTool()
	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs:   []string{url + "/blocked"},
		Format: "text",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Error fetching")
	assert.Contains(t, result.Output, "URL blocked by robots.txt")
}

func TestFetch_RobotsMissing(t *testing.T) {
	url := runHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/content" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Content without robots.txt")
			return
		}
		http.NotFound(w, r)
	})

	tool := NewFetchTool()
	result, err := tool.handler.CallTool(t.Context(), FetchToolArgs{
		URLs:   []string{url + "/content"},
		Format: "text",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Successfully fetched")
	assert.Contains(t, result.Output, "Content without robots.txt")
}

func TestFetchTool_OutputSchema(t *testing.T) {
	tool := NewFetchTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestFetchTool_ParametersAreObjects(t *testing.T) {
	tool := NewFetchTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
