// Package toolserver provides a lightweight HTTP server that exposes agent tools remotely.
// This is useful for running cagent tools inside containers while allowing external
// callers to invoke them.
package toolserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
)

// Server is a lightweight HTTP server that exposes agent tools for remote invocation.
type Server struct {
	agents map[string]*agent.Agent
}

// CallToolRequest is the request body for calling a tool.
type CallToolRequest struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded arguments
}

// CallToolResponse is the response from calling a tool.
type CallToolResponse struct {
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
}

// ErrorResponse represents an error response from the server.
type ErrorResponse struct {
	Error string `json:"error"`
}

// New creates a new tool server from the given agent configuration source.
func New(ctx context.Context, agentSource config.Source, runConfig *config.RuntimeConfig) (*Server, error) {
	// Load the team using teamloader
	team, err := teamloader.Load(ctx, agentSource, runConfig)
	if err != nil {
		return nil, fmt.Errorf("loading agent configuration: %w", err)
	}

	// Build a map of all agents
	agents := make(map[string]*agent.Agent)
	for _, name := range team.AgentNames() {
		a, _ := team.Agent(name)
		agents[name] = a
	}

	return &Server{
		agents: agents,
	}, nil
}

// Serve starts the HTTP server on the given listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /health", s.handleHealth)

	// Call a tool for a specific agent
	mux.HandleFunc("POST /agents/{agent}/tools/{tool}", s.handleCallAgentTool)

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	slog.Info("Tool server listening", "addr", ln.Addr().String())
	return server.Serve(ln)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleCallAgentTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.PathValue("agent")
	toolName := r.PathValue("tool")

	a, ok := s.agents[agentName]
	if !ok {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", agentName))
		return
	}

	var req CallToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	result, found, err := s.callToolOnAgent(ctx, a, toolName, req.Arguments)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("tool execution failed: %v", err))
		return
	}
	if !found {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("tool %q not found on agent %q", toolName, agentName))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(CallToolResponse{
		Output:  result.Output,
		IsError: result.IsError,
	})
}

func (s *Server) callToolOnAgent(ctx context.Context, a *agent.Agent, toolName, arguments string) (*tools.ToolCallResult, bool, error) {
	agentTools, err := a.Tools(ctx)
	if err != nil {
		return nil, false, err
	}

	tool := findTool(agentTools, toolName)
	if tool == nil {
		return nil, false, nil
	}

	if tool.Handler == nil {
		return nil, true, fmt.Errorf("tool %q has no handler", toolName)
	}

	result, err := tool.Handler(ctx, tools.ToolCall{
		ID:   "toolserver-call",
		Type: "function",
		Function: tools.FunctionCall{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		return nil, true, err
	}

	return result, true, nil
}

func findTool(agentTools []tools.Tool, name string) *tools.Tool {
	for i := range agentTools {
		if agentTools[i].Name == name {
			return &agentTools[i]
		}
	}
	return nil
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
