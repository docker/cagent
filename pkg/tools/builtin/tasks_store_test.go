package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestFileTaskStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStoreWithDir("test-project", tmpDir)

	// Initially empty
	tasks, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, tasks)

	// Save some tasks
	tasksToSave := []Task{
		{ID: "task_1", Description: "First task", Status: "pending"},
		{ID: "task_2", Description: "Second task", Status: "in-progress", BlockedBy: []string{"task_1"}},
	}
	err = store.Save(tasksToSave)
	require.NoError(t, err)

	// Load them back
	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "task_1", loaded[0].ID)
	assert.Equal(t, "task_2", loaded[1].ID)
	assert.Equal(t, []string{"task_1"}, loaded[1].BlockedBy)
}

func TestFileTaskStore_FileCreatedOnSave(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStoreWithDir("my-project", tmpDir)
	expectedPath := filepath.Join(tmpDir, "my-project.json")

	// File should not exist yet
	_, err := os.Stat(expectedPath)
	assert.True(t, os.IsNotExist(err))

	// Save creates the file
	err = store.Save([]Task{{ID: "task_1", Description: "Test", Status: "pending"}})
	require.NoError(t, err)

	// File should now exist
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err)
}

func TestFileTaskStore_LoadNonExistentFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStoreWithDir("nonexistent", tmpDir)

	// Should return empty list, not error
	tasks, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestFileTaskStore_SanitizesListID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Try to use path traversal - should be sanitized
	store := NewFileTaskStoreWithDir("../../../etc/passwd", tmpDir)

	err := store.Save([]Task{{ID: "task_1", Description: "Test", Status: "pending"}})
	require.NoError(t, err)

	// File should be created in tmpDir with sanitized name, not elsewhere
	expectedPath := filepath.Join(tmpDir, "passwd.json")
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err)
}

func TestMemoryTaskStore_NoOp(t *testing.T) {
	t.Parallel()

	store := NewMemoryTaskStore()

	// Load always returns empty
	tasks, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, tasks)

	// Save is a no-op
	err = store.Save([]Task{{ID: "task_1", Description: "Test", Status: "pending"}})
	require.NoError(t, err)

	// Still empty after save
	tasks, err = store.Load()
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestTasksToolWithStore_Persistence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create first tool instance and add a task
	store1 := NewFileTaskStoreWithDir("persistent-test", tmpDir)
	tool1 := NewTasksToolWithStore(store1)

	_, err := tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Persistent task",
	})
	require.NoError(t, err)

	// Create second tool instance with same store ID
	store2 := NewFileTaskStoreWithDir("persistent-test", tmpDir)
	tool2 := NewTasksToolWithStore(store2)

	// Should load the task from the first instance
	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Persistent task")
}

func TestTasksToolWithStore_LazyLoading(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Pre-populate a task file
	store := NewFileTaskStoreWithDir("lazy-test", tmpDir)
	err := store.Save([]Task{
		{ID: "task_1", Description: "Pre-existing task", Status: "pending"},
	})
	require.NoError(t, err)

	// Create tool - should not load yet
	tool := NewTasksToolWithStore(NewFileTaskStoreWithDir("lazy-test", tmpDir))
	assert.False(t, tool.handler.loaded)

	// First operation triggers load
	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.True(t, tool.handler.loaded)
	assert.Contains(t, result.Output, "Pre-existing task")
}

func TestTasksToolWithStore_PersistsDependencies(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create tasks with dependencies
	store1 := NewFileTaskStoreWithDir("deps-test", tmpDir)
	tool1 := NewTasksToolWithStore(store1)

	// Create first task
	_, err := tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Setup database",
	})
	require.NoError(t, err)

	// Create second task that depends on first
	_, err = tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Run migrations",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	// Load in new instance
	store2 := NewFileTaskStoreWithDir("deps-test", tmpDir)
	tool2 := NewTasksToolWithStore(store2)

	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Setup database")
	assert.Contains(t, result.Output, "Run migrations")
	assert.Contains(t, result.Output, "blocked by")
}

func TestTasksToolWithStore_PersistsStatusChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create and complete a task
	store1 := NewFileTaskStoreWithDir("status-test", tmpDir)
	tool1 := NewTasksToolWithStore(store1)

	_, err := tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Task to complete",
	})
	require.NoError(t, err)

	_, err = tool1.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "in-progress"}},
	})
	require.NoError(t, err)

	// Load in new instance - should see in-progress status
	store2 := NewFileTaskStoreWithDir("status-test", tmpDir)
	tool2 := NewTasksToolWithStore(store2)

	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "■") // in-progress icon
	assert.Contains(t, result.Output, "1 in progress")
}

func TestTasksToolWithStore_ClearsOnAllCompleted(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store := NewFileTaskStoreWithDir("clear-test", tmpDir)
	tool := NewTasksToolWithStore(store)

	// Create and complete a task
	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Single task",
	})
	require.NoError(t, err)

	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})
	require.NoError(t, err)

	// Load in new instance - should be empty (cleared when all completed)
	store2 := NewFileTaskStoreWithDir("clear-test", tmpDir)
	tool2 := NewTasksToolWithStore(store2)

	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No tasks")
}

func TestFileTaskStore_AtomicWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStoreWithDir("atomic-test", tmpDir)

	// Save initial tasks
	err := store.Save([]Task{
		{ID: "task_1", Description: "Initial", Status: "pending"},
	})
	require.NoError(t, err)

	// Verify no .tmp file left behind
	tmpFile := filepath.Join(tmpDir, "atomic-test.json.tmp")
	_, err = os.Stat(tmpFile)
	assert.True(t, os.IsNotExist(err), "temp file should not exist after save")

	// Verify main file exists
	mainFile := filepath.Join(tmpDir, "atomic-test.json")
	_, err = os.Stat(mainFile)
	assert.NoError(t, err, "main file should exist")
}

func TestDefaultTaskListID(t *testing.T) {
	// This test runs in the cagent repo, so it should detect the git repo
	listID := DefaultTaskListID()

	// Should be non-empty
	assert.NotEmpty(t, listID)

	// Should contain "cagent" (the repo name) and a hash
	assert.Contains(t, listID, "cagent")
	assert.Contains(t, listID, "-") // separator between name and hash

	// Should be deterministic (same result on multiple calls)
	listID2 := DefaultTaskListID()
	assert.Equal(t, listID, listID2)
}
