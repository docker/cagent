package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

func TestFileTaskStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStore("test-project", WithBaseDir(tmpDir))

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
	store := NewFileTaskStore("my-project", WithBaseDir(tmpDir))
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
	store := NewFileTaskStore("nonexistent", WithBaseDir(tmpDir))

	// Should return empty list, not error
	tasks, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestFileTaskStore_SanitizesListID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Try to use path traversal - should be sanitized
	store := NewFileTaskStore("../../../etc/passwd", WithBaseDir(tmpDir))

	err := store.Save([]Task{{ID: "task_1", Description: "Test", Status: "pending"}})
	require.NoError(t, err)

	// File should be created in tmpDir with sanitized name, not elsewhere
	expectedPath := filepath.Join(tmpDir, "passwd.json")
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err)
}

func TestFileTaskStore_EmptyListID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name   string
		listID string
	}{
		{"empty string", ""},
		{"dot", "."},
		{"double dot", ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewFileTaskStore(tt.listID, WithBaseDir(tmpDir))

			err := store.Save([]Task{{ID: "task_1", Description: "Test", Status: "pending"}})
			require.NoError(t, err)

			// Should use "default" as filename
			expectedPath := filepath.Join(tmpDir, "default.json")
			_, err = os.Stat(expectedPath)
			require.NoError(t, err)

			// Cleanup for next test
			os.Remove(expectedPath)
		})
	}
}

func TestTasksToolWithStore_Persistence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create first tool instance and add a task
	store1 := NewFileTaskStore("persistent-test", WithBaseDir(tmpDir))
	tool1 := NewTasksTool(store1)

	_, err := tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Persistent task",
	})
	require.NoError(t, err)

	// Create second tool instance with same store ID
	store2 := NewFileTaskStore("persistent-test", WithBaseDir(tmpDir))
	tool2 := NewTasksTool(store2)

	// Should load the task from the first instance
	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Persistent task")
}

func TestTasksToolWithStore_LazyLoading(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Pre-populate a task file
	store := NewFileTaskStore("lazy-test", WithBaseDir(tmpDir))
	err := store.Save([]Task{
		{ID: "task_1", Description: "Pre-existing task", Status: "pending"},
	})
	require.NoError(t, err)

	// Create tool - tasks slice should be empty before first operation
	tool := NewTasksTool(NewFileTaskStore("lazy-test", WithBaseDir(tmpDir)))
	assert.Equal(t, 0, tool.handler.tasks.Length())

	// First operation triggers load
	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Equal(t, 1, tool.handler.tasks.Length())
	assert.Contains(t, result.Output, "Pre-existing task")
}

func TestTasksToolWithStore_PersistsDependencies(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create tasks with dependencies
	store1 := NewFileTaskStore("deps-test", WithBaseDir(tmpDir))
	tool1 := NewTasksTool(store1)

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
	store2 := NewFileTaskStore("deps-test", WithBaseDir(tmpDir))
	tool2 := NewTasksTool(store2)

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
	store1 := NewFileTaskStore("status-test", WithBaseDir(tmpDir))
	tool1 := NewTasksTool(store1)

	_, err := tool1.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Task to complete",
	})
	require.NoError(t, err)

	_, err = tool1.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "in-progress"}},
	})
	require.NoError(t, err)

	// Load in new instance - should see in-progress status
	store2 := NewFileTaskStore("status-test", WithBaseDir(tmpDir))
	tool2 := NewTasksTool(store2)

	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "■") // in-progress icon
	assert.Contains(t, result.Output, "1 in progress")
}

func TestTasksToolWithStore_ClearsOnAllCompleted(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	store := NewFileTaskStore("clear-test", WithBaseDir(tmpDir))
	tool := NewTasksTool(store)

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
	store2 := NewFileTaskStore("clear-test", WithBaseDir(tmpDir))
	tool2 := NewTasksTool(store2)

	result, err := tool2.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No tasks")
}

func TestFileTaskStore_AtomicWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewFileTaskStore("atomic-test", WithBaseDir(tmpDir))

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

func TestDefaultTaskListID_Worktrees(t *testing.T) {
	// Create a temp directory for our test repos
	tmpDir := t.TempDir()

	// Create main repo
	mainRepo := filepath.Join(tmpDir, "main-repo")
	require.NoError(t, os.MkdirAll(mainRepo, 0o755))

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())

	// Create initial commit (required for worktrees)
	testFile := filepath.Join(mainRepo, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())

	// Create a worktree
	worktree := filepath.Join(tmpDir, "worktree-feature")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature", worktree)
	cmd.Dir = mainRepo
	require.NoError(t, cmd.Run())

	// Get task list ID from main repo
	t.Chdir(mainRepo)
	mainListID := DefaultTaskListID()

	// Get task list ID from worktree
	t.Chdir(worktree)
	worktreeListID := DefaultTaskListID()

	// Both should return the same ID (same underlying repo)
	assert.Equal(t, mainListID, worktreeListID, "main repo and worktree should share the same task list ID")

	// ID should contain the repo name
	assert.Contains(t, mainListID, "main-repo")
}

func TestDefaultTaskListID_DifferentRepos(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two separate repos
	repo1 := filepath.Join(tmpDir, "project-alpha")
	repo2 := filepath.Join(tmpDir, "project-beta")
	require.NoError(t, os.MkdirAll(repo1, 0o755))
	require.NoError(t, os.MkdirAll(repo2, 0o755))

	// Initialize both repos
	for _, repo := range []string{repo1, repo2} {
		cmd := exec.Command("git", "init")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())
	}

	// Get task list IDs
	t.Chdir(repo1)
	id1 := DefaultTaskListID()

	t.Chdir(repo2)
	id2 := DefaultTaskListID()

	// Should be different
	assert.NotEqual(t, id1, id2, "different repos should have different task list IDs")
	assert.Contains(t, id1, "project-alpha")
	assert.Contains(t, id2, "project-beta")
}

func TestDefaultTaskListID_NotGitRepo(t *testing.T) {
	// Create a temp directory that's not a git repo
	tmpDir := t.TempDir()
	notGitDir := filepath.Join(tmpDir, "not-a-repo")
	require.NoError(t, os.MkdirAll(notGitDir, 0o755))

	t.Chdir(notGitDir)
	listID := DefaultTaskListID()

	// Should fallback to directory name + hash
	assert.NotEmpty(t, listID)
	assert.Contains(t, listID, "not-a-repo")
	assert.Contains(t, listID, "-") // should still have hash
}

func TestTasksToolWithStore_LoadError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	taskFile := filepath.Join(tmpDir, "corrupted.json")

	// Write corrupted JSON
	err := os.WriteFile(taskFile, []byte("not valid json{"), 0o644)
	require.NoError(t, err)

	// Create store pointing to corrupted file
	store := NewFileTaskStore("corrupted", WithBaseDir(tmpDir))
	tool := NewTasksTool(store)

	// All operations should fail with load error
	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "cannot list tasks")
	assert.Contains(t, result.Output, "failed to load tasks")

	// Create should also fail - prevents overwriting corrupted file
	result, err = tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "test"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "cannot create task")
}
