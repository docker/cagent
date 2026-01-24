package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/memory"
	"github.com/docker/cagent/pkg/memory/database"
	"github.com/docker/cagent/pkg/memory/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockDriver implements memory.Driver for testing
type MockDriver struct {
	stored   map[string]string
	entries  []memory.Entry
	closeErr error
}

func NewMockDriver() *MockDriver {
	return &MockDriver{
		stored:  make(map[string]string),
		entries: []memory.Entry{},
	}
}

func (m *MockDriver) Store(ctx context.Context, key string, value string) error {
	m.stored[key] = value
	m.entries = append(m.entries, memory.Entry{
		ID:        key,
		Content:   value,
		CreatedAt: "2026-01-17T00:00:00Z",
	})
	return nil
}

func (m *MockDriver) Retrieve(ctx context.Context, query memory.Query) ([]memory.Entry, error) {
	if query.ID != "" {
		for _, e := range m.entries {
			if e.ID == query.ID {
				return []memory.Entry{e}, nil
			}
		}
		return []memory.Entry{}, nil
	}
	return m.entries, nil
}

func (m *MockDriver) Delete(ctx context.Context, key string) error {
	delete(m.stored, key)
	var newEntries []memory.Entry
	for _, e := range m.entries {
		if e.ID != key {
			newEntries = append(newEntries, e)
		}
	}
	m.entries = newEntries
	return nil
}

func (m *MockDriver) Close() error {
	return m.closeErr
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	t.Run("register and create driver", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		registry := memory.NewRegistry()

		// Register a mock factory
		mockFactory := &mockFactory{}
		registry.Register("mock", mockFactory)

		// Create driver
		cfg := latest.MemoryConfig{Kind: "mock"}
		driver, err := registry.CreateDriver(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, driver)
	})

	t.Run("error on unknown kind", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		registry := memory.NewRegistry()

		cfg := latest.MemoryConfig{Kind: "unknown"}
		driver, err := registry.CreateDriver(ctx, cfg)
		require.Error(t, err)
		assert.Nil(t, driver)

		var unsupportedErr *memory.UnsupportedKindError
		assert.ErrorAs(t, err, &unsupportedErr)
		assert.Equal(t, "unknown", unsupportedErr.Kind)
	})
}

type mockFactory struct{}

func (f *mockFactory) CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (memory.Driver, error) {
	return NewMockDriver(), nil
}

func TestDatabaseAdapter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockDriver := NewMockDriver()
	adapter := memory.NewDatabaseAdapter(mockDriver)

	t.Run("add memory", func(t *testing.T) {
		mem := database.UserMemory{
			ID:     "test-1",
			Memory: "test content",
		}
		err := adapter.AddMemory(ctx, mem)
		require.NoError(t, err)
		assert.Equal(t, "test content", mockDriver.stored["test-1"])
	})

	t.Run("add memory with auto ID", func(t *testing.T) {
		mem := database.UserMemory{
			Memory: "auto id content",
		}
		err := adapter.AddMemory(ctx, mem)
		require.NoError(t, err)
		// Should have stored with a UUID
		assert.Len(t, mockDriver.stored, 2)
	})

	t.Run("get memories", func(t *testing.T) {
		memories, err := adapter.GetMemories(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(memories), 2)
	})

	t.Run("delete memory", func(t *testing.T) {
		mem := database.UserMemory{ID: "test-1"}
		err := adapter.DeleteMemory(ctx, mem)
		require.NoError(t, err)
		_, exists := mockDriver.stored["test-1"]
		assert.False(t, exists)
	})
}

func TestDefaultRegistry(t *testing.T) {
	t.Run("default registry is singleton", func(t *testing.T) {
		reg1 := memory.DefaultRegistry()
		reg2 := memory.DefaultRegistry()
		assert.Same(t, reg1, reg2)
	})
}

// Integration test with real SQLite driver
func TestSQLiteDriverIntegration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "integration_test.db")
	cfg := latest.MemoryConfig{
		Kind: "sqlite",
		Path: dbPath,
	}

	// Use the actual sqlite factory
	sqliteFactory := &sqlite.Factory{}
	registry := memory.NewRegistry()
	registry.Register("sqlite", sqliteFactory)

	driver, err := registry.CreateDriver(ctx, cfg)
	require.NoError(t, err)
	defer driver.Close()

	// Test full workflow through adapter
	adapter := memory.NewDatabaseAdapter(driver)

	// Add
	err = adapter.AddMemory(ctx, database.UserMemory{
		ID:     "integration-1",
		Memory: "Integration test memory",
	})
	require.NoError(t, err)

	// Get
	memories, err := adapter.GetMemories(ctx)
	require.NoError(t, err)
	require.Len(t, memories, 1)
	assert.Equal(t, "Integration test memory", memories[0].Memory)

	// Delete
	err = adapter.DeleteMemory(ctx, database.UserMemory{ID: "integration-1"})
	require.NoError(t, err)

	memories, err = adapter.GetMemories(ctx)
	require.NoError(t, err)
	assert.Empty(t, memories)
}

// sqliteTestFactory wraps sqlite.Factory for testing (kept for reference)
type sqliteTestFactory struct{}

func (f *sqliteTestFactory) CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (memory.Driver, error) {
	factory := &sqlite.Factory{}
	return factory.CreateDriver(ctx, cfg)
}
