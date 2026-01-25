package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/cagent/pkg/paths"
)

// TaskListFile represents the JSON structure for persisted task lists
type TaskListFile struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

const taskListFileVersion = 1

// TaskStore defines the interface for task persistence
type TaskStore interface {
	// Load loads tasks from the store. Returns empty slice if not found.
	Load() ([]Task, error)
	// Save persists tasks to the store.
	Save(tasks []Task) error
}

// FileTaskStore implements TaskStore using a JSON file
type FileTaskStore struct {
	listID  string
	baseDir string
	mu      sync.RWMutex
}

// NewFileTaskStore creates a new file-based task store
func NewFileTaskStore(listID string) *FileTaskStore {
	return &FileTaskStore{
		listID:  listID,
		baseDir: paths.GetTasksDir(),
	}
}

// NewFileTaskStoreWithDir creates a file-based task store with a custom directory (for testing)
func NewFileTaskStoreWithDir(listID, baseDir string) *FileTaskStore {
	return &FileTaskStore{
		listID:  listID,
		baseDir: baseDir,
	}
}

func (s *FileTaskStore) filePath() string {
	// Sanitize listID to be safe as filename
	safeID := filepath.Base(s.listID)
	return filepath.Join(s.baseDir, safeID+".json")
}

// Load loads tasks from the JSON file
func (s *FileTaskStore) Load() ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - return empty list
			return []Task{}, nil
		}
		return nil, fmt.Errorf("reading task file: %w", err)
	}

	var file TaskListFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing task file: %w", err)
	}

	return file.Tasks, nil
}

// Save persists tasks to the JSON file
func (s *FileTaskStore) Save(tasks []Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.filePath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating tasks directory: %w", err)
	}

	file := TaskListFile{
		Version: taskListFileVersion,
		Tasks:   tasks,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tasks: %w", err)
	}

	// Write atomically using temp file + rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("writing task file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up on failure
		return fmt.Errorf("renaming task file: %w", err)
	}

	return nil
}

// MemoryTaskStore implements TaskStore with in-memory storage only (no persistence)
// Used when no listID is provided
type MemoryTaskStore struct{}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{}
}

func (s *MemoryTaskStore) Load() ([]Task, error) {
	return []Task{}, nil
}

func (s *MemoryTaskStore) Save(_ []Task) error {
	// No-op for memory store
	return nil
}
