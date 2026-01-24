package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_CreateDriver(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	factory := &Factory{}

	t.Run("creates driver with valid path", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		cfg := latest.MemoryConfig{
			Kind: "sqlite",
			Path: dbPath,
		}

		driver, err := factory.CreateDriver(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, driver)
		defer driver.Close()
	})

	t.Run("fails without path", func(t *testing.T) {
		t.Parallel()
		cfg := latest.MemoryConfig{
			Kind: "sqlite",
			Path: "",
		}

		driver, err := factory.CreateDriver(ctx, cfg)
		require.Error(t, err)
		assert.Nil(t, driver)
		assert.Contains(t, err.Error(), "requires a path")
	})
}

func TestDriver_StoreAndRetrieve(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	driver := createTestDriver(t)
	defer driver.Close()

	t.Run("store with explicit key", func(t *testing.T) {
		err := driver.Store(ctx, "test-key-1", "test value 1")
		require.NoError(t, err)

		entries, err := driver.Retrieve(ctx, memory.Query{ID: "test-key-1"})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "test-key-1", entries[0].ID)
		assert.Equal(t, "test value 1", entries[0].Content)
	})

	t.Run("store with auto-generated key", func(t *testing.T) {
		err := driver.Store(ctx, "", "auto key value")
		require.NoError(t, err)

		entries, err := driver.Retrieve(ctx, memory.Query{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(entries), 1)
	})

	t.Run("retrieve all with limit", func(t *testing.T) {
		// Add more entries
		for range 5 {
			err := driver.Store(ctx, "", "bulk value")
			require.NoError(t, err)
		}

		entries, err := driver.Retrieve(ctx, memory.Query{Limit: 3})
		require.NoError(t, err)
		assert.Len(t, entries, 3)
	})

	t.Run("retrieve with semantic query falls back to all", func(t *testing.T) {
		entries, err := driver.Retrieve(ctx, memory.Query{
			Semantic: "some semantic query",
			Limit:    2,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(entries), 2)
	})
}

func TestDriver_Delete(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	driver := createTestDriver(t)
	defer driver.Close()

	// Store a memory
	err := driver.Store(ctx, "delete-test", "to be deleted")
	require.NoError(t, err)

	// Verify it exists
	entries, err := driver.Retrieve(ctx, memory.Query{ID: "delete-test"})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Delete it
	err = driver.Delete(ctx, "delete-test")
	require.NoError(t, err)

	// Verify it's gone
	entries, err = driver.Retrieve(ctx, memory.Query{ID: "delete-test"})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestDriver_UpdateExisting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	driver := createTestDriver(t)
	defer driver.Close()

	// Store initial value
	err := driver.Store(ctx, "update-key", "initial value")
	require.NoError(t, err)

	// Update with same key
	err = driver.Store(ctx, "update-key", "updated value")
	require.NoError(t, err)

	// Retrieve and verify updated
	entries, err := driver.Retrieve(ctx, memory.Query{ID: "update-key"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "updated value", entries[0].Content)
}

func createTestDriver(t *testing.T) *Driver {
	t.Helper()
	ctx := t.Context()
	factory := &Factory{}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := latest.MemoryConfig{
		Kind: "sqlite",
		Path: dbPath,
	}
	driver, err := factory.CreateDriver(ctx, cfg)
	require.NoError(t, err)
	return driver.(*Driver)
}
