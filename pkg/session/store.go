package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/sqliteutil"
)

var (
	ErrEmptyID  = errors.New("session ID cannot be empty")
	ErrNotFound = errors.New("session not found")
)

// Summary contains lightweight session metadata for listing purposes.
// This is used instead of loading full Session objects with all messages.
type Summary struct {
	ID        string
	Title     string
	CreatedAt time.Time
	Starred   bool
}

// convertMessagesToItems converts a slice of Messages to SessionItems for backward compatibility
func convertMessagesToItems(messages []Message) []Item {
	items := make([]Item, len(messages))
	for i := range messages {
		items[i] = NewMessageItem(&messages[i])
	}
	return items
}

// Store defines the interface for session storage
type Store interface {
	AddSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessions(ctx context.Context) ([]*Session, error)
	GetSessionSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error
	SetSessionStarred(ctx context.Context, id string, starred bool) error
}

type InMemorySessionStore struct {
	sessions *concurrent.Map[string, *Session]
}

func NewInMemorySessionStore() Store {
	return &InMemorySessionStore{
		sessions: concurrent.NewMap[string, *Session](),
	}
}

func (s *InMemorySessionStore) AddSession(_ context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}
	s.sessions.Store(session.ID, session)
	return nil
}

func (s *InMemorySessionStore) GetSession(_ context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}
	session, exists := s.sessions.Load(id)
	if !exists {
		return nil, ErrNotFound
	}
	return session, nil
}

func (s *InMemorySessionStore) GetSessions(_ context.Context) ([]*Session, error) {
	sessions := make([]*Session, 0, s.sessions.Length())
	s.sessions.Range(func(key string, value *Session) bool {
		sessions = append(sessions, value)
		return true
	})
	return sessions, nil
}

func (s *InMemorySessionStore) GetSessionSummaries(_ context.Context) ([]Summary, error) {
	summaries := make([]Summary, 0, s.sessions.Length())
	s.sessions.Range(func(_ string, value *Session) bool {
		summaries = append(summaries, Summary{
			ID:        value.ID,
			Title:     value.Title,
			CreatedAt: value.CreatedAt,
			Starred:   value.Starred,
		})
		return true
	})
	return summaries, nil
}

func (s *InMemorySessionStore) DeleteSession(_ context.Context, id string) error {
	if id == "" {
		return ErrEmptyID
	}
	_, exists := s.sessions.Load(id)
	if !exists {
		return ErrNotFound
	}
	s.sessions.Delete(id)
	return nil
}

// UpdateSession updates an existing session, or creates it if it doesn't exist (upsert).
// This enables lazy session persistence - sessions are only stored when they have content.
func (s *InMemorySessionStore) UpdateSession(_ context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}
	s.sessions.Store(session.ID, session)
	return nil
}

// SetSessionStarred sets the starred status of a session.
func (s *InMemorySessionStore) SetSessionStarred(_ context.Context, id string, starred bool) error {
	if id == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(id)
	if !exists {
		return ErrNotFound
	}
	session.Starred = starred
	s.sessions.Store(id, session)
	return nil
}

// SQLiteSessionStore implements Store using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(path string) (Store, error) {
	db, err := sqliteutil.OpenDB(path)
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			messages TEXT,
			created_at TEXT
		)
	`)
	if err != nil {
		db.Close()
		if sqliteutil.IsCantOpenError(err) {
			return nil, sqliteutil.DiagnoseDBOpenError(path, err)
		}
		return nil, err
	}

	// Initialize and run migrations
	migrationManager := NewMigrationManager(db)
	err = migrationManager.InitializeMigrations(context.Background())
	if err != nil {
		return nil, err
	}

	return &SQLiteSessionStore{db: db}, nil
}

// AddSession adds a new session to the store
func (s *SQLiteSessionStore) AddSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	itemsJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	permissionsJSON := ""
	if session.Permissions != nil {
		permBytes, err := json.Marshal(session.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	// Marshal agent model overrides (default to empty object if nil)
	agentModelOverridesJSON := "{}"
	if len(session.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(session.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	// Marshal custom models used (default to empty array if nil)
	customModelsUsedJSON := "[]"
	if len(session.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(session.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, string(itemsJSON), session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking)
	return err
}

// scanSession scans a single row into a Session struct
func scanSession(scanner interface {
	Scan(dest ...any) error
},
) (*Session, error) {
	var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, starredStr, agentModelOverridesJSON, customModelsUsedJSON, thinkingStr string
	var sessionID string
	var workingDir sql.NullString
	var permissionsJSON sql.NullString

	err := scanner.Scan(&sessionID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr, &starredStr, &permissionsJSON, &agentModelOverridesJSON, &customModelsUsedJSON, &thinkingStr)
	if err != nil {
		return nil, err
	}

	// Ok listen up, we used to only store messages in the database, but now we
	// store messages and sub-sessions. So we need to handle both cases.
	// Legacy format has Message structs directly, new format has Item wrappers.
	// When unmarshaling new format into []Message, we get empty structs.
	// We detect legacy format by checking if the first message has actual content.
	var items []Item
	var messages []Message
	if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
		return nil, err
	}
	// Check if this is legacy format by seeing if we got actual message content
	isLegacyFormat := len(messages) > 0 && (messages[0].AgentName != "" || messages[0].Message.Content != "" || messages[0].Message.Role != "")
	if isLegacyFormat {
		// Legacy format: messages were successfully parsed, convert them to items
		items = convertMessagesToItems(messages)
	} else {
		// New format: unmarshal directly as items
		if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
			return nil, err
		}
	}

	toolsApproved, err := strconv.ParseBool(toolsApprovedStr)
	if err != nil {
		return nil, err
	}

	inputTokens, err := strconv.ParseInt(inputTokensStr, 10, 64)
	if err != nil {
		return nil, err
	}

	outputTokens, err := strconv.ParseInt(outputTokensStr, 10, 64)
	if err != nil {
		return nil, err
	}

	cost, err := strconv.ParseFloat(costStr, 64)
	if err != nil {
		return nil, err
	}

	sendUserMessage, err := strconv.ParseBool(sendUserMessageStr)
	if err != nil {
		return nil, err
	}

	maxIterations, err := strconv.Atoi(maxIterationsStr)
	if err != nil {
		return nil, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, err
	}

	starred, err := strconv.ParseBool(starredStr)
	if err != nil {
		return nil, err
	}

	thinking, err := strconv.ParseBool(thinkingStr)
	if err != nil {
		return nil, err
	}

	// Parse permissions if present
	var permissions *PermissionsConfig
	if permissionsJSON.Valid && permissionsJSON.String != "" {
		permissions = &PermissionsConfig{}
		if err := json.Unmarshal([]byte(permissionsJSON.String), permissions); err != nil {
			return nil, err
		}
	}

	// Parse agent model overrides (may be empty or "{}")
	var agentModelOverrides map[string]string
	if agentModelOverridesJSON != "" && agentModelOverridesJSON != "{}" {
		if err := json.Unmarshal([]byte(agentModelOverridesJSON), &agentModelOverrides); err != nil {
			return nil, err
		}
	}

	// Parse custom models used (may be empty or "[]")
	var customModelsUsed []string
	if customModelsUsedJSON != "" && customModelsUsedJSON != "[]" {
		if err := json.Unmarshal([]byte(customModelsUsedJSON), &customModelsUsed); err != nil {
			return nil, err
		}
	}

	return &Session{
		ID:                  sessionID,
		Title:               titleStr,
		Messages:            items,
		ToolsApproved:       toolsApproved,
		Thinking:            thinking,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		Cost:                cost,
		SendUserMessage:     sendUserMessage,
		MaxIterations:       maxIterations,
		CreatedAt:           createdAt,
		WorkingDir:          workingDir.String,
		Starred:             starred,
		Permissions:         permissions,
		AgentModelOverrides: agentModelOverrides,
		CustomModelsUsed:    customModelsUsed,
	}, nil
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	row := s.db.QueryRowContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking FROM sessions WHERE id = ?", id)

	session, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return session, nil
}

// GetSessions retrieves all sessions
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking FROM sessions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing.
// This is much faster than GetSessions as it doesn't load message content.
func (s *SQLiteSessionStore) GetSessionSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, created_at, starred FROM sessions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []Summary
	for rows.Next() {
		var id, title, createdAtStr, starredStr string
		if err := rows.Scan(&id, &title, &createdAtStr, &starredStr); err != nil {
			return nil, err
		}
		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, err
		}
		starred, err := strconv.ParseBool(starredStr)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, Summary{
			ID:        id,
			Title:     title,
			CreatedAt: createdAt,
			Starred:   starred,
		})
	}

	return summaries, nil
}

// DeleteSession deletes a session by ID
func (s *SQLiteSessionStore) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateSession updates an existing session, or creates it if it doesn't exist (upsert).
// This enables lazy session persistence - sessions are only stored when they have content.
func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	itemsJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	permissionsJSON := ""
	if session.Permissions != nil {
		permBytes, err := json.Marshal(session.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	// Marshal agent model overrides (default to empty object if nil)
	agentModelOverridesJSON := "{}"
	if len(session.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(session.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	// Marshal custom models used (default to empty array if nil)
	customModelsUsedJSON := "[]"
	if len(session.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(session.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	// Use INSERT OR REPLACE for upsert behavior - creates if not exists, updates if exists
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   messages = excluded.messages,
		   title = excluded.title,
		   tools_approved = excluded.tools_approved,
		   input_tokens = excluded.input_tokens,
		   output_tokens = excluded.output_tokens,
		   cost = excluded.cost,
		   send_user_message = excluded.send_user_message,
		   max_iterations = excluded.max_iterations,
		   working_dir = excluded.working_dir,
		   starred = excluded.starred,
		   permissions = excluded.permissions,
		   agent_model_overrides = excluded.agent_model_overrides,
		   custom_models_used = excluded.custom_models_used,
		   thinking = excluded.thinking`,
		session.ID, string(itemsJSON), session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir,
		session.CreatedAt.Format(time.RFC3339), session.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking)
	return err
}

// SetSessionStarred sets the starred status of a session.
func (s *SQLiteSessionStore) SetSessionStarred(ctx context.Context, id string, starred bool) error {
	if id == "" {
		return ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, "UPDATE sessions SET starred = ? WHERE id = ?", starred, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteSessionStore) Close() error {
	return s.db.Close()
}
