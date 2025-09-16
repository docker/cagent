package sqlite

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"

	"github.com/docker/cagent/pkg/memory/database"
)

type MemoryDatabase struct {
	db *sql.DB
}

func NewMemoryDatabase(path string) (database.Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(context.Background(), "CREATE TABLE IF NOT EXISTS memories (id TEXT PRIMARY KEY, created_at TEXT, memory TEXT)")
	if err != nil {
		return nil, err
	}

	return &MemoryDatabase{db: db}, nil
}

func (m *MemoryDatabase) AddMemory(ctx context.Context, memory database.UserMemory) error {
	if memory.ID == "" {
		return database.ErrEmptyID
	}
	_, err := m.db.ExecContext(ctx, "INSERT INTO memories (id, created_at, memory) VALUES (?, ?, ?)",
		memory.ID, memory.CreatedAt, memory.Memory)
	return err
}

func (m *MemoryDatabase) GetMemories(ctx context.Context) ([]database.UserMemory, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT id, created_at, memory FROM memories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []database.UserMemory
	for rows.Next() {
		var memory database.UserMemory
		err := rows.Scan(&memory.ID, &memory.CreatedAt, &memory.Memory)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}

	return memories, nil
}

func (m *MemoryDatabase) DeleteMemory(ctx context.Context, memory database.UserMemory) error {
	_, err := m.db.ExecContext(ctx, "DELETE FROM memories WHERE id = ?", memory.ID)
	return err
}
