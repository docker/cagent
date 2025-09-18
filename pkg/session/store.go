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
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error
}

// SQLiteSessionStore implements Store using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
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
		"INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, string(itemsJSON), session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.CreatedAt.Format(time.RFC3339))
	return err
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	row := s.db.QueryRowContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, created_at FROM sessions WHERE id = ?", id)

	var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr string
	var sessionID string

	err := row.Scan(&sessionID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &createdAtStr)
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
		Title:           titleStr,
		Messages:        items,
		ToolsApproved:   toolsApproved,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		Cost:            cost,
		SendUserMessage: sendUserMessage,
		MaxIterations:   maxIterations,
		CreatedAt:       createdAt,
	}, nil
}

// GetSessions retrieves all sessions
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, created_at FROM sessions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]*Session, 0)
	for rows.Next() {
		var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr string
		var sessionID string

		err := rows.Scan(&sessionID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &createdAtStr)
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
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
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
		"UPDATE sessions SET messages = ?, title = ?, tools_approved = ?, input_tokens = ?, output_tokens = ?, cost = ?, send_user_message = ?, max_iterations = ? WHERE id = ?",
		string(itemsJSON), session.Title, session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Cost, session.SendUserMessage, session.MaxIterations, session.ID)
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
