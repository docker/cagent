package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// FileTaskStoreOption configures a FileTaskStore
type FileTaskStoreOption func(*FileTaskStore)

// WithBaseDir sets a custom base directory (for testing)
func WithBaseDir(dir string) FileTaskStoreOption {
	return func(s *FileTaskStore) {
		s.baseDir = dir
	}
}

// NewFileTaskStore creates a new file-based task store
func NewFileTaskStore(listID string, opts ...FileTaskStoreOption) *FileTaskStore {
	s := &FileTaskStore{
		listID:  listID,
		baseDir: paths.GetTasksDir(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

// DefaultTaskListID returns a default task list ID based on the git repository.
// It uses the git common dir (shared across worktrees) to generate a deterministic ID.
// Format: <dirname>-<short-hash> (e.g., "cagent-a1b2c3d4")
// Falls back to working directory if not in a git repo.
func DefaultTaskListID() string {
	// Try to get the git common directory (shared across worktrees)
	repoPath := getGitCommonDir()
	if repoPath == "" {
		// Fallback to current working directory
		var err error
		repoPath, err = os.Getwd()
		if err != nil {
			return "default"
		}
	}

	// Get the directory name
	dirName := filepath.Base(repoPath)
	// Remove .git suffix if present (for bare repos or .git dirs)
	dirName = strings.TrimSuffix(dirName, ".git")
	if dirName == "" || dirName == "." {
		dirName = "project"
	}

	// Generate short hash of the full path for uniqueness
	hash := sha256.Sum256([]byte(repoPath))
	shortHash := hex.EncodeToString(hash[:])[:8]

	return fmt.Sprintf("%s-%s", dirName, shortHash)
}

// getGitCommonDir returns the path to the git common directory.
// This is the main .git directory shared across all worktrees.
// Returns empty string if not in a git repository.
func getGitCommonDir() string {
	// First check if we're in a git repo at all
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	gitCommonDir := strings.TrimSpace(string(output))
	if gitCommonDir == "" {
		return ""
	}

	// Convert to absolute path if relative
	if !filepath.IsAbs(gitCommonDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		gitCommonDir = filepath.Join(cwd, gitCommonDir)
	}

	// Clean the path
	gitCommonDir = filepath.Clean(gitCommonDir)

	// Resolve symlinks to get canonical path (important for macOS where /tmp -> /private/tmp)
	gitCommonDir, err = filepath.EvalSymlinks(gitCommonDir)
	if err != nil {
		// If we can't resolve symlinks, use the cleaned path
		gitCommonDir = filepath.Clean(gitCommonDir)
	}

	// If it ends with .git, return the parent directory
	if filepath.Base(gitCommonDir) == ".git" {
		return filepath.Dir(gitCommonDir)
	}

	// For bare repos or other cases, return as-is
	return gitCommonDir
}
