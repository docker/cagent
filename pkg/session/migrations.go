package session

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Migration represents a database migration
type Migration struct {
	ID          int
	Name        string
	Description string
	UpSQL       string
	DownSQL     string
	AppliedAt   time.Time
}

// MigrationManager handles database migrations
type MigrationManager struct {
	db *sql.DB
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db *sql.DB) *MigrationManager {
	return &MigrationManager{db: db}
}

// InitializeMigrations sets up the migrations table and runs pending migrations
func (m *MigrationManager) InitializeMigrations(ctx context.Context) error {
	// Create migrations table if it doesn't exist
	err := m.createMigrationsTable(ctx)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Run all pending migrations
	err = m.RunPendingMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to run pending migrations: %w", err)
	}

	return nil
}

// createMigrationsTable creates the migrations tracking table
func (m *MigrationManager) createMigrationsTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			applied_at TEXT NOT NULL
		)
	`)
	return err
}

// RunPendingMigrations executes all migrations that haven't been applied yet
func (m *MigrationManager) RunPendingMigrations(ctx context.Context) error {
	migrations := getAllMigrations()

	for _, migration := range migrations {
		applied, err := m.isMigrationApplied(ctx, migration.Name)
		if err != nil {
			return fmt.Errorf("failed to check if migration %s is applied: %w", migration.Name, err)
		}

		if !applied {
			err = m.applyMigration(ctx, &migration)
			if err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
			}
		}
	}

	return nil
}

// isMigrationApplied checks if a migration has already been applied
func (m *MigrationManager) isMigrationApplied(ctx context.Context, name string) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE name = ?", name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// applyMigration applies a single migration
func (m *MigrationManager) applyMigration(ctx context.Context, migration *Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// TODO: handle error
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(ctx, migration.UpSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO migrations (id, name, description, applied_at) VALUES (?, ?, ?, ?)",
		migration.ID, migration.Name, migration.Description, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	return nil
}

// GetAppliedMigrations returns a list of applied migrations
func (m *MigrationManager) GetAppliedMigrations(ctx context.Context) ([]Migration, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT id, name, description, applied_at FROM migrations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []Migration
	for rows.Next() {
		var migration Migration
		var appliedAtStr string

		err := rows.Scan(&migration.ID, &migration.Name, &migration.Description, &appliedAtStr)
		if err != nil {
			return nil, err
		}

		migration.AppliedAt, err = time.Parse(time.RFC3339, appliedAtStr)
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// getAllMigrations returns all available migrations in order
func getAllMigrations() []Migration {
	return []Migration{
		{
			ID:          1,
			Name:        "001_add_tools_approved_column",
			Description: "Add tools_approved column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN tools_approved BOOLEAN DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN tools_approved`,
		},
		{
			ID:          2,
			Name:        "002_add_usage_column",
			Description: "Add usage column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN input_tokens INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN input_tokens`,
		},
		{
			ID:          3,
			Name:        "003_add_output_tokens_column",
			Description: "Add output_tokens column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN output_tokens INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN output_tokens`,
		},
		{
			ID:          4,
			Name:        "004_add_title_column",
			Description: "Add title column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN title TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN title`,
		},
		{
			ID:          5,
			Name:        "005_add_cost_column",
			Description: "Add cost column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN cost REAL DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN cost`,
		},
		{
			ID:          6,
			Name:        "006_add_send_user_message_column",
			Description: "Add send_user_message column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN send_user_message BOOLEAN DEFAULT 1`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN send_user_message`,
		},
		{
			ID:          7,
			Name:        "007_add_max_iterations_column",
			Description: "Add max_iterations column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN max_iterations INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN max_iterations`,
		},
		{
			ID:          8,
			Name:        "008_add_working_dir_column",
			Description: "Add working_dir column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN working_dir TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN working_dir`,
		},
		{
			ID:          9,
			Name:        "009_add_starred_column",
			Description: "Add starred column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN starred BOOLEAN DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN starred`,
		},
		{
			ID:          10,
			Name:        "010_add_permissions_column",
			Description: "Add permissions column to sessions table for session-level permission overrides",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN permissions TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN permissions`,
		},
		{
			ID:          11,
			Name:        "011_add_agent_model_overrides_column",
			Description: "Add agent_model_overrides column to sessions table for per-session model switching",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN agent_model_overrides TEXT DEFAULT '{}'`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN agent_model_overrides`,
		},
		{
			ID:          12,
			Name:        "012_add_custom_models_used_column",
			Description: "Add custom_models_used column to sessions table for tracking custom models used in session",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN custom_models_used TEXT DEFAULT '[]'`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN custom_models_used`,
		},
		{
			ID:          13,
			Name:        "013_add_thinking_column",
			Description: "Add thinking column to sessions table for session-level thinking toggle (default enabled)",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN thinking BOOLEAN DEFAULT 1`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN thinking`,
		},
	}
}
