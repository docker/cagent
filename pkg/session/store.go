package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrEmptyID  = errors.New("session ID cannot be empty")
	ErrNotFound = errors.New("session not found")
)

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
	GetSessionsByUser(ctx context.Context, userID string) ([]*Session, error)
	GetSessionsByAgent(ctx context.Context, agentFilename string) ([]*Session, error)
	GetSessionsByUserAndAgent(ctx context.Context, userID, agentFilename string) ([]*Session, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error
	// New method to check if a user owns a session
	IsSessionOwnedByUser(ctx context.Context, sessionID, userID string) (bool, error)
}

// SQLiteSessionStore implements Store using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(path string) (Store, error) {
	// Add query parameters for better concurrency handling
	// _busy_timeout: Wait up to 5 seconds if database is locked
	// _journal_mode=WAL: Enable Write-Ahead Logging for better concurrent access
	db, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	// Configure connection pool to serialize writes (SQLite limitation)
	// This prevents "database is locked" errors from concurrent writes
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			messages TEXT,
			created_at TEXT
		)
	`)
	if err != nil {
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

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, messages, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, session.UserID, string(itemsJSON), session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339))
	return err
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	row := s.db.QueryRowContext(ctx,
		"SELECT id, user_id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at FROM sessions WHERE id = ?", id)

	var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, userID string
	var sessionID string
	var workingDir sql.NullString

	err := row.Scan(&sessionID, &userID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Ok listen up, we used to only store messages in the database, but now we
	// store messages and sub-sessions. So we need to handle both cases.
	// We do this in a kind of hacky way, but it works. "AgentFilename" is always present
	// in a message in the old format, so we check for it to determine the format.
	var items []Item
	var messages []Message
	if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
		return nil, err
	}
	if len(messages) > 0 && messages[0].AgentFilename == "" {
		if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
			return nil, err
		}
	} else {
		items = convertMessagesToItems(messages)
	}

	toolsApproved, err := strconv.ParseBool(toolsApprovedStr)
	if err != nil {
		return nil, err
	}

	inputTokens, err := strconv.Atoi(inputTokensStr)
	if err != nil {
		return nil, err
	}

	outputTokens, err := strconv.Atoi(outputTokensStr)
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

	return &Session{
		ID:              sessionID,
		UserID:          userID,
		Title:           titleStr,
		Messages:        items,
		ToolsApproved:   toolsApproved,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		Cost:            cost,
		SendUserMessage: sendUserMessage,
		MaxIterations:   maxIterations,
		CreatedAt:       createdAt,
		WorkingDir:      workingDir.String,
	}, nil
}

// GetSessions retrieves all sessions
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at FROM sessions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr string
		var sessionID string
		var workingDir sql.NullString

		err := rows.Scan(&sessionID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr)
		if err != nil {
			return nil, err
		}

		// Ok listen up, we used to only store messages in the database, but now we
		// store messages and sub-sessions. So we need to handle both cases.
		// We do this in a kind of hacky way, but it works. "AgentFilename" is always present
		// in a message in the old format, so we check for it to determine the format.
		var items []Item
		var messages []Message
		if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
			return nil, err
		}
		if len(messages) > 0 && messages[0].AgentFilename == "" {
			if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
				return nil, err
			}
		} else {
			items = convertMessagesToItems(messages)
		}

		toolsApproved, err := strconv.ParseBool(toolsApprovedStr)
		if err != nil {
			return nil, err
		}

		inputTokens, err := strconv.Atoi(inputTokensStr)
		if err != nil {
			return nil, err
		}

		outputTokens, err := strconv.Atoi(outputTokensStr)
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

		session := &Session{
			ID:              sessionID,
			Title:           titleStr,
			Messages:        items,
			ToolsApproved:   toolsApproved,
			InputTokens:     inputTokens,
			OutputTokens:    outputTokens,
			Cost:            cost,
			SendUserMessage: sendUserMessage,
			MaxIterations:   maxIterations,
			CreatedAt:       createdAt,
			WorkingDir:      workingDir.String,
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionsByAgent retrieves all sessions for a specific agent
func (s *SQLiteSessionStore) GetSessionsByAgent(ctx context.Context, agentFilename string) ([]*Session, error) {
	allSessions, err := s.GetSessions(ctx)
	if err != nil {
		return nil, err
	}

	var filteredSessions []*Session
	for _, session := range allSessions {
		// Check if any message in this session belongs to the specified agent
		hasAgentMessage := false
		for _, item := range session.Messages {
			if item.Message != nil && item.Message.AgentFilename == agentFilename {
				hasAgentMessage = true
				break
			}
		}

		if hasAgentMessage {
			filteredSessions = append(filteredSessions, session)
		}
	}

	return filteredSessions, nil
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

// UpdateSession updates an existing session
func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	itemsJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET messages = ?, title = ?, tools_approved = ?, input_tokens = ?, output_tokens = ?, cost = ?, send_user_message = ?, max_iterations = ?, working_dir = ? WHERE id = ?",
		string(itemsJSON), session.Title, session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.ID)
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

// GetSessionsByUser retrieves all sessions for a specific user
func (s *SQLiteSessionStore) GetSessionsByUser(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, user_id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at FROM sessions WHERE user_id = ? ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, sessionUserID string
		var sessionID string
		var workingDir sql.NullString

		err := rows.Scan(&sessionID, &sessionUserID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr)
		if err != nil {
			return nil, err
		}

		// Parse messages
		var items []Item
		var messages []Message
		if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
			return nil, err
		}
		if len(messages) > 0 && messages[0].AgentFilename == "" {
			if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
				return nil, err
			}
		} else {
			items = convertMessagesToItems(messages)
		}

		toolsApproved, _ := strconv.ParseBool(toolsApprovedStr)
		inputTokens, _ := strconv.Atoi(inputTokensStr)
		outputTokens, _ := strconv.Atoi(outputTokensStr)
		cost, _ := strconv.ParseFloat(costStr, 64)
		sendUserMessage, _ := strconv.ParseBool(sendUserMessageStr)
		maxIterations, _ := strconv.Atoi(maxIterationsStr)
		createdAt, _ := time.Parse(time.RFC3339, createdAtStr)

		session := &Session{
			ID:              sessionID,
			UserID:          sessionUserID,
			Title:           titleStr,
			Messages:        items,
			ToolsApproved:   toolsApproved,
			InputTokens:     inputTokens,
			OutputTokens:    outputTokens,
			Cost:            cost,
			SendUserMessage: sendUserMessage,
			MaxIterations:   maxIterations,
			CreatedAt:       createdAt,
			WorkingDir:      workingDir.String,
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionsByUserAndAgent retrieves sessions for a specific user and agent
func (s *SQLiteSessionStore) GetSessionsByUserAndAgent(ctx context.Context, userID, agentFilename string) ([]*Session, error) {
	userSessions, err := s.GetSessionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var filteredSessions []*Session
	for _, session := range userSessions {
		// Check if any message in this session belongs to the specified agent
		hasAgentMessage := false
		for _, item := range session.Messages {
			if item.Message != nil && item.Message.AgentFilename == agentFilename {
				hasAgentMessage = true
				break
			}
		}

		if hasAgentMessage {
			filteredSessions = append(filteredSessions, session)
		}
	}

	return filteredSessions, nil
}

// IsSessionOwnedByUser checks if a session belongs to a specific user
func (s *SQLiteSessionStore) IsSessionOwnedByUser(ctx context.Context, sessionID, userID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ? AND user_id = ?", sessionID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetDB returns the underlying database connection
func (s *SQLiteSessionStore) GetDB() *sql.DB {
	return s.db
}

// Close closes the database connection
func (s *SQLiteSessionStore) Close() error {
	return s.db.Close()
}
