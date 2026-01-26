package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameCreateTask      = "create_task"
	ToolNameCreateTasks     = "create_tasks"
	ToolNameUpdateTasks     = "update_tasks"
	ToolNameListTasks       = "list_tasks"
	ToolNameAddTaskDep      = "add_task_dependency"
	ToolNameRemoveTaskDep   = "remove_task_dependency"
	ToolNameGetBlockedTasks = "get_blocked_tasks"
)

type TasksTool struct {
	tools.BaseToolSet
	handler *tasksHandler
}

var _ tools.ToolSet = (*TasksTool)(nil)

// Task represents a task with optional dependencies
type Task struct {
	ID          string   `json:"id" jsonschema:"ID of the task"`
	Description string   `json:"description" jsonschema:"Description of the task"`
	Status      string   `json:"status" jsonschema:"Status: pending, in-progress, or completed"`
	BlockedBy   []string `json:"blocked_by,omitempty" jsonschema:"IDs of tasks that must be completed before this one can start"`
	Blocks      []string `json:"blocks,omitempty" jsonschema:"IDs of tasks that are waiting for this one to complete"`
	Owner       string   `json:"owner,omitempty" jsonschema:"Owner/assignee of this task"`
}

type CreateTaskArgs struct {
	Description string   `json:"description" jsonschema:"Description of the task,required"`
	BlockedBy   []string `json:"blocked_by,omitempty" jsonschema:"IDs of tasks that must be completed before this one can start"`
	Owner       string   `json:"owner,omitempty" jsonschema:"Owner/assignee of this task"`
}

type CreateTaskItem struct {
	Description string   `json:"description" jsonschema:"Description of the task,required"`
	BlockedBy   []string `json:"blocked_by,omitempty" jsonschema:"IDs of tasks that must be completed before this one can start"`
	Owner       string   `json:"owner,omitempty" jsonschema:"Owner/assignee of this task"`
}

type CreateTasksArgs struct {
	Tasks []CreateTaskItem `json:"tasks" jsonschema:"List of tasks to create,required"`
}

type TaskUpdate struct {
	ID     string `json:"id" jsonschema:"ID of the task,required"`
	Status string `json:"status,omitempty" jsonschema:"New status: pending, in-progress, or completed"`
	Owner  string `json:"owner,omitempty" jsonschema:"New owner/assignee"`
}

type UpdateTasksArgs struct {
	Updates []TaskUpdate `json:"updates" jsonschema:"List of task updates,required"`
}

type AddTaskDependencyArgs struct {
	TaskID    string   `json:"task_id" jsonschema:"ID of the task to add dependencies to,required"`
	BlockedBy []string `json:"blocked_by" jsonschema:"IDs of tasks that must be completed first,required"`
}

type RemoveTaskDependencyArgs struct {
	TaskID    string   `json:"task_id" jsonschema:"ID of the task to remove dependencies from,required"`
	BlockedBy []string `json:"blocked_by" jsonschema:"IDs of blocking tasks to remove,required"`
}

type GetBlockedTasksArgs struct {
	BlockedBy string `json:"blocked_by,omitempty" jsonschema:"Filter by specific blocker ID (optional)"`
}

type tasksHandler struct {
	mu       sync.RWMutex
	tasks    *concurrent.Slice[Task]
	store    TaskStore
	loadOnce sync.Once
	loadErr  error // Captured error from initial load
}

// tasksTool holds the singleton instance
var (
	tasksTool     *TasksTool
	tasksToolOnce sync.Once
	tasksToolOpts struct {
		listID string
	}
)

// SetTaskListID allows overriding the default task list ID (call before NewTasksTool)
// This is used by the --task-list CLI flag.
func SetTaskListID(id string) {
	tasksToolOpts.listID = id
}

// NewTasksTool returns the shared TasksTool instance.
// All agents share the same task list, persisted to ~/.cagent/tasks/<repo-id>.json
func NewTasksTool() *TasksTool {
	tasksToolOnce.Do(func() {
		listID := tasksToolOpts.listID
		if listID == "" {
			listID = DefaultTaskListID()
		}
		store := NewFileTaskStore(listID)
		tasksTool = newTasksToolWithStore(store)
	})
	return tasksTool
}

// newTasksToolWithStore creates a new TasksTool with the specified store (for testing)
func newTasksToolWithStore(store TaskStore) *TasksTool {
	return &TasksTool{
		handler: &tasksHandler{
			tasks: concurrent.NewSlice[Task](),
			store: store,
		},
	}
}

// ensureLoaded loads tasks from store on first access (lazy loading)
// Thread-safe via sync.Once. Returns error if load failed.
func (h *tasksHandler) ensureLoaded() error {
	h.loadOnce.Do(func() {
		tasks, err := h.store.Load()
		if err != nil {
			h.loadErr = fmt.Errorf("failed to load tasks: %w", err)
			slog.Error("Failed to load tasks from store", "error", err)
			return
		}

		for _, task := range tasks {
			h.tasks.Append(task)
		}

		if len(tasks) > 0 {
			slog.Debug("Loaded tasks from store", "count", len(tasks))
		}
	})
	return h.loadErr
}

// save persists tasks to store
// Must be called with h.mu held (write lock)
// Returns an error message to append to output if save fails, empty string on success
func (h *tasksHandler) save() string {
	if err := h.store.Save(h.tasks.All()); err != nil {
		slog.Error("Failed to save tasks to store", "error", err)
		return fmt.Sprintf(" (warning: failed to persist - %v)", err)
	}
	return ""
}

func (t *TasksTool) Instructions() string {
	return `## Using the Tasks Tools

IMPORTANT: Use these tools to track tasks with dependencies:

1. Before starting complex work:
   - Create tasks using create_task with blocked_by for dependencies
   - Break down work into smaller tasks

2. Dependencies:
   - Tasks with blocked_by cannot start until blockers are completed
   - Completing a task unblocks dependent tasks
   - Use list_tasks to see blocked status

3. While working:
   - Use list_tasks to see available tasks
   - Mark tasks as "in-progress" when starting
   - Mark as "completed" when done

4. Visual indicators in list_tasks:
   - ✓ = completed, ■ = in-progress, □ = pending, ⚠ = blocked

5. Persistence:
   - Tasks are automatically saved and persist across sessions
   - Tasks are shared across all worktrees of the same git repository
   - All agents share the same task list automatically`
}

func (h *tasksHandler) canStart(taskID string) (bool, []string) {
	task, idx := h.findTask(taskID)
	if idx == -1 {
		return false, []string{"task not found"}
	}
	if len(task.BlockedBy) == 0 {
		return true, nil
	}
	var pendingBlockers []string
	for _, blockerID := range task.BlockedBy {
		blocker, blockerIdx := h.findTask(blockerID)
		if blockerIdx != -1 && blocker.Status != "completed" {
			pendingBlockers = append(pendingBlockers, blockerID)
		}
	}
	return len(pendingBlockers) == 0, pendingBlockers
}

func (h *tasksHandler) findTask(id string) (*Task, int) {
	task, idx := h.tasks.Find(func(t Task) bool { return t.ID == id })
	if idx == -1 {
		return nil, -1
	}
	return &task, idx
}

func (h *tasksHandler) taskExists(id string) bool {
	_, idx := h.findTask(id)
	return idx != -1
}

// addBlockerLink adds taskID to the Blocks list of blockerID
func (h *tasksHandler) addBlockerLink(blockerID, taskID string) {
	_, idx := h.findTask(blockerID)
	if idx != -1 {
		h.tasks.Update(idx, func(t Task) Task {
			t.Blocks = append(t.Blocks, taskID)
			return t
		})
	}
}

// removeBlockerLink removes taskID from the Blocks list of blockerID
func (h *tasksHandler) removeBlockerLink(blockerID, taskID string) {
	_, idx := h.findTask(blockerID)
	if idx != -1 {
		h.tasks.Update(idx, func(t Task) Task {
			var newBlocks []string
			for _, b := range t.Blocks {
				if b != taskID {
					newBlocks = append(newBlocks, b)
				}
			}
			t.Blocks = newBlocks
			return t
		})
	}
}

func (h *tasksHandler) hasCircularDependency(taskID string, newBlockedBy []string) bool {
	blocked := make(map[string]bool)
	var collectBlocked func(id string)
	collectBlocked = func(id string) {
		task, idx := h.findTask(id)
		if idx == -1 {
			return
		}
		for _, blockedID := range task.Blocks {
			if !blocked[blockedID] {
				blocked[blockedID] = true
				collectBlocked(blockedID)
			}
		}
	}
	collectBlocked(taskID)
	for _, blockerID := range newBlockedBy {
		if blocked[blockerID] || blockerID == taskID {
			return true
		}
	}
	return false
}

func (h *tasksHandler) getUnblockedTasks(completedID string) []string {
	var unblocked []string
	h.tasks.Range(func(_ int, task Task) bool {
		for _, blockerID := range task.BlockedBy {
			if blockerID == completedID {
				if canStart, _ := h.canStart(task.ID); canStart && task.Status == "pending" {
					unblocked = append(unblocked, task.ID)
				}
				break
			}
		}
		return true
	})
	return unblocked
}

func (h *tasksHandler) createTask(_ context.Context, params CreateTaskArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot create task: %v", err)), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, blockerID := range params.BlockedBy {
		if !h.taskExists(blockerID) {
			return tools.ResultError(fmt.Sprintf("invalid blocked_by reference: %s not found", blockerID)), nil
		}
	}
	id := fmt.Sprintf("task_%d", h.tasks.Length()+1)
	task := Task{
		ID:          id,
		Description: params.Description,
		Status:      "pending",
		BlockedBy:   params.BlockedBy,
		Owner:       params.Owner,
	}
	h.tasks.Append(task)
	for _, blockerID := range params.BlockedBy {
		h.addBlockerLink(blockerID, id)
	}

	saveWarning := h.save()

	var output strings.Builder
	fmt.Fprintf(&output, "Created task [%s]: %s", id, params.Description)
	if len(params.BlockedBy) > 0 {
		fmt.Fprintf(&output, " (blocked by %s)", strings.Join(params.BlockedBy, ", "))
	}
	output.WriteString(saveWarning)
	return &tools.ToolCallResult{Output: output.String(), Meta: h.tasks.All()}, nil
}

func (h *tasksHandler) createTasks(_ context.Context, params CreateTasksArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot create tasks: %v", err)), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	start := h.tasks.Length()

	// Build a map of task IDs that will be created in this batch
	batchIDs := make(map[string]int) // ID -> index in batch
	for i := range params.Tasks {
		batchIDs[fmt.Sprintf("task_%d", start+i+1)] = i
	}

	// Validate all tasks before creating any
	for i, item := range params.Tasks {
		taskID := fmt.Sprintf("task_%d", start+i+1)
		for _, blockerID := range item.BlockedBy {
			// Check for self-dependency
			if blockerID == taskID {
				return tools.ResultError(fmt.Sprintf("task cannot depend on itself: %s", taskID)), nil
			}

			// Check if blocker is in this batch
			if blockerIdx, inBatch := batchIDs[blockerID]; inBatch {
				// Must be earlier in the batch (can't depend on later tasks)
				if blockerIdx >= i {
					return tools.ResultError(fmt.Sprintf("invalid blocked_by reference: %s must be created before %s", blockerID, taskID)), nil
				}
				// Check for mutual dependency (direct cycle in batch)
				for _, blockerBlockedBy := range params.Tasks[blockerIdx].BlockedBy {
					if blockerBlockedBy == taskID {
						return tools.ResultError(fmt.Sprintf("circular dependency detected: %s and %s block each other", taskID, blockerID)), nil
					}
				}
			} else if !h.taskExists(blockerID) {
				// Not in batch and doesn't exist in store
				return tools.ResultError(fmt.Sprintf("invalid blocked_by reference: %s not found", blockerID)), nil
			}
		}
	}

	// All validations passed, create the tasks
	var createdIDs []string
	for i, item := range params.Tasks {
		id := fmt.Sprintf("task_%d", start+i+1)
		task := Task{
			ID:          id,
			Description: item.Description,
			Status:      "pending",
			BlockedBy:   item.BlockedBy,
			Owner:       item.Owner,
		}
		h.tasks.Append(task)
		createdIDs = append(createdIDs, id)
		for _, blockerID := range item.BlockedBy {
			h.addBlockerLink(blockerID, id)
		}
	}

	saveWarning := h.save()

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Created %d tasks: %s%s", len(params.Tasks), strings.Join(createdIDs, ", "), saveWarning),
		Meta:   h.tasks.All(),
	}, nil
}

func (h *tasksHandler) updateTasks(_ context.Context, params UpdateTasksArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot update tasks: %v", err)), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var notFound, updated, blocked, newlyUnblocked []string
	for _, update := range params.Updates {
		task, idx := h.findTask(update.ID)
		if idx == -1 {
			notFound = append(notFound, update.ID)
			continue
		}
		if update.Status == "in-progress" && task.Status == "pending" {
			if canStart, blockers := h.canStart(update.ID); !canStart {
				blocked = append(blocked, fmt.Sprintf("cannot start %s: blocked by %s", update.ID, strings.Join(blockers, ", ")))
				continue
			}
		}
		wasCompleting := update.Status == "completed" && task.Status != "completed"
		h.tasks.Update(idx, func(t Task) Task {
			if update.Status != "" {
				t.Status = update.Status
			}
			if update.Owner != "" {
				t.Owner = update.Owner
			}
			return t
		})
		updated = append(updated, fmt.Sprintf("%s -> %s", update.ID, update.Status))
		if wasCompleting {
			newlyUnblocked = append(newlyUnblocked, h.getUnblockedTasks(update.ID)...)
		}
	}
	var output strings.Builder
	if len(updated) > 0 {
		fmt.Fprintf(&output, "Updated %d tasks: %s", len(updated), strings.Join(updated, ", "))
	}
	for _, id := range newlyUnblocked {
		if output.Len() > 0 {
			output.WriteString("; ")
		}
		fmt.Fprintf(&output, "%s is now unblocked", id)
	}
	if len(blocked) > 0 {
		if output.Len() > 0 {
			output.WriteString("; ")
		}
		output.WriteString(strings.Join(blocked, "; "))
	}
	if len(notFound) > 0 {
		if output.Len() > 0 {
			output.WriteString("; ")
		}
		fmt.Fprintf(&output, "Not found: %s", strings.Join(notFound, ", "))
	}
	if len(updated) == 0 && (len(notFound) > 0 || len(blocked) > 0) {
		return tools.ResultError(output.String()), nil
	}
	if h.allCompleted() {
		h.tasks.Clear()
	}

	saveWarning := h.save()
	output.WriteString(saveWarning)

	return &tools.ToolCallResult{Output: output.String(), Meta: h.tasks.All()}, nil
}

func (h *tasksHandler) allCompleted() bool {
	if h.tasks.Length() == 0 {
		return false
	}
	allDone := true
	h.tasks.Range(func(_ int, task Task) bool {
		if task.Status != "completed" {
			allDone = false
			return false
		}
		return true
	})
	return allDone
}

func (h *tasksHandler) listTasks(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot list tasks: %v", err)), nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var output strings.Builder
	var completed, inProgress, pending, blockedCount int
	h.tasks.Range(func(_ int, task Task) bool {
		switch task.Status {
		case "completed":
			completed++
		case "in-progress":
			inProgress++
		default:
			pending++
			if canStart, _ := h.canStart(task.ID); !canStart {
				blockedCount++
			}
		}
		return true
	})
	if h.tasks.Length() == 0 {
		return &tools.ToolCallResult{Output: "No tasks.\n", Meta: h.tasks.All()}, nil
	}
	fmt.Fprintf(&output, "Tasks (%d done, %d in progress, %d pending", completed, inProgress, pending)
	if blockedCount > 0 {
		fmt.Fprintf(&output, ", %d blocked", blockedCount)
	}
	output.WriteString(")\n\n")
	h.tasks.Range(func(_ int, task Task) bool {
		var icon, suffix string
		switch task.Status {
		case "completed":
			icon = "✓"
		case "in-progress":
			icon = "■"
		default:
			if canStart, blockers := h.canStart(task.ID); canStart {
				icon = "□"
			} else {
				icon = "⚠"
				suffix = fmt.Sprintf(" → blocked by: %s", strings.Join(blockers, ", "))
			}
		}
		fmt.Fprintf(&output, "%s [%s] %s", icon, task.ID, task.Description)
		if task.Owner != "" {
			fmt.Fprintf(&output, " (%s)", task.Owner)
		}
		output.WriteString(suffix + "\n")
		return true
	})
	return &tools.ToolCallResult{Output: output.String(), Meta: h.tasks.All()}, nil
}

func (h *tasksHandler) addDependency(_ context.Context, params AddTaskDependencyArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot add dependency: %v", err)), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	task, idx := h.findTask(params.TaskID)
	if idx == -1 {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.TaskID)), nil
	}
	if task.Status == "completed" {
		return tools.ResultError(fmt.Sprintf("cannot add dependency to completed task: %s", params.TaskID)), nil
	}
	for _, blockerID := range params.BlockedBy {
		if !h.taskExists(blockerID) {
			return tools.ResultError(fmt.Sprintf("blocker not found: %s", blockerID)), nil
		}
		if blockerID == params.TaskID {
			return tools.ResultError(fmt.Sprintf("task cannot depend on itself: %s", params.TaskID)), nil
		}
	}
	if h.hasCircularDependency(params.TaskID, params.BlockedBy) {
		return tools.ResultError("circular dependency detected"), nil
	}
	existingBlockers := make(map[string]bool)
	for _, b := range task.BlockedBy {
		existingBlockers[b] = true
	}
	var added, alreadyExists []string
	for _, blockerID := range params.BlockedBy {
		if existingBlockers[blockerID] {
			alreadyExists = append(alreadyExists, blockerID)
		} else {
			added = append(added, blockerID)
		}
	}
	if len(added) == 0 {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Dependency already exists: %s is already blocked by %s", params.TaskID, strings.Join(alreadyExists, ", ")),
			Meta:   h.tasks.All(),
		}, nil
	}
	h.tasks.Update(idx, func(t Task) Task {
		t.BlockedBy = append(t.BlockedBy, added...)
		return t
	})
	for _, blockerID := range added {
		h.addBlockerLink(blockerID, params.TaskID)
	}

	saveWarning := h.save()

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Added dependency: %s is now blocked by %s%s", params.TaskID, strings.Join(added, ", "), saveWarning),
		Meta:   h.tasks.All(),
	}, nil
}

func (h *tasksHandler) removeDependency(_ context.Context, params RemoveTaskDependencyArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot remove dependency: %v", err)), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	task, idx := h.findTask(params.TaskID)
	if idx == -1 {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.TaskID)), nil
	}
	toRemove := make(map[string]bool)
	for _, b := range params.BlockedBy {
		toRemove[b] = true
	}
	var removed, newBlockedBy []string
	for _, blockerID := range task.BlockedBy {
		if toRemove[blockerID] {
			removed = append(removed, blockerID)
		} else {
			newBlockedBy = append(newBlockedBy, blockerID)
		}
	}
	if len(removed) == 0 {
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("No matching dependencies found to remove from %s", params.TaskID),
			Meta:   h.tasks.All(),
		}, nil
	}
	h.tasks.Update(idx, func(t Task) Task {
		t.BlockedBy = newBlockedBy
		return t
	})
	for _, blockerID := range removed {
		h.removeBlockerLink(blockerID, params.TaskID)
	}

	saveWarning := h.save()

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("Removed dependency: %s is no longer blocked by %s%s", params.TaskID, strings.Join(removed, ", "), saveWarning),
		Meta:   h.tasks.All(),
	}, nil
}

func (h *tasksHandler) getBlockedTasks(_ context.Context, params GetBlockedTasksArgs) (*tools.ToolCallResult, error) {
	if err := h.ensureLoaded(); err != nil {
		return tools.ResultError(fmt.Sprintf("cannot get blocked tasks: %v", err)), nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var output strings.Builder
	output.WriteString("Blocked tasks:\n")
	found := false
	h.tasks.Range(func(_ int, task Task) bool {
		if len(task.BlockedBy) == 0 || task.Status == "completed" {
			return true
		}
		if params.BlockedBy != "" {
			hasBlocker := false
			for _, b := range task.BlockedBy {
				if b == params.BlockedBy {
					hasBlocker = true
					break
				}
			}
			if !hasBlocker {
				return true
			}
		}
		if canStart, blockers := h.canStart(task.ID); !canStart {
			found = true
			fmt.Fprintf(&output, "- [%s] %s → blocked by: %s\n", task.ID, task.Description, strings.Join(blockers, ", "))
		}
		return true
	})
	if !found {
		output.Reset()
		output.WriteString("No blocked tasks")
		if params.BlockedBy != "" {
			fmt.Fprintf(&output, " (filtered by %s)", params.BlockedBy)
		}
		output.WriteString(".\n")
	}
	return &tools.ToolCallResult{Output: output.String(), Meta: h.tasks.All()}, nil
}

func (t *TasksTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:        ToolNameCreateTask,
			Category:    "tasks",
			Description: "Create a new task. Use blocked_by to specify dependencies on other tasks.",
			Parameters:  tools.MustSchemaFor[CreateTaskArgs](),
			Handler:     tools.NewHandler(t.handler.createTask),
			Annotations: tools.ToolAnnotations{Title: "Create Task", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameCreateTasks,
			Category:    "tasks",
			Description: "Create multiple tasks at once with dependencies.",
			Parameters:  tools.MustSchemaFor[CreateTasksArgs](),
			Handler:     tools.NewHandler(t.handler.createTasks),
			Annotations: tools.ToolAnnotations{Title: "Create Tasks", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameUpdateTasks,
			Category:    "tasks",
			Description: "Update the status of tasks. Cannot start a task blocked by incomplete dependencies.",
			Parameters:  tools.MustSchemaFor[UpdateTasksArgs](),
			Handler:     tools.NewHandler(t.handler.updateTasks),
			Annotations: tools.ToolAnnotations{Title: "Update Tasks", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameListTasks,
			Category:    "tasks",
			Description: "List all tasks with status and dependencies. Visual indicators: ✓=done, ■=in-progress, □=available, ⚠=blocked",
			Handler:     t.handler.listTasks,
			Annotations: tools.ToolAnnotations{Title: "List Tasks", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameAddTaskDep,
			Category:    "tasks",
			Description: "Add a dependency to an existing task.",
			Parameters:  tools.MustSchemaFor[AddTaskDependencyArgs](),
			Handler:     tools.NewHandler(t.handler.addDependency),
			Annotations: tools.ToolAnnotations{Title: "Add Task Dependency", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameRemoveTaskDep,
			Category:    "tasks",
			Description: "Remove a dependency from a task.",
			Parameters:  tools.MustSchemaFor[RemoveTaskDependencyArgs](),
			Handler:     tools.NewHandler(t.handler.removeDependency),
			Annotations: tools.ToolAnnotations{Title: "Remove Task Dependency", ReadOnlyHint: true},
		},
		{
			Name:        ToolNameGetBlockedTasks,
			Category:    "tasks",
			Description: "Get a list of all blocked tasks and what is blocking them.",
			Parameters:  tools.MustSchemaFor[GetBlockedTasksArgs](),
			Handler:     tools.NewHandler(t.handler.getBlockedTasks),
			Annotations: tools.ToolAnnotations{Title: "Get Blocked Tasks", ReadOnlyHint: true},
		},
	}, nil
}
