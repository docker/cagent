package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/memory"
	"github.com/docker/cagent/pkg/sqliteutil"
	"github.com/google/uuid"
)

// Driver implements the memory.Driver interface using SQLite
type Driver struct {
	db *sql.DB
}

// Factory creates SQLite drivers
type Factory struct{}

var _ memory.Factory = (*Factory)(nil)

func (f *Factory) CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (memory.Driver, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("sqlite driver requires a path")
	}

	db, err := sqliteutil.OpenDB(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		created_at TEXT,
		content TEXT,
		metadata TEXT
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create memories table: %w", err)
	}

	return &Driver{db: db}, nil
}

func (d *Driver) Store(ctx context.Context, key string, value string) error {
	if key == "" {
		key = uuid.New().String()
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO memories (id, created_at, content, metadata) VALUES (?, ?, ?, ?)",
		key, createdAt, value, "{}")
	if err != nil {
		return fmt.Errorf("failed to store memory: %w", err)
	}
	return nil
}

func (d *Driver) Retrieve(ctx context.Context, query memory.Query) ([]memory.Entry, error) {
	var rows *sql.Rows
	var err error

	if query.ID != "" {
		rows, err = d.db.QueryContext(ctx,
			"SELECT id, created_at, content FROM memories WHERE id = ?",
			query.ID)
	} else if query.Semantic != "" {
		// Semantic search not yet implemented for SQLite
		// For now, fall back to retrieving all memories
		// Future: Use FTS5 or vector extension for semantic search
		sqlQuery := "SELECT id, created_at, content FROM memories ORDER BY created_at DESC"
		if query.Limit > 0 {
			sqlQuery = fmt.Sprintf("%s LIMIT %d", sqlQuery, query.Limit)
		}
		rows, err = d.db.QueryContext(ctx, sqlQuery)
	} else {
		sqlQuery := "SELECT id, created_at, content FROM memories ORDER BY created_at DESC"
		if query.Limit > 0 {
			sqlQuery = fmt.Sprintf("%s LIMIT %d", sqlQuery, query.Limit)
		}
		rows, err = d.db.QueryContext(ctx, sqlQuery)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve memories: %w", err)
	}
	defer rows.Close()

	var entries []memory.Entry
	for rows.Next() {
		var e memory.Entry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Content); err != nil {
			return nil, fmt.Errorf("failed to scan memory row: %w", err)
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating memory rows: %w", err)
	}

	return entries, nil
}

func (d *Driver) Delete(ctx context.Context, key string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM memories WHERE id = ?", key)
	if err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}
	return nil
}

func (d *Driver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}
