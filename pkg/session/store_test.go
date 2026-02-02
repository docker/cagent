package session

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
)

func TestStoreAgentName(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent1 := agent.New("test-agent-1", "test prompt 1")
	testAgent2 := agent.New("test-agent-2", "test prompt 2")

	session := &Session{
		ID: "test-session",
		Messages: []Item{
			NewMessageItem(UserMessage("Hello")),
			NewMessageItem(NewAgentMessage(testAgent1, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Hello from test-agent-1",
			})),
			NewMessageItem(NewAgentMessage(testAgent2, &chat.Message{
				Role:    chat.MessageRoleUser,
				Content: "Another message from test-agent-2",
			})),
		},
		InputTokens:  100,
		OutputTokens: 200,
		CreatedAt:    time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "test-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Len(t, retrievedSession.GetAllMessages(), 3)

	// First message should be user message with empty agent name
	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)
	assert.Equal(t, "Hello", retrievedSession.Messages[0].Message.Message.Content)

	// Second message should have the first agent's name
	assert.Equal(t, "test-agent-1", retrievedSession.Messages[1].Message.AgentName)
	assert.Equal(t, "Hello from test-agent-1", retrievedSession.Messages[1].Message.Message.Content)

	// Third message should have the second agent's name
	assert.Equal(t, "test-agent-2", retrievedSession.Messages[2].Message.AgentName)
	assert.Equal(t, "Another message from test-agent-2", retrievedSession.Messages[2].Message.Message.Content)
}

func TestStoreMultipleAgents(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store_multi.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	agent1 := agent.New("agent-1", "agent 1 prompt")
	agent2 := agent.New("agent-2", "agent 2 prompt")

	session := &Session{
		ID:        "multi-agent-session",
		CreatedAt: time.Now(),
		Messages: []Item{
			NewMessageItem(UserMessage("Start conversation")),
			NewMessageItem(NewAgentMessage(agent1, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Response from agent 1",
			})),
			NewMessageItem(NewAgentMessage(agent2, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Response from agent 2",
			})),
		},
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "multi-agent-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Len(t, retrievedSession.Messages, 3)

	// First message should be user message with empty agent name
	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)

	// Second message should have agent-1 name
	assert.Equal(t, "agent-1", retrievedSession.Messages[1].Message.AgentName)
	assert.Equal(t, "Response from agent 1", retrievedSession.Messages[1].Message.Message.Content)

	// Third message should have agent-2 name
	assert.Equal(t, "agent-2", retrievedSession.Messages[2].Message.AgentName)
	assert.Equal(t, "Response from agent 2", retrievedSession.Messages[2].Message.Message.Content)
}

func TestGetSessions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_sessions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1 := &Session{
		ID: "session-1",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Message from session 1",
			})),
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	session2 := &Session{
		ID: "session-2",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Message from session 2",
			})),
		},
		CreatedAt: time.Now(),
	}

	// Store the sessions
	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	// Retrieve all sessions
	sessions, err := store.GetSessions(t.Context())
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	for _, session := range sessions {
		assert.Len(t, session.Messages, 1)
		assert.Equal(t, "test-agent", session.Messages[0].Message.AgentName)
	}
}

func TestGetSessionSummaries(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_session_summaries.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1Time := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	session2Time := time.Now().UTC().Truncate(time.Second)

	session1 := &Session{
		ID:    "session-1",
		Title: "First Session",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "A very long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session1Time,
	}

	session2 := &Session{
		ID:    "session-2",
		Title: "Second Session",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Another long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session2Time,
	}

	// Store the sessions
	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	// Retrieve summaries (should be lightweight, without messages)
	summaries, err := store.GetSessionSummaries(t.Context())
	require.NoError(t, err)
	assert.Len(t, summaries, 2)

	// Summaries should be ordered by created_at DESC (most recent first)
	assert.Equal(t, "session-2", summaries[0].ID)
	assert.Equal(t, "Second Session", summaries[0].Title)
	assert.Equal(t, session2Time, summaries[0].CreatedAt)

	assert.Equal(t, "session-1", summaries[1].ID)
	assert.Equal(t, "First Session", summaries[1].Title)
	assert.Equal(t, session1Time, summaries[1].CreatedAt)
}

func TestStoreAgentNameJSON(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store_json.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	agent1 := agent.New("my-agent", "test prompt")
	agent2 := agent.New("another-agent", "another prompt")

	session := &Session{
		ID: "json-test-session",
		Messages: []Item{
			NewMessageItem(UserMessage("User input")),
			NewMessageItem(NewAgentMessage(agent1, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Response from my-agent",
			})),
			NewMessageItem(NewAgentMessage(agent2, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Response from another-agent",
			})),
		},
		CreatedAt: time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "json-test-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)                  // User message
	assert.Equal(t, "my-agent", retrievedSession.Messages[1].Message.AgentName)      // First agent
	assert.Equal(t, "another-agent", retrievedSession.Messages[2].Message.AgentName) // Second agent
}

func TestNewSQLiteSessionStore_DirectoryNotWritable(t *testing.T) {
	readOnlyDir := filepath.Join(t.TempDir(), "readonly")
	err := os.Mkdir(readOnlyDir, 0o555)
	require.NoError(t, err)

	_, err = NewSQLiteSessionStore(filepath.Join(readOnlyDir, "session.db"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "cannot create database")
	assert.Contains(t, err.Error(), "permission denied or file cannot be created")
}

func TestUpdateSession_LazyCreation(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_lazy.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session but don't add it to the store (simulating lazy creation)
	session := &Session{
		ID:        "lazy-session",
		CreatedAt: time.Now(),
	}

	// Verify session doesn't exist yet
	_, err = store.GetSession(t.Context(), "lazy-session")
	require.ErrorIs(t, err, ErrNotFound)

	// UpdateSession creates the session (upsert) but does NOT persist messages
	// Messages must be added separately via AddMessage
	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Session exists but has no messages yet
	retrieved, err := store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Empty(t, retrieved.Messages)

	// Add messages via AddMessage (the proper way)
	_, err = store.AddMessage(t.Context(), "lazy-session", UserMessage("Hello"))
	require.NoError(t, err)

	_, err = store.AddMessage(t.Context(), "lazy-session", NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hi there!",
	}))
	require.NoError(t, err)

	// Now the session should have messages
	retrieved, err = store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, 2)
	assert.Equal(t, "Hello", retrieved.Messages[0].Message.Message.Content)
	assert.Equal(t, "Hi there!", retrieved.Messages[1].Message.Message.Content)
}

func TestUpdateSession_LazyCreation_InMemory(t *testing.T) {
	store := NewInMemorySessionStore()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session but don't add it to the store
	session := &Session{
		ID:        "lazy-session",
		CreatedAt: time.Now(),
	}

	// Verify session doesn't exist yet
	_, err := store.GetSession(t.Context(), "lazy-session")
	require.ErrorIs(t, err, ErrNotFound)

	// UpdateSession creates the session (upsert) without messages
	// Messages must be added separately via AddMessage (like SQLite behavior)
	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Session exists but has no messages yet
	retrieved, err := store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Empty(t, retrieved.Messages)

	// Add messages via AddMessage
	_, err = store.AddMessage(t.Context(), "lazy-session", UserMessage("Hello"))
	require.NoError(t, err)
	_, err = store.AddMessage(t.Context(), "lazy-session", NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hi there!",
	}))
	require.NoError(t, err)

	// Now the session has 2 messages
	retrieved, err = store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, 2)
}

func TestStorePermissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session with permissions
	session := &Session{
		ID:        "permissions-session",
		CreatedAt: time.Now(),
		Permissions: &PermissionsConfig{
			Allow: []string{"read_*", "think"},
			Deny:  []string{"shell:cmd=rm*", "dangerous_tool"},
		},
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "permissions-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Permissions)

	assert.Equal(t, []string{"read_*", "think"}, retrieved.Permissions.Allow)
	assert.Equal(t, []string{"shell:cmd=rm*", "dangerous_tool"}, retrieved.Permissions.Deny)
}

func TestStorePermissions_NilPermissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_nil_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without permissions (legacy behavior)
	session := &Session{
		ID:        "no-permissions-session",
		CreatedAt: time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "no-permissions-session")
	require.NoError(t, err)

	// Permissions should be nil
	assert.Nil(t, retrieved.Permissions)
}

func TestUpdateSession_Permissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_update_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without permissions
	session := &Session{
		ID:        "update-permissions-session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Update with permissions
	session.Permissions = &PermissionsConfig{
		Allow: []string{"safe_*"},
		Deny:  []string{"dangerous_*"},
	}

	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(t.Context(), "update-permissions-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Permissions)

	assert.Equal(t, []string{"safe_*"}, retrieved.Permissions.Allow)
	assert.Equal(t, []string{"dangerous_*"}, retrieved.Permissions.Deny)
}

func TestAgentModelOverrides_SQLite(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session with model overrides
	session := &Session{
		ID:        "model-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
		AgentModelOverrides: map[string]string{
			"root":       "openai/gpt-4o",
			"researcher": "anthropic/claude-sonnet-4-0",
		},
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "model-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify model overrides were persisted
	assert.Len(t, retrieved.AgentModelOverrides, 2)
	assert.Equal(t, "openai/gpt-4o", retrieved.AgentModelOverrides["root"])
	assert.Equal(t, "anthropic/claude-sonnet-4-0", retrieved.AgentModelOverrides["researcher"])
}

func TestAgentModelOverrides_Update(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides_update.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without model overrides
	session := &Session{
		ID:        "update-model-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Update the session with model overrides
	session.AgentModelOverrides = map[string]string{
		"root": "google/gemini-2.5-flash",
	}

	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(t.Context(), "update-model-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Len(t, retrieved.AgentModelOverrides, 1)
	assert.Equal(t, "google/gemini-2.5-flash", retrieved.AgentModelOverrides["root"])
}

func TestAgentModelOverrides_EmptyMap(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides_empty.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without model overrides (nil map)
	session := &Session{
		ID:        "no-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "no-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify no model overrides (should be nil or empty)
	assert.Empty(t, retrieved.AgentModelOverrides)
}

func TestThinking_Persistence(t *testing.T) {
	t.Parallel()

	t.Run("default is true (thinking enabled)", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-default-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  true, // Default value for new sessions
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-default-session")
		require.NoError(t, err)
		assert.True(t, retrieved.Thinking)
	})

	t.Run("persists when set to false (thinking disabled)", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-disabled-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  false,
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-disabled-session")
		require.NoError(t, err)
		assert.False(t, retrieved.Thinking)
	})

	t.Run("updates correctly via toggle", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-toggle-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  true,
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		// Simulate toggle: true -> false
		session.Thinking = !session.Thinking
		err = store.UpdateSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-toggle-session")
		require.NoError(t, err)
		assert.False(t, retrieved.Thinking)

		// Toggle again: false -> true
		session.Thinking = !session.Thinking
		err = store.UpdateSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err = store.GetSession(t.Context(), "thinking-toggle-session")
		require.NoError(t, err)
		assert.True(t, retrieved.Thinking)
	})
}

func TestNewSQLiteSessionStore_MigrationFailureRecovery(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_migration_recovery.db")
	backupPath := dbPath + ".bak"

	// Create a corrupted database file that will fail migrations
	err := os.WriteFile(dbPath, []byte("not a valid sqlite database"), 0o644)
	require.NoError(t, err)

	// Opening should trigger recovery: backup the corrupt file and create fresh db
	store, err := NewSQLiteSessionStore(dbPath)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Verify a backup was created
	_, err = os.Stat(backupPath)
	require.NoError(t, err, "backup file should exist")

	// Verify the store works with the fresh database
	session := &Session{
		ID:        "test-session",
		CreatedAt: time.Now(),
	}
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "test-session")
	require.NoError(t, err)
	assert.Equal(t, "test-session", retrieved.ID)
}

func TestBackupDatabase(t *testing.T) {
	t.Run("backs up existing database file", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "test.db")
		backupPath := dbPath + ".bak"

		// Create a file to backup
		err := os.WriteFile(dbPath, []byte("test content"), 0o644)
		require.NoError(t, err)

		// Also create WAL and SHM files
		err = os.WriteFile(dbPath+"-wal", []byte("wal content"), 0o644)
		require.NoError(t, err)
		err = os.WriteFile(dbPath+"-shm", []byte("shm content"), 0o644)
		require.NoError(t, err)

		// Backup the database
		err = backupDatabase(dbPath)
		require.NoError(t, err)

		// Original should be gone
		_, err = os.Stat(dbPath)
		assert.True(t, os.IsNotExist(err), "original file should be moved")

		// WAL and SHM should also be gone
		_, err = os.Stat(dbPath + "-wal")
		assert.True(t, os.IsNotExist(err), "WAL file should be moved")
		_, err = os.Stat(dbPath + "-shm")
		assert.True(t, os.IsNotExist(err), "SHM file should be moved")

		// Check backup files exist
		_, err = os.Stat(backupPath)
		require.NoError(t, err, "main backup should exist")
		_, err = os.Stat(backupPath + "-wal")
		require.NoError(t, err, "WAL backup should exist")
		_, err = os.Stat(backupPath + "-shm")
		require.NoError(t, err, "SHM backup should exist")
	})

	t.Run("handles nonexistent file gracefully", func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "nonexistent.db")

		// Backup should succeed (nothing to backup)
		err := backupDatabase(dbPath)
		require.NoError(t, err)
	})
}

// TestBackwardCompatibility_ReadLegacyMessages verifies that new code can read
// sessions that were created by older cagent versions (messages in JSON column only).
func TestBackwardCompatibility_ReadLegacyMessages(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_legacy.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	sqliteStore := store.(*SQLiteSessionStore)

	// Simulate a legacy session by inserting directly into the sessions table
	// with messages in the JSON column and NO entries in session_items
	legacyMessages := []Item{
		NewMessageItem(UserMessage("Hello from legacy")),
		NewMessageItem(&Message{
			AgentName: "test-agent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Hi from legacy agent!",
			},
		}),
	}

	legacyMessagesJSON, err := json.Marshal(legacyMessages)
	require.NoError(t, err)

	_, err = sqliteStore.db.ExecContext(t.Context(),
		`INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking)
		 VALUES (?, ?, 0, 0, 0, 'Legacy Session', 0, 1, 0, '', ?, 0, '', '{}', '[]', 1)`,
		"legacy-session", string(legacyMessagesJSON), time.Now().Format(time.RFC3339))
	require.NoError(t, err)

	// Now read the session using the store API - it should fall back to messages column
	retrieved, err := store.GetSession(t.Context(), "legacy-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify messages were read from the legacy column
	assert.Len(t, retrieved.Messages, 2)
	assert.Equal(t, "Hello from legacy", retrieved.Messages[0].Message.Message.Content)
	assert.Equal(t, "test-agent", retrieved.Messages[1].Message.AgentName)
	assert.Equal(t, "Hi from legacy agent!", retrieved.Messages[1].Message.Message.Content)
}

// TestForwardCompatibility_MessagesColumnPopulated verifies that new code populates
// the messages column so older cagent versions can read sessions.
func TestForwardCompatibility_MessagesColumnPopulated(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_forward.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	sqliteStore := store.(*SQLiteSessionStore)

	// Create a session using the new API
	session := &Session{
		ID:        "new-session",
		CreatedAt: time.Now(),
	}
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages using the new granular API
	_, err = store.AddMessage(t.Context(), "new-session", UserMessage("Hello from new code"))
	require.NoError(t, err)

	_, err = store.AddMessage(t.Context(), "new-session", &Message{
		AgentName: "new-agent",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Response from new agent",
		},
	})
	require.NoError(t, err)

	// Verify messages column is populated (how old cagent would read it)
	var messagesJSON string
	err = sqliteStore.db.QueryRowContext(t.Context(),
		"SELECT messages FROM sessions WHERE id = ?", "new-session").Scan(&messagesJSON)
	require.NoError(t, err)
	assert.NotEmpty(t, messagesJSON)
	assert.NotEqual(t, "[]", messagesJSON)

	// Parse and verify the messages column content
	var items []Item
	err = json.Unmarshal([]byte(messagesJSON), &items)
	require.NoError(t, err)

	assert.Len(t, items, 2)
	assert.Equal(t, "Hello from new code", items[0].Message.Message.Content)
	assert.Equal(t, "new-agent", items[1].Message.AgentName)
	assert.Equal(t, "Response from new agent", items[1].Message.Message.Content)
}

// TestForwardCompatibility_SubSessionPopulated verifies that sub-sessions
// are properly serialized to the messages column for backward compatibility.
func TestForwardCompatibility_SubSessionPopulated(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_subsession.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	sqliteStore := store.(*SQLiteSessionStore)

	// Create parent session
	parentSession := &Session{
		ID:        "parent-session",
		CreatedAt: time.Now(),
	}
	err = store.AddSession(t.Context(), parentSession)
	require.NoError(t, err)

	// Add a message to parent
	_, err = store.AddMessage(t.Context(), "parent-session", UserMessage("Start task"))
	require.NoError(t, err)

	// Create and add a sub-session
	subSession := &Session{
		ID:        "sub-session",
		CreatedAt: time.Now(),
		Messages: []Item{
			NewMessageItem(UserMessage("Sub task")),
			NewMessageItem(&Message{
				AgentName: "sub-agent",
				Message: chat.Message{
					Role:    chat.MessageRoleAssistant,
					Content: "Sub response",
				},
			}),
		},
	}
	err = store.AddSubSession(t.Context(), "parent-session", subSession)
	require.NoError(t, err)

	// Verify parent's messages column contains the sub-session
	var messagesJSON string
	err = sqliteStore.db.QueryRowContext(t.Context(),
		"SELECT messages FROM sessions WHERE id = ?", "parent-session").Scan(&messagesJSON)
	require.NoError(t, err)

	var items []Item
	err = json.Unmarshal([]byte(messagesJSON), &items)
	require.NoError(t, err)

	assert.Len(t, items, 2) // user message + subsession
	assert.Equal(t, "Start task", items[0].Message.Message.Content)
	assert.NotNil(t, items[1].SubSession)
	assert.Equal(t, "sub-session", items[1].SubSession.ID)
	assert.Len(t, items[1].SubSession.Messages, 2)
}

// TestForwardCompatibility_SummaryPopulated verifies that summaries
// are properly serialized to the messages column for backward compatibility.
func TestForwardCompatibility_SummaryPopulated(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_summary.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	sqliteStore := store.(*SQLiteSessionStore)

	// Create session
	session := &Session{
		ID:        "summary-session",
		CreatedAt: time.Now(),
	}
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages and a summary
	_, err = store.AddMessage(t.Context(), "summary-session", UserMessage("Hello"))
	require.NoError(t, err)

	err = store.AddSummary(t.Context(), "summary-session", "This is a summary of the conversation.")
	require.NoError(t, err)

	// Verify messages column contains the summary
	var messagesJSON string
	err = sqliteStore.db.QueryRowContext(t.Context(),
		"SELECT messages FROM sessions WHERE id = ?", "summary-session").Scan(&messagesJSON)
	require.NoError(t, err)

	var items []Item
	err = json.Unmarshal([]byte(messagesJSON), &items)
	require.NoError(t, err)

	assert.Len(t, items, 2)
	assert.Equal(t, "Hello", items[0].Message.Message.Content)
	assert.Equal(t, "This is a summary of the conversation.", items[1].Summary)
}

// TestMigration_ExistingMessagesToSessionItems verifies that the migration
// properly converts legacy messages JSON to session_items table.
func TestMigration_ExistingMessagesToSessionItems(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_migration.db")

	// First, create a database with the legacy schema (before migrations)
	db, err := sql.Open("sqlite", tempDB)
	require.NoError(t, err)

	// Create minimal schema
	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			messages TEXT,
			created_at TEXT
		)
	`)
	require.NoError(t, err)

	// Insert a legacy session with messages
	legacyMessages := []Item{
		NewMessageItem(UserMessage("Legacy message 1")),
		NewMessageItem(&Message{
			AgentName: "legacy-agent",
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Legacy response",
			},
		}),
	}
	legacyJSON, err := json.Marshal(legacyMessages)
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(),
		"INSERT INTO sessions (id, messages, created_at) VALUES (?, ?, ?)",
		"migration-test-session", string(legacyJSON), time.Now().Format(time.RFC3339))
	require.NoError(t, err)

	db.Close()

	// Now open with the store, which runs migrations
	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Session should be readable via the new API
	retrieved, err := store.GetSession(t.Context(), "migration-test-session")
	require.NoError(t, err)

	assert.Len(t, retrieved.Messages, 2)
	assert.Equal(t, "Legacy message 1", retrieved.Messages[0].Message.Message.Content)
	assert.Equal(t, "legacy-agent", retrieved.Messages[1].Message.AgentName)
}
