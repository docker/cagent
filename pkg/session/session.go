package session

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
)

// TODO: instead of trimming, we should compact the history when it nears the
// context size of the current LLM
var maxMessages = 100 // Maximum number of messages to keep in context

// Item represents either a message or a sub-session
type Item struct {
	// Message holds a regular conversation message
	Message *Message `json:"message,omitempty"`

	// SubSession holds a complete sub-session from task transfers
	SubSession *Session `json:"sub_session,omitempty"`

	// Summary is a summary of the session up until this point
	Summary string `json:"summary,omitempty"`
}

// IsMessage returns true if this item contains a message
func (si *Item) IsMessage() bool {
	return si.Message != nil
}

// IsSubSession returns true if this item contains a sub-session
func (si *Item) IsSubSession() bool {
	return si.SubSession != nil
}

// Session represents the agent's state including conversation history and variables
type Session struct {
	// ID is the unique identifier for the session
	ID string `json:"id"`

	// Title is the title of the session, set by the runtime
	Title string `json:"title"`

	// Messages holds the conversation history (messages and sub-sessions)
	Messages []Item `json:"messages"`

	// CreatedAt is the time the session was created
	CreatedAt time.Time `json:"created_at"`

	// ToolsApproved is a flag to indicate if the tools have been approved
	ToolsApproved bool `json:"tools_approved"`

	// WorkingDir is the base directory used for filesystem-aware tools
	WorkingDir string `json:"working_dir,omitempty"`

	// SendUserMessage is a flag to indicate if the user message should be sent
	SendUserMessage bool

	// MaxIterations is the maximum number of agentic loop iterations to prevent infinite loops
	// If 0, there is no limit
	MaxIterations int `json:"max_iterations"`

	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// Message is a message from an agent
type Message struct {
	AgentFilename string       `json:"agentFilename"`
	AgentName     string       `json:"agentName"` // TODO: rename to agent_name
	Message       chat.Message `json:"message"`
	// Implicit is an optional field to indicate if the message shouldn't be shown to the user. It's needed for special  situations
	// like when an agent transfers a task to another agent - new session is created with a default user message, but this shouldn't be shown to the user.
	// Such messages should be marked as true
	Implicit bool `json:"implicit,omitempty"`
}

func ImplicitUserMessage(agentFilename, content string) *Message {
	return &Message{
		AgentFilename: agentFilename,
		AgentName:     "",
		Message: chat.Message{
			Role:      chat.MessageRoleUser,
			Content:   content,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
		Implicit: true,
	}
}

func UserMessage(agentFilename, content string) *Message {
	return &Message{
		AgentFilename: agentFilename,
		AgentName:     "",
		Message: chat.Message{
			Role:      chat.MessageRoleUser,
			Content:   content,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	}
}

func NewAgentMessage(a *agent.Agent, message *chat.Message) *Message {
	return &Message{
		AgentFilename: "",
		AgentName:     a.Name(),
		Message:       *message,
	}
}

func SystemMessage(content string) *Message {
	return &Message{
		AgentFilename: "",
		AgentName:     "",
		Message: chat.Message{
			Role:      chat.MessageRoleSystem,
			Content:   content,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	}
}

// Helper functions for creating SessionItems

// NewMessageItem creates a SessionItem containing a message
func NewMessageItem(msg *Message) Item {
	return Item{Message: msg}
}

// NewSubSessionItem creates a SessionItem containing a sub-session
func NewSubSessionItem(subSession *Session) Item {
	return Item{SubSession: subSession}
}

// Session helper methods

// AddMessage adds a message to the session
func (s *Session) AddMessage(msg *Message) {
	s.Messages = append(s.Messages, NewMessageItem(msg))
}

// AddSubSession adds a sub-session to the session
func (s *Session) AddSubSession(subSession *Session) {
	s.Messages = append(s.Messages, NewSubSessionItem(subSession))
}

// AllowedDirectories returns the directories that should be considered safe for tools
func (s *Session) AllowedDirectories() []string {
	if s.WorkingDir == "" {
		return nil
	}
	return []string{s.WorkingDir}
}

// GetAllMessages extracts all messages from the session, including from sub-sessions
func (s *Session) GetAllMessages() []Message {
	var messages []Message
	for _, item := range s.Messages {
		if item.IsMessage() && item.Message.Message.Role != chat.MessageRoleSystem {
			messages = append(messages, *item.Message)
		} else if item.IsSubSession() {
			// Recursively get messages from sub-sessions
			subMessages := item.SubSession.GetAllMessages()
			messages = append(messages, subMessages...)
		}
	}
	return messages
}

func (s *Session) GetLastAssistantMessageContent() string {
	messages := s.GetAllMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Message.Role == chat.MessageRoleAssistant {
			return strings.TrimSpace(messages[i].Message.Content)
		}
	}
	return ""
}

type Opt func(s *Session)

func WithUserMessage(agentFilename, content string) Opt {
	return func(s *Session) {
		s.AddMessage(UserMessage(agentFilename, content))
	}
}

func WithImplicitUserMessage(agentFilename, content string) Opt {
	return func(s *Session) {
		s.AddMessage(ImplicitUserMessage(agentFilename, content))
	}
}

func WithSystemMessage(content string) Opt {
	return func(s *Session) {
		s.AddMessage(SystemMessage(content))
	}
}

func WithMaxIterations(maxIterations int) Opt {
	return func(s *Session) {
		s.MaxIterations = maxIterations
	}
}

func WithWorkingDir(workingDir string) Opt {
	return func(s *Session) {
		s.WorkingDir = workingDir
	}
}

// New creates a new agent session
func New(opts ...Opt) *Session {
	sessionID := uuid.New().String()
	slog.Debug("Creating new session", "session_id", sessionID)

	s := &Session{
		ID:              sessionID,
		CreatedAt:       time.Now(),
		SendUserMessage: true,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *Session) GetMessages(a *agent.Agent) []chat.Message {
	slog.Debug("Getting messages for agent", "agent", a.Name(), "session_id", s.ID)

	var messages []chat.Message

	if a.HasSubAgents() {
		subAgents := append(a.SubAgents(), a.Parents()...)

		subAgentsStr := ""
		var validAgentIDs []string
		for _, subAgent := range subAgents {
			subAgentsStr += "ID: " + subAgent.Name() + " | Name: " + subAgent.Name() + " | Description: " + subAgent.Description() + "\n"
			validAgentIDs = append(validAgentIDs, subAgent.Name())
		}

		messages = append(messages, chat.Message{
			Role:    chat.MessageRoleSystem,
			Content: "You are a multi-agent system, make sure to answer the user query in the most helpful way possible. You have access to these sub-agents:\n" + subAgentsStr + "\nIMPORTANT: You can ONLY transfer tasks to the agents listed above using their ID. The valid agent IDs are: " + strings.Join(validAgentIDs, ", ") + ". You MUST NOT attempt to transfer to any other agent IDs - doing so will cause system errors.\n\nIf you are the best to answer the question according to your description, you can answer it.\n\nIf another agent is better for answering the question according to its description, call `transfer_task` function to transfer the question to that agent using the agent's ID. When transferring, do not generate any text other than the function call.\n\n",
		})
	}

	content := a.Instruction()

	if a.AddDate() {
		content += "\n\n" + "Today's date: " + time.Now().Format("2006-01-02")
	}

	wd := s.WorkingDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			slog.Error("getting current working directory for environment info", "error", err)
		}
	}
	if wd != "" {
		if a.AddEnvironmentInfo() {
			content += "\n\n" + getEnvironmentInfo(wd)
		}

		for _, prompt := range a.AddPromptFiles() {
			additionalPrompt, err := readPromptFile(wd, prompt)
			if err != nil {
				slog.Error("reading prompt file", "file", prompt, "error", err)
				continue
			}

			if additionalPrompt == "" {
				content += "\n\n" + additionalPrompt
			}
		}
	}

	messages = append(messages, chat.Message{
		Role:    chat.MessageRoleSystem,
		Content: content,
	})

	for _, tool := range a.ToolSets() {
		if tool.Instructions() != "" {
			messages = append(messages, chat.Message{
				Role:    chat.MessageRoleSystem,
				Content: tool.Instructions(),
			})
		}
	}

	lastSummaryIndex := -1
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Summary != "" {
			lastSummaryIndex = i
			break
		}
	}

	if lastSummaryIndex != -1 {
		messages = append(messages, chat.Message{
			Role:      chat.MessageRoleSystem,
			Content:   "Session Summary: " + s.Messages[lastSummaryIndex].Summary,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
	}

	startIndex := lastSummaryIndex + 1
	if lastSummaryIndex == -1 {
		startIndex = 0
	}

	for i := startIndex; i < len(s.Messages); i++ {
		item := s.Messages[i]
		if item.IsMessage() {
			messages = append(messages, item.Message.Message)
		}
	}

	maxItems := a.NumHistoryItems()
	if maxItems <= 0 {
		maxItems = maxMessages
	}

	trimmed := trimMessages(messages, maxItems)

	systemCount := 0
	conversationCount := 0
	for i := range trimmed {
		if trimmed[i].Role == chat.MessageRoleSystem {
			systemCount++
		} else {
			conversationCount++
		}
	}

	slog.Debug("Retrieved messages for agent",
		"agent", a.Name(),
		"session_id", s.ID,
		"total_messages", len(messages),
		"trimmed_total", len(trimmed),
		"system_messages", systemCount,
		"conversation_messages", conversationCount,
		"max_history_items", maxItems)

	return trimmed
}

func (s *Session) GetMostRecentAgentFilename() string {
	// Check items in reverse order
	for i := len(s.Messages) - 1; i >= 0; i-- {
		item := s.Messages[i]
		if item.IsMessage() {
			if agentFilename := item.Message.AgentFilename; agentFilename != "" {
				return agentFilename
			}
		} else if item.IsSubSession() {
			if filename := item.SubSession.GetMostRecentAgentFilename(); filename != "" {
				return filename
			}
		}
	}
	return ""
}

// trimMessages ensures we don't exceed the maximum number of messages while maintaining
// consistency between assistant messages and their tool call results.
// System messages are always preserved and not counted against the limit.
func trimMessages(messages []chat.Message, maxItems int) []chat.Message {
	// Separate system messages from conversation messages
	var systemMessages []chat.Message
	var conversationMessages []chat.Message

	for i := range messages {
		if messages[i].Role == chat.MessageRoleSystem {
			systemMessages = append(systemMessages, messages[i])
		} else {
			conversationMessages = append(conversationMessages, messages[i])
		}
	}

	// If conversation messages fit within limit, return all messages
	if len(conversationMessages) <= maxItems {
		return messages
	}

	// Keep track of tool call IDs that need to be removed
	toolCallsToRemove := make(map[string]bool)

	// Calculate how many conversation messages we need to remove
	toRemove := len(conversationMessages) - maxItems

	// Start from the beginning (oldest messages)
	for i := range toRemove {
		// If this is an assistant message with tool calls, mark them for removal
		if conversationMessages[i].Role == chat.MessageRoleAssistant {
			for _, toolCall := range conversationMessages[i].ToolCalls {
				toolCallsToRemove[toolCall.ID] = true
			}
		}
	}

	// Combine system messages with trimmed conversation messages
	result := make([]chat.Message, 0, len(systemMessages)+maxItems)

	// Add all system messages first
	result = append(result, systemMessages...)

	// Add the most recent conversation messages
	for i := toRemove; i < len(conversationMessages); i++ {
		msg := conversationMessages[i]

		// Skip tool messages that correspond to removed assistant messages
		if msg.Role == chat.MessageRoleTool && toolCallsToRemove[msg.ToolCallID] {
			continue
		}

		result = append(result, msg)
	}

	return result
}
