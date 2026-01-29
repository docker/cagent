package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/docker/cagent/pkg/chat"
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

// Store defines the interface for session storage
type Store interface {
	// === Core session operations ===
	AddSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessions(ctx context.Context) ([]*Session, error)
	GetSessionSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error // Updates metadata only (not messages/items)
	SetSessionStarred(ctx context.Context, id string, starred bool) error

	// === Granular item operations ===

	// AddMessage adds a message to a session at the next position.
	// Returns the ID of the created message item.
	AddMessage(ctx context.Context, sessionID string, msg *Message) (int64, error)

	// UpdateMessage updates an existing message by its ID.
	// This is used to finalize streaming messages with complete content.
	UpdateMessage(ctx context.Context, messageID int64, msg *Message) error

	// AddSubSession creates a sub-session and links it to the parent.
	// The sub-session is stored as a separate session row with parent_id set.
	AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) error

	// AddSummary adds a summary item to a session at the next position
	AddSummary(ctx context.Context, sessionID, summary string) error

	// === Granular metadata updates ===

	// UpdateSessionTokens updates only token/cost fields
	UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, cost float64) error

	// UpdateSessionTitle updates only the title
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
}

type InMemorySessionStore struct {
	sessions  *concurrent.Map[string, *Session]
	messageID int64 // simple counter for message IDs
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
// Note: Like SQLite, this only stores metadata. Messages are stored separately via AddMessage.
func (s *InMemorySessionStore) UpdateSession(_ context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	// Create a shallow copy of the session
	newSession := *session
	newSession.Messages = nil // Messages stored separately via AddMessage

	// Preserve existing messages if session already exists
	if existing, exists := s.sessions.Load(session.ID); exists {
		newSession.Messages = existing.Messages
	}

	s.sessions.Store(session.ID, &newSession)
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

// AddMessage adds a message to a session at the next position.
// Returns the ID of the created message (for in-memory, this is a simple counter).
func (s *InMemorySessionStore) AddMessage(_ context.Context, sessionID string, msg *Message) (int64, error) {
	if sessionID == "" {
		return 0, ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return 0, ErrNotFound
	}
	s.messageID++
	msg.ID = s.messageID
	session.AddMessage(msg)
	return s.messageID, nil
}

// UpdateMessage updates an existing message by its ID.
func (s *InMemorySessionStore) UpdateMessage(_ context.Context, messageID int64, msg *Message) error {
	// For in-memory store, we need to find the message across all sessions
	var found bool
	s.sessions.Range(func(_ string, session *Session) bool {
		for i := range session.Messages {
			if session.Messages[i].Message != nil && session.Messages[i].Message.ID == messageID {
				// Preserve the message ID when updating
				msg.ID = messageID
				session.Messages[i].Message = msg
				found = true
				return false
			}
		}
		return true
	})
	if !found {
		return ErrNotFound
	}
	return nil
}

// AddSubSession creates a sub-session and links it to the parent.
func (s *InMemorySessionStore) AddSubSession(_ context.Context, parentSessionID string, subSession *Session) error {
	if parentSessionID == "" {
		return ErrEmptyID
	}
	parent, exists := s.sessions.Load(parentSessionID)
	if !exists {
		return ErrNotFound
	}
	subSession.ParentID = parentSessionID
	s.sessions.Store(subSession.ID, subSession)
	parent.AddSubSession(subSession)
	return nil
}

// AddSummary adds a summary item to a session at the next position.
func (s *InMemorySessionStore) AddSummary(_ context.Context, sessionID, summary string) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return ErrNotFound
	}
	session.Messages = append(session.Messages, Item{Summary: summary})
	return nil
}

// querier is an interface that abstracts *sql.DB and *sql.Tx for query operations.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// SQLiteSessionStore implements Store using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// syncMessagesColumn rebuilds the messages JSON column from session_items for backward compatibility.
// This allows older versions of cagent to read sessions created by newer versions.
func (s *SQLiteSessionStore) syncMessagesColumn(ctx context.Context, sessionID string) error {
	return s.syncMessagesColumnWith(ctx, s.db, sessionID)
}

// syncMessagesColumnTx is like syncMessagesColumn but uses an existing transaction.
func (s *SQLiteSessionStore) syncMessagesColumnTx(ctx context.Context, tx *sql.Tx, sessionID string) error {
	return s.syncMessagesColumnWith(ctx, tx, sessionID)
}

// syncMessagesColumnWith rebuilds the messages JSON column using the provided querier.
func (s *SQLiteSessionStore) syncMessagesColumnWith(ctx context.Context, q querier, sessionID string) error {
	items, err := s.loadSessionItemsWith(ctx, q, sessionID)
	if err != nil {
		return fmt.Errorf("loading session items: %w", err)
	}

	messagesJSON, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshaling messages: %w", err)
	}

	_, err = q.ExecContext(ctx, "UPDATE sessions SET messages = ? WHERE id = ?", string(messagesJSON), sessionID)
	return err
}

// UpdateSessionTokens updates only token/cost fields.
func (s *InMemorySessionStore) UpdateSessionTokens(_ context.Context, sessionID string, inputTokens, outputTokens int64, cost float64) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return ErrNotFound
	}
	session.InputTokens = inputTokens
	session.OutputTokens = outputTokens
	session.Cost = cost
	return nil
}

// UpdateSessionTitle updates only the title.
func (s *InMemorySessionStore) UpdateSessionTitle(_ context.Context, sessionID, title string) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return ErrNotFound
	}
	session.Title = title
	return nil
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(path string) (Store, error) {
	store, err := openAndMigrateSQLiteStore(path)
	if err != nil {
		// If migrations failed, try to recover by backing up the database and starting fresh
		slog.Warn("Failed to open session store, attempting recovery", "error", err)

		backupErr := backupDatabase(path)
		if backupErr != nil {
			// Return the original error if backup failed
			slog.Error("Failed to backup database for recovery", "error", backupErr)
			return nil, fmt.Errorf("migration failed: %w (backup also failed: %v)", err, backupErr)
		}

		// Try again with a fresh database
		store, err = openAndMigrateSQLiteStore(path)
		if err != nil {
			return nil, fmt.Errorf("migration failed even after database reset: %w", err)
		}

		slog.Info("Successfully recovered session store with fresh database")
	}

	return store, nil
}

// openAndMigrateSQLiteStore opens the database and runs migrations
func openAndMigrateSQLiteStore(path string) (*SQLiteSessionStore, error) {
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
		db.Close()
		return nil, err
	}

	return &SQLiteSessionStore{db: db}, nil
}

// backupDatabase moves the database file (and related WAL files) to a backup
func backupDatabase(path string) error {
	backupPath := path + ".bak"

	slog.Info("Backing up database", "from", path, "to", backupPath)

	// Move the main database file
	if err := os.Rename(path, backupPath); err != nil {
		if os.IsNotExist(err) {
			// No database file to backup, that's fine
			return nil
		}
		return fmt.Errorf("failed to move database file: %w", err)
	}

	// Also move WAL and SHM files if they exist (SQLite WAL mode artifacts)
	walPath := path + "-wal"
	if _, err := os.Stat(walPath); err == nil {
		if err := os.Rename(walPath, backupPath+"-wal"); err != nil {
			slog.Warn("Failed to move WAL file", "error", err)
		}
	}

	shmPath := path + "-shm"
	if _, err := os.Stat(shmPath); err == nil {
		if err := os.Rename(shmPath, backupPath+"-shm"); err != nil {
			slog.Warn("Failed to move SHM file", "error", err)
		}
	}

	return nil
}

// AddSession adds a new session to the store, including any messages
func (s *SQLiteSessionStore) AddSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
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

	// Use NULL for empty parent_id to avoid foreign key constraint issues
	var parentID any
	if session.ParentID != "" {
		parentID = session.ParentID
	}

	// Use a transaction to insert session and its items
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx,
		"INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, parentID)
	if err != nil {
		return err
	}

	// Insert all messages into session_items
	for position, item := range session.Messages {
		if err := s.addItemTx(ctx, tx, session.ID, position, item); err != nil {
			return fmt.Errorf("adding item at position %d: %w", position, err)
		}
	}

	return tx.Commit()
}

// scanSession scans a single row into a Session struct
// Note: Messages are loaded separately from session_items table
func scanSession(scanner interface {
	Scan(dest ...any) error
},
) (*Session, error) {
	var toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, starredStr, agentModelOverridesJSON, customModelsUsedJSON, thinkingStr string
	var sessionID string
	var workingDir sql.NullString
	var permissionsJSON sql.NullString
	var parentID sql.NullString

	err := scanner.Scan(&sessionID, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr, &starredStr, &permissionsJSON, &agentModelOverridesJSON, &customModelsUsedJSON, &thinkingStr, &parentID)
	if err != nil {
		return nil, err
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
		Messages:            nil, // Loaded separately from session_items
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
		ParentID:            parentID.String,
	}, nil
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	row := s.db.QueryRowContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE id = ?", id)

	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Load messages from session_items table
	items, err := s.loadSessionItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("loading session items: %w", err)
	}
	sess.Messages = items

	return sess, nil
}

// sessionItemRow holds the raw data from a session_items row
type sessionItemRow struct {
	position     int
	itemType     string
	agentName    sql.NullString
	messageJSON  sql.NullString
	implicit     bool
	subsessionID sql.NullString
	summaryText  sql.NullString
}

// loadSessionItems loads all items for a session from the session_items table.
// If no items exist in session_items, it falls back to the legacy messages JSON column
// for backward compatibility with sessions created by older cagent versions.
func (s *SQLiteSessionStore) loadSessionItems(ctx context.Context, sessionID string) ([]Item, error) {
	return s.loadSessionItemsWith(ctx, s.db, sessionID)
}

// loadSessionItemsWith loads items using the provided querier (db or tx).
func (s *SQLiteSessionStore) loadSessionItemsWith(ctx context.Context, q querier, sessionID string) ([]Item, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT position, item_type, agent_name, message_json, implicit, subsession_id, summary_text
		 FROM session_items WHERE session_id = ? ORDER BY position`, sessionID)
	if err != nil {
		return nil, err
	}

	// First, collect all raw row data so we can close the result set
	// before making any recursive calls (SQLite doesn't allow concurrent queries)
	var rawRows []sessionItemRow
	for rows.Next() {
		var row sessionItemRow
		if err := rows.Scan(&row.position, &row.itemType, &row.agentName, &row.messageJSON, &row.implicit, &row.subsessionID, &row.summaryText); err != nil {
			rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// If no session_items found, fall back to legacy messages column
	if len(rawRows) == 0 {
		return s.loadMessagesFromLegacyColumn(ctx, sessionID)
	}

	// Now process the collected rows, making recursive calls as needed
	var items []Item
	for _, row := range rawRows {
		switch row.itemType {
		case "message":
			var chatMsg chat.Message
			if err := json.Unmarshal([]byte(row.messageJSON.String), &chatMsg); err != nil {
				return nil, fmt.Errorf("unmarshaling message at position %d: %w", row.position, err)
			}
			items = append(items, Item{
				Message: &Message{
					AgentName: row.agentName.String,
					Message:   chatMsg,
					Implicit:  row.implicit,
				},
			})

		case "subsession":
			// Recursively load sub-session
			subSession, err := s.loadSessionWith(ctx, q, row.subsessionID.String)
			if err != nil {
				return nil, fmt.Errorf("getting sub-session %s: %w", row.subsessionID.String, err)
			}
			items = append(items, Item{SubSession: subSession})

		case "summary":
			items = append(items, Item{Summary: row.summaryText.String})
		}
	}

	return items, nil
}

// loadSessionWith loads a session using the provided querier.
func (s *SQLiteSessionStore) loadSessionWith(ctx context.Context, q querier, id string) (*Session, error) {
	row := q.QueryRowContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE id = ?", id)

	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Load messages
	items, err := s.loadSessionItemsWith(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("loading session items: %w", err)
	}
	sess.Messages = items

	return sess, nil
}

// loadMessagesFromLegacyColumn loads messages from the legacy messages JSON column.
// This is used for backward compatibility with sessions created by older cagent versions
// that haven't been migrated to the session_items table yet.
func (s *SQLiteSessionStore) loadMessagesFromLegacyColumn(ctx context.Context, sessionID string) ([]Item, error) {
	var messagesJSON sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT messages FROM sessions WHERE id = ?", sessionID).Scan(&messagesJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if !messagesJSON.Valid || messagesJSON.String == "" || messagesJSON.String == "[]" {
		return nil, nil
	}

	var items []Item
	if err := json.Unmarshal([]byte(messagesJSON.String), &items); err != nil {
		return nil, fmt.Errorf("unmarshaling legacy messages: %w", err)
	}

	return items, nil
}

// GetSessions retrieves all root sessions (excludes sub-sessions)
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE parent_id IS NULL OR parent_id = '' ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect sessions first to close the rows before loading items
	var sessions []*Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load messages for each session
	for _, session := range sessions {
		items, err := s.loadSessionItems(ctx, session.ID)
		if err != nil {
			return nil, fmt.Errorf("loading items for session %s: %w", session.ID, err)
		}
		session.Messages = items
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing (excludes sub-sessions).
// This is much faster than GetSessions as it doesn't load message content.
func (s *SQLiteSessionStore) GetSessionSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, created_at, starred FROM sessions WHERE parent_id IS NULL OR parent_id = '' ORDER BY created_at DESC")
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

// UpdateSession updates an existing session's metadata, or creates it if it doesn't exist (upsert).
// Only metadata is modified - use AddMessage, AddSubSession, AddSummary for items.
// Messages are persisted separately via events to avoid duplication.
func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
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

	// Use NULL for empty parent_id to avoid foreign key constraint issues
	var parentID any
	if session.ParentID != "" {
		parentID = session.ParentID
	}

	// Use a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Use INSERT OR REPLACE for upsert behavior - creates if not exists, updates if exists
	_, err = tx.ExecContext(ctx,
		`INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
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
		   thinking = excluded.thinking,
		   parent_id = excluded.parent_id`,
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir,
		session.CreatedAt.Format(time.RFC3339), session.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, parentID)
	if err != nil {
		return err
	}

	// Note: Messages are NOT persisted here. They are persisted via events
	// (UserMessageEvent, MessageAddedEvent, etc.) to avoid duplication.

	return tx.Commit()
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

// AddMessage adds a message to a session at the next position.
// Returns the ID of the created message item.
func (s *SQLiteSessionStore) AddMessage(ctx context.Context, sessionID string, msg *Message) (int64, error) {
	if sessionID == "" {
		return 0, ErrEmptyID
	}

	msgJSON, err := json.Marshal(msg.Message)
	if err != nil {
		return 0, fmt.Errorf("marshaling message: %w", err)
	}

	// Insert a new message at the next position
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO session_items (session_id, position, item_type, agent_name, message_json, implicit)
		 VALUES (?, (SELECT COALESCE(MAX(position), -1) + 1 FROM session_items WHERE session_id = ?), 'message', ?, ?, ?)`,
		sessionID, sessionID, msg.AgentName, string(msgJSON), msg.Implicit)
	if err != nil {
		return 0, fmt.Errorf("inserting message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting last insert id: %w", err)
	}

	// Update messages column for backward compatibility with older cagent versions
	if err := s.syncMessagesColumn(ctx, sessionID); err != nil {
		slog.Warn("[STORE] Failed to sync messages column", "session_id", sessionID, "error", err)
	}

	slog.Debug("[STORE] AddMessage", "session_id", sessionID, "message_id", id, "role", msg.Message.Role, "agent", msg.AgentName)
	return id, nil
}

// UpdateMessage updates an existing message by its ID.
func (s *SQLiteSessionStore) UpdateMessage(ctx context.Context, messageID int64, msg *Message) error {
	msgJSON, err := json.Marshal(msg.Message)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE session_items SET message_json = ?, implicit = ? WHERE id = ?`,
		string(msgJSON), msg.Implicit, messageID)
	if err != nil {
		return fmt.Errorf("updating message: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	// Get session ID for this message to sync the messages column
	var sessionID string
	err = s.db.QueryRowContext(ctx, "SELECT session_id FROM session_items WHERE id = ?", messageID).Scan(&sessionID)
	if err == nil {
		if syncErr := s.syncMessagesColumn(ctx, sessionID); syncErr != nil {
			slog.Warn("[STORE] Failed to sync messages column", "session_id", sessionID, "error", syncErr)
		}
	}

	slog.Debug("[STORE] UpdateMessage", "message_id", messageID, "role", msg.Message.Role, "agent", msg.AgentName)
	return nil
}

// AddSubSession creates a sub-session and links it to the parent.
func (s *SQLiteSessionStore) AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) error {
	if parentSessionID == "" || subSession.ID == "" {
		return ErrEmptyID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Set parent_id on sub-session
	subSession.ParentID = parentSessionID

	// 2. Insert sub-session as a new session row
	if err := s.addSessionTx(ctx, tx, subSession); err != nil {
		return fmt.Errorf("inserting sub-session: %w", err)
	}

	// 3. Recursively add all items from the sub-session
	for i, item := range subSession.Messages {
		if err := s.addItemTx(ctx, tx, subSession.ID, i, item); err != nil {
			return fmt.Errorf("inserting sub-session item %d: %w", i, err)
		}
	}

	// 4. Add reference in parent's items
	_, err = tx.ExecContext(ctx,
		`INSERT INTO session_items (session_id, position, item_type, subsession_id)
		 VALUES (?, (SELECT COALESCE(MAX(position), -1) + 1 FROM session_items WHERE session_id = ?), 'subsession', ?)`,
		parentSessionID, parentSessionID, subSession.ID)
	if err != nil {
		return fmt.Errorf("inserting subsession reference: %w", err)
	}

	// 5. Update messages column for both parent and sub-session for backward compatibility
	if err := s.syncMessagesColumnTx(ctx, tx, parentSessionID); err != nil {
		slog.Warn("[STORE] Failed to sync parent messages column", "session_id", parentSessionID, "error", err)
	}
	if err := s.syncMessagesColumnTx(ctx, tx, subSession.ID); err != nil {
		slog.Warn("[STORE] Failed to sync sub-session messages column", "session_id", subSession.ID, "error", err)
	}

	return tx.Commit()
}

// addSessionTx inserts a session within a transaction.
func (s *SQLiteSessionStore) addSessionTx(ctx context.Context, tx *sql.Tx, session *Session) error {
	permissionsJSON := ""
	if session.Permissions != nil {
		permBytes, err := json.Marshal(session.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	agentModelOverridesJSON := "{}"
	if len(session.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(session.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	customModelsUsedJSON := "[]"
	if len(session.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(session.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	// Use NULL for empty parent_id to avoid foreign key constraint issues
	var parentID any
	if session.ParentID != "" {
		parentID = session.ParentID
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations,
		session.WorkingDir, session.CreatedAt.Format(time.RFC3339), session.Starred,
		permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, parentID)
	return err
}

// addItemTx inserts a session item within a transaction.
func (s *SQLiteSessionStore) addItemTx(ctx context.Context, tx *sql.Tx, sessionID string, position int, item Item) error {
	switch {
	case item.Message != nil:
		msgJSON, err := json.Marshal(item.Message.Message)
		if err != nil {
			return fmt.Errorf("marshaling message: %w", err)
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO session_items (session_id, position, item_type, agent_name, message_json, implicit)
			 VALUES (?, ?, 'message', ?, ?, ?)`,
			sessionID, position, item.Message.AgentName, string(msgJSON), item.Message.Implicit)
		return err

	case item.SubSession != nil:
		// Recursively add the sub-session
		subSession := item.SubSession
		subSession.ParentID = sessionID

		if err := s.addSessionTx(ctx, tx, subSession); err != nil {
			return fmt.Errorf("inserting nested sub-session: %w", err)
		}

		for i, subItem := range subSession.Messages {
			if err := s.addItemTx(ctx, tx, subSession.ID, i, subItem); err != nil {
				return fmt.Errorf("inserting nested sub-session item %d: %w", i, err)
			}
		}

		_, err := tx.ExecContext(ctx,
			`INSERT INTO session_items (session_id, position, item_type, subsession_id)
			 VALUES (?, ?, 'subsession', ?)`,
			sessionID, position, subSession.ID)
		return err

	case item.Summary != "":
		_, err := tx.ExecContext(ctx,
			`INSERT INTO session_items (session_id, position, item_type, summary_text)
			 VALUES (?, ?, 'summary', ?)`,
			sessionID, position, item.Summary)
		return err

	default:
		return nil // Empty item, skip
	}
}

// AddSummary adds a summary item to a session at the next position.
func (s *SQLiteSessionStore) AddSummary(ctx context.Context, sessionID, summary string) error {
	if sessionID == "" {
		return ErrEmptyID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_items (session_id, position, item_type, summary_text)
		 VALUES (?, (SELECT COALESCE(MAX(position), -1) + 1 FROM session_items WHERE session_id = ?), 'summary', ?)`,
		sessionID, sessionID, summary)
	if err != nil {
		return err
	}

	// Update messages column for backward compatibility with older cagent versions
	if syncErr := s.syncMessagesColumn(ctx, sessionID); syncErr != nil {
		slog.Warn("[STORE] Failed to sync messages column", "session_id", sessionID, "error", syncErr)
	}

	return nil
}

// UpdateSessionTokens updates only token/cost fields.
func (s *SQLiteSessionStore) UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, cost float64) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET input_tokens = ?, output_tokens = ?, cost = ? WHERE id = ?",
		inputTokens, outputTokens, cost, sessionID)
	return err
}

// UpdateSessionTitle updates only the title.
func (s *SQLiteSessionStore) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET title = ? WHERE id = ?",
		title, sessionID)
	return err
}
