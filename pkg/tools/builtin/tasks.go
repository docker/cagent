package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/path"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameCreateTask       = "create_task"
	ToolNameGetTask          = "get_task"
	ToolNameUpdateTask       = "update_task"
	ToolNameDeleteTask       = "delete_task"
	ToolNameListTasks        = "list_tasks"
	ToolNameNextTask         = "next_task"
	ToolNameAddDependency    = "add_dependency"
	ToolNameRemoveDependency = "remove_dependency"
)

type TaskPriority string

const (
	PriorityCritical TaskPriority = "critical"
	PriorityHigh     TaskPriority = "high"
	PriorityMedium   TaskPriority = "medium"
	PriorityLow      TaskPriority = "low"
)

var priorityOrder = map[TaskPriority]int{
	PriorityCritical: 0,
	PriorityHigh:     1,
	PriorityMedium:   2,
	PriorityLow:      3,
}

func validPriority(p string) bool {
	_, ok := priorityOrder[TaskPriority(p)]
	return ok
}

type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
	StatusBlocked    TaskStatus = "blocked"
)

func validStatus(s string) bool {
	switch TaskStatus(s) {
	case StatusPending, StatusInProgress, StatusDone, StatusBlocked:
		return true
	}
	return false
}

type Task struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Priority     TaskPriority `json:"priority"`
	Status       TaskStatus   `json:"status"`
	Dependencies []string     `json:"dependencies"`
	CreatedAt    string       `json:"createdAt"`
	UpdatedAt    string       `json:"updatedAt"`
}

type taskWithEffective struct {
	Task
	EffectiveStatus TaskStatus `json:"effectiveStatus"`
}

type taskStore struct {
	Tasks map[string]Task `json:"tasks"`
}

type TasksTool struct {
	mu       sync.Mutex
	filePath string
	basePath string
}

var (
	_ tools.ToolSet      = (*TasksTool)(nil)
	_ tools.Instructable = (*TasksTool)(nil)
)

func NewTasksTool(storagePath string) *TasksTool {
	return &TasksTool{
		filePath: storagePath,
		basePath: filepath.Dir(storagePath),
	}
}

func (t *TasksTool) Instructions() string {
	return `## Using the Tasks Tools

These tools provide persistent task management with priorities, dependencies, and status tracking.

Tasks are persisted to a JSON file so they survive across sessions.

### Key concepts:
- **Priority**: critical > high > medium > low
- **Status**: pending, in_progress, done, blocked
- **Dependencies**: A task is automatically blocked if any of its dependencies are not done
- **Effective status**: The computed status taking dependencies into account

### Workflow:
1. Use create_task to create tasks with titles, descriptions, priorities, and dependencies
2. Use list_tasks to see all tasks sorted by priority (blocked tasks last)
3. Use next_task to find the highest-priority actionable task
4. Use update_task to change status, priority, or other fields as work progresses
5. Use add_dependency / remove_dependency to manage task ordering`
}

func (t *TasksTool) load() taskStore {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return taskStore{Tasks: make(map[string]Task)}
	}
	var store taskStore
	if err := json.Unmarshal(data, &store); err != nil {
		return taskStore{Tasks: make(map[string]Task)}
	}
	if store.Tasks == nil {
		store.Tasks = make(map[string]Task)
	}
	return store
}

func (t *TasksTool) save(store taskStore) error {
	if err := os.MkdirAll(filepath.Dir(t.filePath), 0o700); err != nil {
		return fmt.Errorf("creating storage directory: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling task store: %w", err)
	}
	return os.WriteFile(t.filePath, data, 0o644)
}

func effectiveStatus(task Task, tasks map[string]Task) TaskStatus {
	if task.Status == StatusDone {
		return StatusDone
	}
	for _, depID := range task.Dependencies {
		dep, ok := tasks[depID]
		if ok && dep.Status != StatusDone {
			return StatusBlocked
		}
	}
	return task.Status
}

func hasCycle(tasks map[string]Task, startID string, deps []string) bool {
	visited := make(map[string]bool)
	stack := make([]string, len(deps))
	copy(stack, deps)
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == startID {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		if task, ok := tasks[current]; ok {
			stack = append(stack, task.Dependencies...)
		}
	}
	return false
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (t *TasksTool) resolveDescription(description, filePath string) (string, error) {
	if filePath != "" {
		validatedPath, err := path.ValidatePathInDirectory(filePath, t.basePath)
		if err != nil {
			return "", fmt.Errorf("invalid file path: %w", err)
		}
		data, err := os.ReadFile(validatedPath)
		if err != nil {
			return "", fmt.Errorf("reading file %s: %w", validatedPath, err)
		}
		return string(data), nil
	}
	return description, nil
}

func sortTasks(tasks []taskWithEffective) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if (a.EffectiveStatus == StatusBlocked) != (b.EffectiveStatus == StatusBlocked) {
			return a.EffectiveStatus != StatusBlocked
		}
		pa, pb := priorityOrder[a.Priority], priorityOrder[b.Priority]
		if pa != pb {
			return pa < pb
		}
		return a.CreatedAt < b.CreatedAt
	})
}

// Tool argument types

type CreateTaskArgs struct {
	Title        string   `json:"title" jsonschema:"Short title for the task"`
	Description  string   `json:"description,omitempty" jsonschema:"Task description (ignored if path is given)"`
	Path         string   `json:"path,omitempty" jsonschema:"Path to a markdown file whose content becomes the task description"`
	Priority     string   `json:"priority,omitempty" jsonschema:"Priority: critical, high, medium (default), or low"`
	Dependencies []string `json:"dependencies,omitempty" jsonschema:"IDs of tasks that must be completed before this one"`
}

type GetTaskArgs struct {
	ID string `json:"id" jsonschema:"Task ID"`
}

type UpdateTaskArgs struct {
	ID           string   `json:"id" jsonschema:"Task ID to update"`
	Title        string   `json:"title,omitempty" jsonschema:"New title"`
	Description  string   `json:"description,omitempty" jsonschema:"New description"`
	Path         string   `json:"path,omitempty" jsonschema:"Read new description from this file"`
	Priority     string   `json:"priority,omitempty" jsonschema:"New priority: critical, high, medium, or low"`
	Status       string   `json:"status,omitempty" jsonschema:"New status: pending, in_progress, done, or blocked"`
	Dependencies []string `json:"dependencies,omitempty" jsonschema:"Replace dependency list with these task IDs"`
}

type DeleteTaskArgs struct {
	ID string `json:"id" jsonschema:"Task ID to delete"`
}

type ListTasksArgs struct {
	Status   string `json:"status,omitempty" jsonschema:"Filter by effective status: pending, in_progress, done, blocked"`
	Priority string `json:"priority,omitempty" jsonschema:"Filter by priority level: critical, high, medium, low"`
}

type AddDependencyArgs struct {
	TaskID      string `json:"taskId" jsonschema:"The task that depends on another"`
	DependsOnID string `json:"dependsOnId" jsonschema:"The task that must be completed first"`
}

type RemoveDependencyArgs struct {
	TaskID      string `json:"taskId" jsonschema:"The task to remove the dependency from"`
	DependsOnID string `json:"dependsOnId" jsonschema:"The dependency to remove"`
}

// Tool handlers

func (t *TasksTool) createTask(_ context.Context, params CreateTaskArgs) (*tools.ToolCallResult, error) {
	desc, err := t.resolveDescription(params.Description, params.Path)
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}

	priority := TaskPriority(params.Priority)
	if params.Priority == "" {
		priority = PriorityMedium
	} else if !validPriority(params.Priority) {
		return tools.ResultError(fmt.Sprintf("invalid priority: %s", params.Priority)), nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	id := uuid.New().String()

	deps := params.Dependencies
	if deps == nil {
		deps = []string{}
	}
	for _, depID := range deps {
		if _, ok := store.Tasks[depID]; !ok {
			return tools.ResultError(fmt.Sprintf("dependency task not found: %s", depID)), nil
		}
	}
	if hasCycle(store.Tasks, id, deps) {
		return tools.ResultError("adding these dependencies would create a cycle"), nil
	}

	task := Task{
		ID:           id,
		Title:        params.Title,
		Description:  desc,
		Priority:     priority,
		Status:       StatusPending,
		Dependencies: deps,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}

	store.Tasks[id] = task
	if err := t.save(store); err != nil {
		return tools.ResultError(err.Error()), nil
	}

	return taskResult(task), nil
}

func (t *TasksTool) getTask(_ context.Context, params GetTaskArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	task, ok := store.Tasks[params.ID]
	if !ok {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.ID)), nil
	}

	return taskWithEffectiveResult(task, store.Tasks), nil
}

func (t *TasksTool) updateTask(_ context.Context, params UpdateTaskArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	task, ok := store.Tasks[params.ID]
	if !ok {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.ID)), nil
	}

	if params.Title != "" {
		task.Title = params.Title
	}
	if params.Path != "" || params.Description != "" {
		desc, err := t.resolveDescription(params.Description, params.Path)
		if err != nil {
			return tools.ResultError(err.Error()), nil
		}
		task.Description = desc
	}
	if params.Priority != "" {
		if !validPriority(params.Priority) {
			return tools.ResultError(fmt.Sprintf("invalid priority: %s", params.Priority)), nil
		}
		task.Priority = TaskPriority(params.Priority)
	}
	if params.Status != "" {
		if !validStatus(params.Status) {
			return tools.ResultError(fmt.Sprintf("invalid status: %s", params.Status)), nil
		}
		task.Status = TaskStatus(params.Status)
	}
	if params.Dependencies != nil {
		for _, depID := range params.Dependencies {
			if _, exists := store.Tasks[depID]; !exists {
				return tools.ResultError(fmt.Sprintf("dependency task not found: %s", depID)), nil
			}
		}
		if hasCycle(store.Tasks, params.ID, params.Dependencies) {
			return tools.ResultError("adding these dependencies would create a cycle"), nil
		}
		task.Dependencies = params.Dependencies
	}

	task.UpdatedAt = now()
	store.Tasks[params.ID] = task

	if err := t.save(store); err != nil {
		return tools.ResultError(err.Error()), nil
	}

	return taskResult(task), nil
}

func (t *TasksTool) deleteTask(_ context.Context, params DeleteTaskArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	if _, ok := store.Tasks[params.ID]; !ok {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.ID)), nil
	}

	for id, task := range store.Tasks {
		filtered := make([]string, 0, len(task.Dependencies))
		for _, d := range task.Dependencies {
			if d != params.ID {
				filtered = append(filtered, d)
			}
		}
		task.Dependencies = filtered
		store.Tasks[id] = task
	}

	delete(store.Tasks, params.ID)

	if err := t.save(store); err != nil {
		return tools.ResultError(err.Error()), nil
	}

	out, err := json.MarshalIndent(map[string]string{"deleted": params.ID}, "", "  ")
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}
	return &tools.ToolCallResult{Output: string(out)}, nil
}

func (t *TasksTool) listTasks(_ context.Context, params ListTasksArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	var tasks []taskWithEffective
	for _, task := range store.Tasks {
		tasks = append(tasks, taskWithEffective{
			Task:            task,
			EffectiveStatus: effectiveStatus(task, store.Tasks),
		})
	}

	if params.Status != "" {
		filtered := tasks[:0]
		for _, task := range tasks {
			if string(task.EffectiveStatus) == params.Status {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	if params.Priority != "" {
		filtered := tasks[:0]
		for _, task := range tasks {
			if string(task.Priority) == params.Priority {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}

	sortTasks(tasks)

	out, err := json.Marshal(tasks)
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}
	return &tools.ToolCallResult{Output: string(out)}, nil
}

func (t *TasksTool) nextTask(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	var tasks []taskWithEffective
	for _, task := range store.Tasks {
		tasks = append(tasks, taskWithEffective{
			Task:            task,
			EffectiveStatus: effectiveStatus(task, store.Tasks),
		})
	}
	sortTasks(tasks)

	for _, task := range tasks {
		if task.EffectiveStatus != StatusBlocked && task.EffectiveStatus != StatusDone {
			out, err := json.Marshal(task)
			if err != nil {
				return tools.ResultError(err.Error()), nil
			}
			return &tools.ToolCallResult{Output: string(out)}, nil
		}
	}

	return &tools.ToolCallResult{
		Output: "No actionable tasks. Everything is either done or blocked.",
	}, nil
}

func (t *TasksTool) addDependency(_ context.Context, params AddDependencyArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	task, ok := store.Tasks[params.TaskID]
	if !ok {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.TaskID)), nil
	}
	if _, ok := store.Tasks[params.DependsOnID]; !ok {
		return tools.ResultError(fmt.Sprintf("dependency task not found: %s", params.DependsOnID)), nil
	}
	if slices.Contains(task.Dependencies, params.DependsOnID) {
		return tools.ResultError("dependency already exists"), nil
	}

	newDeps := append(task.Dependencies, params.DependsOnID)
	if hasCycle(store.Tasks, params.TaskID, newDeps) {
		return tools.ResultError("adding this dependency would create a cycle"), nil
	}

	task.Dependencies = newDeps
	task.UpdatedAt = now()
	store.Tasks[params.TaskID] = task

	if err := t.save(store); err != nil {
		return tools.ResultError(err.Error()), nil
	}

	return taskResult(task), nil
}

func (t *TasksTool) removeDependency(_ context.Context, params RemoveDependencyArgs) (*tools.ToolCallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	store := t.load()
	task, ok := store.Tasks[params.TaskID]
	if !ok {
		return tools.ResultError(fmt.Sprintf("task not found: %s", params.TaskID)), nil
	}

	filtered := make([]string, 0, len(task.Dependencies))
	for _, d := range task.Dependencies {
		if d != params.DependsOnID {
			filtered = append(filtered, d)
		}
	}
	task.Dependencies = filtered
	task.UpdatedAt = now()
	store.Tasks[params.TaskID] = task

	if err := t.save(store); err != nil {
		return tools.ResultError(err.Error()), nil
	}

	return taskResult(task), nil
}

func taskResult(task Task) *tools.ToolCallResult {
	out, err := json.Marshal(task)
	if err != nil {
		return tools.ResultError(err.Error())
	}
	return &tools.ToolCallResult{Output: string(out)}
}

func taskWithEffectiveResult(task Task, tasks map[string]Task) *tools.ToolCallResult {
	result := taskWithEffective{
		Task:            task,
		EffectiveStatus: effectiveStatus(task, tasks),
	}
	out, err := json.Marshal(result)
	if err != nil {
		return tools.ResultError(err.Error())
	}
	return &tools.ToolCallResult{Output: string(out)}
}

func boolPtr(b bool) *bool { return &b }

func (t *TasksTool) Tools(_ context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:        ToolNameCreateTask,
			Category:    "tasks",
			Description: "Create a new task. Provide a title and either a description or a path to a markdown file whose content will be used as the description. Optionally set priority and dependencies on other task IDs.",
			Parameters:  tools.MustSchemaFor[CreateTaskArgs](),
			Handler:     tools.NewHandler(t.createTask),
			Annotations: tools.ToolAnnotations{
				Title: "Create Task",
			},
		},
		{
			Name:        ToolNameGetTask,
			Category:    "tasks",
			Description: "Get full details of a single task by ID, including its effective status (blocked if any dependency is not done).",
			Parameters:  tools.MustSchemaFor[GetTaskArgs](),
			Handler:     tools.NewHandler(t.getTask),
			Annotations: tools.ToolAnnotations{
				Title:        "Get Task",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameUpdateTask,
			Category:    "tasks",
			Description: "Update fields of an existing task. You can change title, description (or path to re-read from file), priority, status, and dependencies.",
			Parameters:  tools.MustSchemaFor[UpdateTaskArgs](),
			Handler:     tools.NewHandler(t.updateTask),
			Annotations: tools.ToolAnnotations{
				Title: "Update Task",
			},
		},
		{
			Name:        ToolNameDeleteTask,
			Category:    "tasks",
			Description: "Delete a task by ID. Also removes it from other tasks' dependency lists.",
			Parameters:  tools.MustSchemaFor[DeleteTaskArgs](),
			Handler:     tools.NewHandler(t.deleteTask),
			Annotations: tools.ToolAnnotations{
				Title:           "Delete Task",
				DestructiveHint: boolPtr(true),
			},
		},
		{
			Name:        ToolNameListTasks,
			Category:    "tasks",
			Description: "List all tasks, sorted by priority (critical first) with blocked tasks last. Optionally filter by status or priority.",
			Parameters:  tools.MustSchemaFor[ListTasksArgs](),
			Handler:     tools.NewHandler(t.listTasks),
			Annotations: tools.ToolAnnotations{
				Title:        "List Tasks",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameNextTask,
			Category:    "tasks",
			Description: strings.TrimSpace("Get the highest-priority actionable task — one that is not blocked and not done. Great for asking 'what should I work on next?'"),
			Handler:     t.nextTask,
			Annotations: tools.ToolAnnotations{
				Title:        "Next Task",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameAddDependency,
			Category:    "tasks",
			Description: "Add a dependency: taskId will be blocked until dependsOnId is done.",
			Parameters:  tools.MustSchemaFor[AddDependencyArgs](),
			Handler:     tools.NewHandler(t.addDependency),
			Annotations: tools.ToolAnnotations{
				Title: "Add Dependency",
			},
		},
		{
			Name:        ToolNameRemoveDependency,
			Category:    "tasks",
			Description: "Remove a dependency from a task.",
			Parameters:  tools.MustSchemaFor[RemoveDependencyArgs](),
			Handler:     tools.NewHandler(t.removeDependency),
			Annotations: tools.ToolAnnotations{
				Title: "Remove Dependency",
			},
		},
	}, nil
}
