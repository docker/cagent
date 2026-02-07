// Package tuistate provides persistent TUI state storage (tabs, recent/favorite directories).
package tuistate

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/sqliteutil"
)

// Store manages persistent TUI state in a SQLite database.
type Store struct {
	db *sql.DB
}

// New creates a new TUI state store, initializing the database if needed.
func New() (*Store, error) {
	dbPath := filepath.Join(paths.GetDataDir(), "tui_state.db")
	db, err := sqliteutil.OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening TUI state store: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating TUI state store: %w", err)
	}

	return store, nil
}

// migrate runs database migrations.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tabs (
			session_id TEXT PRIMARY KEY,
			working_dir TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_active_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS active_tab (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			session_id TEXT NOT NULL
		);
		
		CREATE TABLE IF NOT EXISTS recent_dirs (
			path TEXT PRIMARY KEY,
			used_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS favorite_dirs (
			path TEXT PRIMARY KEY,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// AddTab adds a new tab to the store.
func (s *Store) AddTab(ctx context.Context, sessionID, workingDir string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tabs (session_id, working_dir, created_at, last_active_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, sessionID, workingDir)
	if err != nil {
		return fmt.Errorf("adding tab: %w", err)
	}

	// Also track the working directory as recently used (skip empty paths)
	if workingDir != "" {
		_, err = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO recent_dirs (path, used_at)
			VALUES (?, CURRENT_TIMESTAMP)
		`, workingDir)
		return err
	}
	return nil
}

// RemoveTab removes a tab from the store.
func (s *Store) RemoveTab(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tabs WHERE session_id = ?`, sessionID)
	return err
}

// SetActiveTab sets the currently active tab.
func (s *Store) SetActiveTab(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO active_tab (id, session_id)
		VALUES (1, ?)
	`, sessionID)
	return err
}

// GetRecentDirs returns the most recently used directories.
func (s *Store) GetRecentDirs(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path FROM recent_dirs
		ORDER BY used_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		dirs = append(dirs, path)
	}
	return dirs, rows.Err()
}

// ClearTabs removes all tabs from the store. Used on clean shutdown.
func (s *Store) ClearTabs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tabs`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM active_tab`)
	return err
}

// AddFavoriteDir adds a directory to the favorites list.
func (s *Store) AddFavoriteDir(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO favorite_dirs (path, added_at)
		VALUES (?, CURRENT_TIMESTAMP)
	`, path)
	return err
}

// RemoveFavoriteDir removes a directory from the favorites list.
func (s *Store) RemoveFavoriteDir(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM favorite_dirs WHERE path = ?`, path)
	return err
}

// GetFavoriteDirs returns all favorite directories, ordered by most recently added.
func (s *Store) GetFavoriteDirs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path FROM favorite_dirs
		ORDER BY added_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		dirs = append(dirs, path)
	}
	return dirs, rows.Err()
}

// IsFavoriteDir checks if a directory is in the favorites list.
func (s *Store) IsFavoriteDir(ctx context.Context, path string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorite_dirs WHERE path = ?`, path).Scan(&count)
	return count > 0, err
}

// ToggleFavoriteDir adds or removes a directory from favorites. Returns the new state (true = now favorite).
func (s *Store) ToggleFavoriteDir(ctx context.Context, path string) (bool, error) {
	isFav, err := s.IsFavoriteDir(ctx, path)
	if err != nil {
		return false, err
	}
	if isFav {
		return false, s.RemoveFavoriteDir(ctx, path)
	}
	return true, s.AddFavoriteDir(ctx, path)
}
