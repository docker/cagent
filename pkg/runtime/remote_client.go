package runtime

import (
	"context"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// RemoteClient is the interface that both HTTP and Connect-RPC clients implement
// for communicating with a remote cagent server.
type RemoteClient interface {
	// GetAgent retrieves an agent configuration by ID
	GetAgent(ctx context.Context, id string) (*latest.Config, error)

	// CreateSession creates a new session
	CreateSession(ctx context.Context, sessTemplate *session.Session) (*session.Session, error)

	// ResumeSession resumes a paused session with an optional rejection reason
	ResumeSession(ctx context.Context, id, confirmation, reason string) error

	// ResumeElicitation sends an elicitation response
	ResumeElicitation(ctx context.Context, sessionID string, action tools.ElicitationAction, content map[string]any) error

	// RunAgent executes an agent and returns a channel of streaming events
	RunAgent(ctx context.Context, sessionID, agent string, messages []api.Message) (<-chan Event, error)

	// RunAgentWithAgentName executes an agent with a specific agent name
	RunAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message) (<-chan Event, error)

	// UpdateSessionTitle updates the title of a session
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
}

// Verify that both clients implement RemoteClient
var (
	_ RemoteClient = (*Client)(nil)
	_ RemoteClient = (*ConnectRPCClient)(nil)
)
