package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// Client is an HTTP client for the cagent server API
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	registry   map[string]func() Event
}

// ClientOption is a function for configuring the Client
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithTimeout sets the HTTP client timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
		}
		c.httpClient.Timeout = timeout
	}
}

// NewClient creates a new HTTP client for the cagent server
func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	client := &Client{
		baseURL: parsedURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		registry: map[string]func() Event{
			"user_message":           func() Event { return &UserMessageEvent{} },
			"tool_call":              func() Event { return &ToolCallEvent{} },
			"tool_call_response":     func() Event { return &ToolCallResponseEvent{} },
			"tool_call_confirmation": func() Event { return &ToolCallConfirmationEvent{} },
			"token_usage":            func() Event { return &TokenUsageEvent{} },
			"stream_stopped":         func() Event { return &StreamStoppedEvent{} },
			"stream_started":         func() Event { return &StreamStartedEvent{} },
			"shell":                  func() Event { return &ShellOutputEvent{} },
			"session_title":          func() Event { return &SessionTitleEvent{} },
			"session_summary":        func() Event { return &SessionSummaryEvent{} },
			"session_compaction":     func() Event { return &SessionCompactionEvent{} },
			"partial_tool_call":      func() Event { return &PartialToolCallEvent{} },
			"max_iterations_reached": func() Event { return &MaxIterationsReachedEvent{} },
			"error":                  func() Event { return &ErrorEvent{} },
			"elicitation_request":    func() Event { return &ElicitationRequestEvent{} },
			"authorization_event":    func() Event { return &AuthorizationEvent{} },
			"agent_choice":           func() Event { return &AgentChoiceEvent{} },
			"agent_choice_reasoning": func() Event { return &AgentChoiceReasoningEvent{} },
			"mcp_init_started":       func() Event { return &MCPInitStartedEvent{} },
			"mcp_init_finished":      func() Event { return &MCPInitFinishedEvent{} },
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error string `json:"error"`
}

// doRequest performs an HTTP request and handles common response patterns
func (c *Client) doRequest(ctx context.Context, method, endpoint string, body, result any) error {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	u := *c.baseURL
	u.Path = path.Join(u.Path, endpoint)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
	}

	return nil
}

// GetAgents retrieves all available agents
func (c *Client) GetAgents(ctx context.Context) ([]api.Agent, error) {
	var agents []api.Agent
	err := c.doRequest(ctx, http.MethodGet, "/api/agents", nil, &agents)
	return agents, err
}

// GetAgent retrieves an agent by ID
func (c *Client) GetAgent(ctx context.Context, id string) (*latest.Config, error) {
	var config latest.Config
	err := c.doRequest(ctx, http.MethodGet, "/api/agents/"+id, nil, &config)
	return &config, err
}

// CreateAgent creates a new agent using a prompt
func (c *Client) CreateAgent(ctx context.Context, prompt string) (*api.CreateAgentResponse, error) {
	req := api.CreateAgentRequest{Prompt: prompt}
	var resp api.CreateAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents", req, &resp)
	return &resp, err
}

// CreateAgentConfig creates a new agent manually with YAML configuration
func (c *Client) CreateAgentConfig(ctx context.Context, filename, model, description, instruction string) (*api.CreateAgentConfigResponse, error) {
	req := api.CreateAgentConfigRequest{
		Filename:    filename,
		Model:       model,
		Description: description,
		Instruction: instruction,
	}
	var resp api.CreateAgentConfigResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/config", req, &resp)
	return &resp, err
}

// EditAgentConfig edits an agent configuration
func (c *Client) EditAgentConfig(ctx context.Context, filename string, config latest.Config) (*api.EditAgentConfigResponse, error) {
	req := api.EditAgentConfigRequest{
		AgentConfig: config,
		Filename:    filename,
	}
	var resp api.EditAgentConfigResponse
	err := c.doRequest(ctx, "PUT", "/api/agents/config", req, &resp)
	return &resp, err
}

// ImportAgent imports an agent from a file path
func (c *Client) ImportAgent(ctx context.Context, filePath string) (*api.ImportAgentResponse, error) {
	req := api.ImportAgentRequest{FilePath: filePath}
	var resp api.ImportAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/import", req, &resp)
	return &resp, err
}

// ExportAgents exports multiple agents as a zip file
func (c *Client) ExportAgents(ctx context.Context) (*api.ExportAgentsResponse, error) {
	var resp api.ExportAgentsResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/export", nil, &resp)
	return &resp, err
}

// PullAgent pulls an agent from a remote registry
func (c *Client) PullAgent(ctx context.Context, name string) (*api.PullAgentResponse, error) {
	req := api.PullAgentRequest{Name: name}
	var resp api.PullAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/pull", req, &resp)
	return &resp, err
}

// PushAgent pushes an agent to a remote registry
func (c *Client) PushAgent(ctx context.Context, filepath, tag string) (*api.PushAgentResponse, error) {
	req := api.PushAgentRequest{Filepath: filepath, Tag: tag}
	var resp api.PushAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/push", req, &resp)
	return &resp, err
}

// DeleteAgent deletes an agent by file path
func (c *Client) DeleteAgent(ctx context.Context, filePath string) (*api.DeleteAgentResponse, error) {
	req := api.DeleteAgentRequest{FilePath: filePath}
	var resp api.DeleteAgentResponse
	err := c.doRequest(ctx, "DELETE", "/api/agents", req, &resp)
	return &resp, err
}

// GetSessions retrieves all sessions
func (c *Client) GetSessions(ctx context.Context) ([]api.SessionsResponse, error) {
	var sessions []api.SessionsResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/sessions", nil, &sessions)
	return sessions, err
}

// GetSession retrieves a session by ID
func (c *Client) GetSession(ctx context.Context, id string) (*api.SessionResponse, error) {
	var sess api.SessionResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/sessions/"+id, nil, &sess)
	return &sess, err
}

// CreateSession creates a new session
func (c *Client) CreateSession(ctx context.Context, sessTemplate *session.Session) (*session.Session, error) {
	var sess session.Session
	err := c.doRequest(ctx, http.MethodPost, "/api/sessions", sessTemplate, &sess)
	return &sess, err
}

// ResumeSession resumes a session by ID with an optional rejection reason
func (c *Client) ResumeSession(ctx context.Context, id, confirmation, reason string) error {
	req := api.ResumeSessionRequest{Confirmation: confirmation, Reason: reason}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+id+"/resume", req, nil)
}

// DeleteSession deletes a session by ID
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, "DELETE", "/api/sessions/"+id, nil, nil)
}

// GetDesktopToken retrieves a desktop authentication token
func (c *Client) GetDesktopToken(ctx context.Context) (*api.DesktopTokenResponse, error) {
	var resp api.DesktopTokenResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/desktop/token", nil, &resp)
	return &resp, err
}

// RunAgent executes an agent and returns a channel of streaming events
func (c *Client) RunAgent(ctx context.Context, sessionID, agent string, messages []api.Message) (<-chan Event, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, "", messages)
}

// RunAgentWithAgentName executes an agent with a specific agent name and returns a channel of streaming events
func (c *Client) RunAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message) (<-chan Event, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, agentName, messages)
}

func (c *Client) runAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message) (<-chan Event, error) {
	endpoint := "/api/sessions/" + sessionID + "/agent/" + agent
	if agentName != "" {
		endpoint += "/" + agentName
	}

	jsonBody, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("marshaling messages: %w", err)
	}

	u := *c.baseURL
	u.Path = path.Join(u.Path, endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading error response body: %w", err)
		}

		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	eventChan := make(chan Event, 128)

	go func() {
		defer close(eventChan)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || line[0] == ':' {
				continue
			}

			after, ok := bytes.CutPrefix(line, []byte("data: "))
			if !ok {
				continue
			}

			slog.Debug("event", "event", string(after))

			// First unmarshal to get the type
			var baseEvent struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(after, &baseEvent); err != nil {
				slog.Debug("event", "error", err)
				continue
			}

			// Then unmarshal the full event
			createEvent, found := c.registry[baseEvent.Type]
			if !found {
				slog.Debug("event", "invalid_type", baseEvent.Type)
				continue
			}

			e := createEvent()
			if err := json.Unmarshal(after, &e); err != nil {
				slog.Debug("event", "error", err)
				continue
			}

			eventChan <- e
		}

		if err := scanner.Err(); err != nil {
			return
		}
	}()

	return eventChan, nil
}

func (c *Client) ResumeElicitation(ctx context.Context, sessionID string, action tools.ElicitationAction, content map[string]any) error {
	req := api.ResumeElicitationRequest{Action: string(action), Content: content}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+sessionID+"/elicitation", req, nil)
}

// UpdateSessionTitle updates the title of a session
func (c *Client) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	req := api.UpdateSessionTitleRequest{Title: title}
	return c.doRequest(ctx, http.MethodPatch, "/api/sessions/"+sessionID+"/title", req, nil)
}
