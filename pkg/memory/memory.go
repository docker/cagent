package memory

import (
	"context"

	"github.com/docker/cagent/pkg/memory/database"
	"github.com/docker/cagent/pkg/memorymanager"
)

type Manager struct {
	db database.Database
}

var _ memorymanager.Manager = (*Manager)(nil)

func NewManager(db database.Database) *Manager {
	return &Manager{
		db: db,
	}
}

func (m *Manager) AddMemory(ctx context.Context, memory database.UserMemory) error {
	return m.db.AddMemory(ctx, memory)
}

func (m *Manager) GetMemories(ctx context.Context) ([]database.UserMemory, error) {
	return m.db.GetMemories(ctx)
}

func (m *Manager) DeleteMemory(ctx context.Context, memory database.UserMemory) error {
	return m.db.DeleteMemory(ctx, memory)
}
