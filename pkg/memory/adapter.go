package memory

import (
	"context"

	"github.com/docker/cagent/pkg/memory/database"
	"github.com/google/uuid"
)

// DatabaseAdapter adapts the new Driver interface to the legacy database.Database interface
type DatabaseAdapter struct {
	driver Driver
}

var _ database.Database = (*DatabaseAdapter)(nil)

// NewDatabaseAdapter creates an adapter that wraps a Driver
func NewDatabaseAdapter(driver Driver) *DatabaseAdapter {
	return &DatabaseAdapter{driver: driver}
}

func (a *DatabaseAdapter) AddMemory(ctx context.Context, memory database.UserMemory) error {
	key := memory.ID
	if key == "" {
		key = uuid.New().String()
	}
	return a.driver.Store(ctx, key, memory.Memory)
}

func (a *DatabaseAdapter) GetMemories(ctx context.Context) ([]database.UserMemory, error) {
	entries, err := a.driver.Retrieve(ctx, Query{})
	if err != nil {
		return nil, err
	}

	memories := make([]database.UserMemory, len(entries))
	for i, e := range entries {
		memories[i] = database.UserMemory{
			ID:        e.ID,
			CreatedAt: e.CreatedAt,
			Memory:    e.Content,
		}
	}
	return memories, nil
}

func (a *DatabaseAdapter) DeleteMemory(ctx context.Context, memory database.UserMemory) error {
	return a.driver.Delete(ctx, memory.ID)
}
