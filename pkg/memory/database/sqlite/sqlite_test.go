package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/memory/database"
)

func setupTestDB(t *testing.T) database.Database {
	t.Helper()

	// Create temporary database file
	tmpFile := t.TempDir() + "/test.db"

	db, err := NewMemoryDatabase(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, db)

	t.Cleanup(func() {
		// Close connection and remove temp file
		memDB := db.(*MemoryDatabase)
		memDB.db.Close()
		os.Remove(tmpFile)
	})

	return db
}

func TestNewMemoryDatabase(t *testing.T) {
	// Test successful database creation
	db := setupTestDB(t)

	assert.NotNil(t, db, "Database should be created successfully")

	// Test with invalid path
	_, err := NewMemoryDatabase("/:invalid:path")
	require.Error(t, err, "Should fail with invalid database path")
}

func TestAddMemory(t *testing.T) {
	db := setupTestDB(t)

	ctx := t.Context()

	// Test adding a valid memory
	memory := database.UserMemory{
		ID:        "test-id-1",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "Test memory content",
	}

	err := db.AddMemory(ctx, memory)
	require.NoError(t, err, "Adding memory should succeed")

	// Test adding a duplicate memory (same ID)
	err = db.AddMemory(ctx, memory)
	require.Error(t, err, "Adding memory with duplicate ID should fail")

	// Test adding with empty ID
	emptyIDMemory := database.UserMemory{
		ID:        "",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "Empty ID memory",
	}

	err = db.AddMemory(ctx, emptyIDMemory)
	require.Error(t, err, "Adding memory with empty ID should fail")
}

func TestGetMemories(t *testing.T) {
	db := setupTestDB(t)

	// Test with empty database
	memories, err := db.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Empty(t, memories, "Empty database should return empty memories slice")

	// Add test memories
	testMemories := []database.UserMemory{
		{
			ID:        "test-id-1",
			CreatedAt: time.Now().Format(time.RFC3339),
			Memory:    "First test memory",
		},
		{
			ID:        "test-id-2",
			CreatedAt: time.Now().Format(time.RFC3339),
			Memory:    "Second test memory",
		},
	}

	for _, memory := range testMemories {
		err := db.AddMemory(t.Context(), memory)
		require.NoError(t, err)
	}

	// Get and verify memories
	memories, err = db.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Len(t, memories, 2, "Should retrieve both added memories")

	// Verify contents (order might not be guaranteed)
	memoryMap := make(map[string]database.UserMemory)
	for _, memory := range memories {
		memoryMap[memory.ID] = memory
	}

	for _, expected := range testMemories {
		actual, exists := memoryMap[expected.ID]
		assert.True(t, exists, "Memory with ID %s should exist", expected.ID)
		assert.Equal(t, expected.Memory, actual.Memory)
		assert.Equal(t, expected.CreatedAt, actual.CreatedAt)
	}
}

func TestDeleteMemory(t *testing.T) {
	db := setupTestDB(t)

	// Add a test memory
	memory := database.UserMemory{
		ID:        "test-id-1",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "Test memory to delete",
	}

	err := db.AddMemory(t.Context(), memory)
	require.NoError(t, err)

	// Verify it exists
	memories, err := db.GetMemories(t.Context())
	require.NoError(t, err)
	require.Len(t, memories, 1)

	// Delete the memory
	err = db.DeleteMemory(t.Context(), memory)
	require.NoError(t, err, "Deleting existing memory should succeed")

	// Verify it's gone
	memories, err = db.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Empty(t, memories, "Memory should be deleted")

	// Try deleting non-existent memory
	nonExistentMemory := database.UserMemory{
		ID: "non-existent-id",
	}
	err = db.DeleteMemory(t.Context(), nonExistentMemory)
	require.NoError(t, err, "Deleting non-existent memory should not return an error")
}

func TestDatabaseOperationsWithCanceledContext(t *testing.T) {
	db := setupTestDB(t)

	// Create a canceled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Test operations with canceled context
	memory := database.UserMemory{
		ID:        "test-id",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "Test memory",
	}

	err := db.AddMemory(ctx, memory)
	require.Error(t, err, "AddMemory should fail with canceled context")

	_, err = db.GetMemories(ctx)
	require.Error(t, err, "GetMemories should fail with canceled context")

	err = db.DeleteMemory(ctx, memory)
	require.Error(t, err, "DeleteMemory should fail with canceled context")
}

func TestDatabaseWithMultipleInstances(t *testing.T) {
	// Create first database instance
	tmpFile := t.TempDir() + "/shared.db"
	db1, err := NewMemoryDatabase(tmpFile)
	require.NoError(t, err)
	defer func() {
		memDB := db1.(*MemoryDatabase)
		memDB.db.Close()
		os.Remove(tmpFile)
	}()

	// Add a memory to the first instance
	memory := database.UserMemory{
		ID:        "shared-id",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "Shared memory",
	}

	err = db1.AddMemory(t.Context(), memory)
	require.NoError(t, err)

	// Create second database instance pointing to the same file
	db2, err := NewMemoryDatabase(tmpFile)
	require.NoError(t, err)
	defer func() {
		memDB := db2.(*MemoryDatabase)
		memDB.db.Close()
	}()

	// Verify second instance can read the memory added by first instance
	memories, err := db2.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Len(t, memories, 1, "Second instance should see memory added by first instance")
	assert.Equal(t, "shared-id", memories[0].ID)
	assert.Equal(t, "Shared memory", memories[0].Memory)
}
