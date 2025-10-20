package api

import (
	"time"

	"github.com/docker/cagent/pkg/chat"
	v2 "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/session"
)

type Message struct {
	Role    chat.MessageRole `json:"role"`
	Content string           `json:"content"`
}

// Agent represents an agent in the API
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Multi       bool   `json:"multi"`
}

// CreateAgentRequest represents a request to create an agent
type CreateAgentRequest struct {
	Prompt string `json:"prompt"`
}

// CreateAgentResponse represents the response from creating an agent
type CreateAgentResponse struct {
	Path string `json:"path"`
	Out  string `json:"out"`
}

// CreateAgentConfigRequest represents a request to create an agent manually
type CreateAgentConfigRequest struct {
	Filename    string `json:"filename"`
	Model       string `json:"model"`
	Description string `json:"description"`
	Instruction string `json:"instruction"`
}

// CreateAgentConfigResponse represents the response from creating an agent config
type CreateAgentConfigResponse struct {
	Filepath string `json:"filepath"`
}

// EditAgentConfigRequest represents a request to edit an agent config
type EditAgentConfigRequest struct {
	AgentConfig v2.Config `json:"agent_config"`
	Filename    string    `json:"filename"`
}

// EditAgentConfigResponse represents the response from editing an agent config
type EditAgentConfigResponse struct {
	Message string `json:"message"`
	Path    string `json:"path"`
	Config  any    `json:"config"`
}

// ImportAgentRequest represents a request to import an agent
type ImportAgentRequest struct {
	FilePath string `json:"file_path"`
}

// ImportAgentResponse represents the response from importing an agent
type ImportAgentResponse struct {
	OriginalPath string `json:"originalPath"`
	TargetPath   string `json:"targetPath"`
	Description  string `json:"description"`
}

// ExportAgentsResponse represents the response from exporting agents
type ExportAgentsResponse struct {
	ZipPath      string `json:"zipPath"`
	ZipFile      string `json:"zipFile"`
	ZipDirectory string `json:"zipDirectory"`
	AgentsDir    string `json:"agentsDir"`
	CreatedAt    string `json:"createdAt"`
}

// PullAgentRequest represents a request to pull an agent
type PullAgentRequest struct {
	Name string `json:"name"`
}

// PullAgentResponse represents the response from pulling an agent
type PullAgentResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PushAgentRequest represents a request to push an agent
type PushAgentRequest struct {
	Filepath string `json:"filepath"`
	Tag      string `json:"tag"`
}

// PushAgentResponse represents the response from pushing an agent
type PushAgentResponse struct {
	Filepath string `json:"filepath"`
	Tag      string `json:"tag"`
	Digest   string `json:"digest"`
}

// DeleteAgentRequest represents a request to delete an agent
type DeleteAgentRequest struct {
	FilePath string `json:"file_path"`
}

// DeleteAgentResponse represents the response from deleting an agent
type DeleteAgentResponse struct {
	FilePath string `json:"filePath"`
}

// SessionsResponse represents a session in the sessions list
type SessionsResponse struct {
	ID                         string `json:"id"`
	Title                      string `json:"title"`
	CreatedAt                  string `json:"created_at"`
	NumMessages                int    `json:"num_messages"`
	InputTokens                int    `json:"input_tokens"`
	OutputTokens               int    `json:"output_tokens"`
	GetMostRecentAgentFilename string `json:"most_recent_agent_filename"`
	WorkingDir                 string `json:"working_dir,omitempty"`
}

// SessionResponse represents a detailed session
type SessionResponse struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Messages      []session.Message   `json:"messages,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	ToolsApproved bool                `json:"tools_approved"`
	InputTokens   int                 `json:"input_tokens"`
	OutputTokens  int                 `json:"output_tokens"`
	WorkingDir    string              `json:"working_dir,omitempty"`
	Pagination    *PaginationMetadata `json:"pagination,omitempty"`
}

// PaginationMetadata contains pagination information
type PaginationMetadata struct {
	TotalMessages int    `json:"total_messages"`        // Total number of messages in session
	Limit         int    `json:"limit"`                 // Number of messages in this response
	HasMore       bool   `json:"has_more"`              // Whether more messages exist
	NextCursor    string `json:"next_cursor,omitempty"` // Cursor for next page
	PrevCursor    string `json:"prev_cursor,omitempty"` // Cursor for previous page
}

// ResumeSessionRequest represents a request to resume a session
type ResumeSessionRequest struct {
	Confirmation string `json:"confirmation"`
}

// DesktopTokenResponse represents the response from getting a desktop token
type DesktopTokenResponse struct {
	Token string `json:"token"`
}

// ResumeStartOauthRequest represents the user approval to start the OAuth flow
type ResumeStartOauthRequest struct {
	Confirmation bool `json:"confirmation"`
}

// ResumeCodeReceivedOauthRequest represents the response from getting the OAuth URL with code and state
type ResumeCodeReceivedOauthRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// ResumeElicitationRequest represents a request to resume with an elicitation response
type ResumeElicitationRequest struct {
	Action  string         `json:"action"`  // "accept", "decline", or "cancel"
	Content map[string]any `json:"content"` // The submitted form data (only present when action is "accept")
}
