package builtin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

// =============================================================================
// Unit Tests: Task Creation with Dependencies
// =============================================================================

func TestTasksTool_CreateTask_Basic(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	result, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Setup database",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created task [task_1]: Setup database")

	tasks := tool.handler.tasks.All()
	require.Len(t, tasks, 1)
	assert.Equal(t, "task_1", tasks[0].ID)
	assert.Equal(t, "pending", tasks[0].Status)
}

func TestTasksTool_CreateTask_WithBlockedBy(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	// Create prerequisite tasks first
	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Task 1"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Task 2"})
	require.NoError(t, err)

	// Create a task that depends on both
	result, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Task 3",
		BlockedBy:   []string{"task_1", "task_2"},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created task [task_3]: Task 3")
	assert.Contains(t, result.Output, "blocked by task_1, task_2")

	tasks := tool.handler.tasks.All()
	require.Len(t, tasks, 3)
	assert.Equal(t, []string{"task_1", "task_2"}, tasks[2].BlockedBy)
}

func TestTasksTool_CreateTask_WithInvalidBlockedBy(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	result, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Some task",
		BlockedBy:   []string{"task_999"},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "invalid blocked_by reference: task_999 not found")
}

func TestTasksTool_CreateTask_WithOwner(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	result, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Backend task",
		Owner:       "backend-dev",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created task [task_1]: Backend task")

	tasks := tool.handler.tasks.All()
	require.Len(t, tasks, 1)
	assert.Equal(t, "backend-dev", tasks[0].Owner)
}

func TestTasksTool_CreateTasks_Batch(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	result, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Research"},
			{Description: "Design", BlockedBy: []string{"task_1"}},
			{Description: "Implement", BlockedBy: []string{"task_2"}},
		},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Created 3 tasks")

	tasks := tool.handler.tasks.All()
	require.Len(t, tasks, 3)
	assert.Empty(t, tasks[0].BlockedBy)
	assert.Equal(t, []string{"task_1"}, tasks[1].BlockedBy)
	assert.Equal(t, []string{"task_2"}, tasks[2].BlockedBy)
}

func TestTasksTool_CreateTasks_CircularDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	// Try to create tasks with circular dependency
	result, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Task A", BlockedBy: []string{"task_2"}},
			{Description: "Task B", BlockedBy: []string{"task_1"}},
		},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	// First task depends on second task which comes later in batch - invalid order
	assert.Contains(t, result.Output, "task_2 must be created before task_1")

	// No tasks should have been created
	tasks := tool.handler.tasks.All()
	assert.Empty(t, tasks)
}

func TestTasksTool_CreateTasks_MutualDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	// Create a task first
	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Existing task"})
	require.NoError(t, err)

	// Try to create tasks where second depends on first, and first depends on second
	// This is a real circular dependency since task_1 exists
	result, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Task A", BlockedBy: []string{"task_1"}}, // task_2 blocked by existing task_1
			{Description: "Task B", BlockedBy: []string{"task_2"}}, // task_3 blocked by task_2 (in batch)
		},
	})

	require.NoError(t, err)
	assert.False(t, result.IsError) // This should work - it's a valid chain
	assert.Contains(t, result.Output, "Created 2 tasks")
}

func TestTasksTool_CreateTasks_SelfDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	// Try to create a task that depends on itself
	result, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Self-referential", BlockedBy: []string{"task_1"}},
		},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "cannot depend on itself")
}

// =============================================================================
// Unit Tests: canStart Logic
// =============================================================================

func TestTasksTool_CanStart_NoDependencies(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Independent task"})
	require.NoError(t, err)

	canStart, blockers := tool.handler.canStart("task_1")
	assert.True(t, canStart)
	assert.Empty(t, blockers)
}

func TestTasksTool_CanStart_WithPendingBlockers(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Blocker"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Dependent",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	canStart, blockers := tool.handler.canStart("task_2")
	assert.False(t, canStart)
	assert.Equal(t, []string{"task_1"}, blockers)
}

func TestTasksTool_CanStart_WithCompletedBlockers(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Blocker"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Dependent",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	// Complete the blocker
	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})
	require.NoError(t, err)

	canStart, blockers := tool.handler.canStart("task_2")
	assert.True(t, canStart)
	assert.Empty(t, blockers)
}

func TestTasksTool_CanStart_MultipleBlockers_PartiallyCompleted(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Blocker 1"},
			{Description: "Blocker 2"},
			{Description: "Dependent", BlockedBy: []string{"task_1", "task_2"}},
		},
	})
	require.NoError(t, err)

	// Complete only one blocker
	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})
	require.NoError(t, err)

	canStart, blockers := tool.handler.canStart("task_3")
	assert.False(t, canStart)
	assert.Equal(t, []string{"task_2"}, blockers)
}

// =============================================================================
// Unit Tests: Update with Dependency Enforcement
// =============================================================================

func TestTasksTool_UpdateTasks_CannotStartBlocked(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Blocker"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Blocked",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	result, err := tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_2", Status: "in-progress"}},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "cannot start task_2: blocked by task_1")
}

func TestTasksTool_UpdateTasks_CanStartAfterBlockerCompleted(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Blocker"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Blocked",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	// Complete blocker first
	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})
	require.NoError(t, err)

	// Now can start the dependent
	result, err := tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_2", Status: "in-progress"}},
	})

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "task_2 -> in-progress")
}

func TestTasksTool_UpdateTasks_CompletionUnblocks(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "First"},
			{Description: "Second", BlockedBy: []string{"task_1"}},
			{Description: "Third", BlockedBy: []string{"task_2"}},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "task_1 -> completed")
	assert.Contains(t, result.Output, "task_2 is now unblocked")

	// task_3 should still be blocked
	canStart, blockers := tool.handler.canStart("task_3")
	assert.False(t, canStart)
	assert.Equal(t, []string{"task_2"}, blockers)
}

// =============================================================================
// Unit Tests: List Tasks
// =============================================================================

func TestTasksTool_ListTasks_Empty(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "No tasks")
}

func TestTasksTool_ListTasks_WithDependencies(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Research"},
			{Description: "Design", BlockedBy: []string{"task_1"}},
			{Description: "Implement", BlockedBy: []string{"task_2"}},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "□ [task_1] Research")
	assert.Contains(t, result.Output, "⚠ [task_2] Design → blocked by: task_1")
	assert.Contains(t, result.Output, "⚠ [task_3] Implement → blocked by: task_2")
}

func TestTasksTool_ListTasks_StatusIcons(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Done"},
			{Description: "Active"},
			{Description: "Pending"},
		},
	})
	require.NoError(t, err)

	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{
			{ID: "task_1", Status: "completed"},
			{ID: "task_2", Status: "in-progress"},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "✓ [task_1] Done")
	assert.Contains(t, result.Output, "■ [task_2] Active")
	assert.Contains(t, result.Output, "□ [task_3] Pending")
}

func TestTasksTool_ListTasks_ShowsOwner(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Backend work",
		Owner:       "backend-dev",
	})
	require.NoError(t, err)

	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "(backend-dev)")
}

func TestTasksTool_ListTasks_Stats(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Task 1"},
			{Description: "Task 2"},
			{Description: "Task 3"},
			{Description: "Task 4", BlockedBy: []string{"task_1"}},
		},
	})
	require.NoError(t, err)

	_, err = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{
			{ID: "task_1", Status: "completed"},
			{ID: "task_2", Status: "in-progress"},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.listTasks(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "1 done")
	assert.Contains(t, result.Output, "1 in progress")
	assert.Contains(t, result.Output, "2 pending")
}

// =============================================================================
// Unit Tests: Add/Remove Dependencies
// =============================================================================

func TestTasksTool_AddDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "First"},
			{Description: "Second"},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.addDependency(t.Context(), AddTaskDependencyArgs{
		TaskID:    "task_2",
		BlockedBy: []string{"task_1"},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Added dependency: task_2 is now blocked by task_1")

	tasks := tool.handler.tasks.All()
	assert.Equal(t, []string{"task_1"}, tasks[1].BlockedBy)
	assert.Contains(t, tasks[0].Blocks, "task_2")
}

func TestTasksTool_AddDependency_PreventCircular(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "First"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Second",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	// Try circular: task_1 blocked by task_2
	result, err := tool.handler.addDependency(t.Context(), AddTaskDependencyArgs{
		TaskID:    "task_1",
		BlockedBy: []string{"task_2"},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "circular dependency detected")
}

func TestTasksTool_AddDependency_PreventSelfDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "Task"})
	require.NoError(t, err)

	result, err := tool.handler.addDependency(t.Context(), AddTaskDependencyArgs{
		TaskID:    "task_1",
		BlockedBy: []string{"task_1"},
	})

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "cannot depend on itself")
}

func TestTasksTool_RemoveDependency(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{Description: "First"})
	require.NoError(t, err)
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Second",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	result, err := tool.handler.removeDependency(t.Context(), RemoveTaskDependencyArgs{
		TaskID:    "task_2",
		BlockedBy: []string{"task_1"},
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "Removed dependency")

	tasks := tool.handler.tasks.All()
	assert.Empty(t, tasks[1].BlockedBy)
	assert.NotContains(t, tasks[0].Blocks, "task_2")
}

// =============================================================================
// Unit Tests: Get Blocked Tasks
// =============================================================================

func TestTasksTool_GetBlockedTasks(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Root"},
			{Description: "Child 1", BlockedBy: []string{"task_1"}},
			{Description: "Child 2", BlockedBy: []string{"task_1"}},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.getBlockedTasks(t.Context(), GetBlockedTasksArgs{})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "task_2")
	assert.Contains(t, result.Output, "task_3")
	assert.Contains(t, result.Output, "blocked by: task_1")
}

func TestTasksTool_GetBlockedTasks_FilterByBlocker(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	_, err := tool.handler.createTasks(t.Context(), CreateTasksArgs{
		Tasks: []CreateTaskItem{
			{Description: "Blocker A"},
			{Description: "Blocker B"},
			{Description: "Blocked by A", BlockedBy: []string{"task_1"}},
			{Description: "Blocked by B", BlockedBy: []string{"task_2"}},
		},
	})
	require.NoError(t, err)

	result, err := tool.handler.getBlockedTasks(t.Context(), GetBlockedTasksArgs{
		BlockedBy: "task_1",
	})

	require.NoError(t, err)
	assert.Contains(t, result.Output, "task_3")
	assert.NotContains(t, result.Output, "task_4")
}

// =============================================================================
// Unit Tests: Shared Instance
// =============================================================================

func TestTasksTool_SharedInstance(t *testing.T) {
	t.Parallel()

	shared1 := NewSharedTasksTool()
	shared2 := NewSharedTasksTool()
	assert.Same(t, shared1, shared2, "NewSharedTasksTool should return same instance")

	nonShared1 := NewTasksTool()
	nonShared2 := NewTasksTool()
	assert.NotSame(t, nonShared1, nonShared2, "NewTasksTool should return different instances")
}

func TestTasksTool_CrossAgentSharing(t *testing.T) {
	// Simulates two agents sharing a task list
	tool := NewTasksTool()

	// Agent A creates a task
	_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Task from Agent A",
		Owner:       "agent-a",
	})
	require.NoError(t, err)

	// Agent B creates a dependent task
	_, err = tool.handler.createTask(t.Context(), CreateTaskArgs{
		Description: "Task from Agent B",
		Owner:       "agent-b",
		BlockedBy:   []string{"task_1"},
	})
	require.NoError(t, err)

	// Both tasks visible
	tasks := tool.handler.tasks.All()
	require.Len(t, tasks, 2)
	assert.Equal(t, "agent-a", tasks[0].Owner)
	assert.Equal(t, "agent-b", tasks[1].Owner)

	// Agent A completes their task
	result, err := tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
		Updates: []TaskUpdate{{ID: "task_1", Status: "completed"}},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "task_2 is now unblocked")

	// Agent B can now start
	canStart, _ := tool.handler.canStart("task_2")
	assert.True(t, canStart)
}

// =============================================================================
// Unit Tests: Schema
// =============================================================================

func TestTasksTool_Schema(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 7)

	// Verify all tools have correct category
	for _, tt := range allTools {
		assert.Equal(t, "tasks", tt.Category)
	}
}

// =============================================================================
// Unit Tests: Concurrency
// =============================================================================

func TestTasksTool_ConcurrentCreates(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := range numGoroutines {
		go func(idx int) {
			_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
				Description: fmt.Sprintf("Task from goroutine %d", idx),
			})
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range numGoroutines {
		<-done
	}

	// Verify all tasks were created with unique IDs
	tasks := tool.handler.tasks.All()
	assert.Len(t, tasks, numGoroutines)

	ids := make(map[string]bool)
	for _, task := range tasks {
		assert.False(t, ids[task.ID], "duplicate task ID: %s", task.ID)
		ids[task.ID] = true
	}
}

func TestTasksTool_ConcurrentUpdates(t *testing.T) {
	t.Parallel()
	tool := NewTasksTool()

	// Create initial tasks
	for i := range 5 {
		_, err := tool.handler.createTask(t.Context(), CreateTaskArgs{
			Description: fmt.Sprintf("Task %d", i+1),
		})
		require.NoError(t, err)
	}

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	// Concurrent updates and reads
	for i := range numGoroutines {
		go func(idx int) {
			if idx%2 == 0 {
				// Update a task
				taskID := fmt.Sprintf("task_%d", (idx%5)+1)
				_, _ = tool.handler.updateTasks(t.Context(), UpdateTasksArgs{
					Updates: []TaskUpdate{{ID: taskID, Status: "in-progress"}},
				})
			} else {
				// List tasks
				_, _ = tool.handler.listTasks(t.Context(), tools.ToolCall{})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range numGoroutines {
		<-done
	}

	// Verify tasks are still consistent
	tasks := tool.handler.tasks.All()
	assert.Len(t, tasks, 5)
}
