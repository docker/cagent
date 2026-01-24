package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadPromptFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use a unique filename to avoid conflicts with files in home directory
	filename := "test_prompt_unique_12345.md"
	err := os.WriteFile(filepath.Join(dir, filename), []byte("content"), 0o644)
	require.NoError(t, err)

	additionalPrompts, err := readPromptFiles(dir, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 1)
	assert.Equal(t, "content", additionalPrompts[0])
}

func TestReadPromptFilesParent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use a unique filename to avoid conflicts with files in home directory
	filename := "test_prompt_parent_12345.md"
	err := os.WriteFile(filepath.Join(dir, filename), []byte("content"), 0o644)
	require.NoError(t, err)

	child := filepath.Join(dir, "child")
	err = os.Mkdir(child, 0o755)
	require.NoError(t, err)

	additionalPrompts, err := readPromptFiles(child, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 1)
	assert.Equal(t, "content", additionalPrompts[0])
}

func TestReadPromptFilesReadFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use a unique filename to avoid conflicts with files in home directory
	filename := "test_prompt_readfirst_12345.md"
	err := os.WriteFile(filepath.Join(dir, filename), []byte("parent"), 0o644)
	require.NoError(t, err)

	child := filepath.Join(dir, "child")
	err = os.Mkdir(child, 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(child, filename), []byte("child"), 0o644)
	require.NoError(t, err)

	additionalPrompts, err := readPromptFiles(child, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 1)
	assert.Equal(t, "child", additionalPrompts[0])
}

func TestReadNoPromptFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Use a unique filename that won't exist anywhere
	filename := "test_prompt_nonexistent_12345.md"

	additionalPrompts, err := readPromptFiles(dir, filename)
	require.NoError(t, err)
	assert.Empty(t, additionalPrompts)
}

func TestReadPromptFilesFromWorkDirAndHome(t *testing.T) {
	t.Parallel()

	// Use a unique filename for this test
	filename := "test_prompt_workdir_and_home_12345.md"

	// Create a temp dir to simulate working directory
	workDir := t.TempDir()
	err := os.WriteFile(filepath.Join(workDir, filename), []byte("workdir content"), 0o644)
	require.NoError(t, err)

	// Get the actual home directory and check if we can write to it
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	homePath := filepath.Join(homeDir, filename)
	// Check if file already exists in home
	_, existsErr := os.Stat(homePath)
	fileExisted := existsErr == nil

	// Create file in home directory
	err = os.WriteFile(homePath, []byte("home content"), 0o644)
	if err != nil {
		t.Skip("Cannot write to home directory")
	}
	// Clean up only if we created it
	if !fileExisted {
		t.Cleanup(func() {
			os.Remove(homePath)
		})
	}

	additionalPrompts, err := readPromptFiles(workDir, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 2)
	assert.Equal(t, "workdir content", additionalPrompts[0])
	assert.Equal(t, "home content", additionalPrompts[1])
}

func TestReadPromptFilesFromHomeOnly(t *testing.T) {
	t.Parallel()

	// Use a unique filename for this test
	filename := "test_prompt_home_only_12345.md"

	// Create a temp dir without the prompt file
	workDir := t.TempDir()

	// Get the actual home directory and check if we can write to it
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	homePath := filepath.Join(homeDir, filename)
	// Check if file already exists in home
	_, existsErr := os.Stat(homePath)
	fileExisted := existsErr == nil

	// Create file in home directory
	err = os.WriteFile(homePath, []byte("home content"), 0o644)
	if err != nil {
		t.Skip("Cannot write to home directory")
	}
	// Clean up only if we created it
	if !fileExisted {
		t.Cleanup(func() {
			os.Remove(homePath)
		})
	}

	additionalPrompts, err := readPromptFiles(workDir, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 1)
	assert.Equal(t, "home content", additionalPrompts[0])
}

func TestReadPromptFilesDeduplication(t *testing.T) {
	t.Parallel()

	// Test that if working directory is under home, we don't duplicate
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	// Use a unique filename for this test
	filename := "test_prompt_dedup_12345.md"
	homePath := filepath.Join(homeDir, filename)

	// Check if file already exists in home
	_, existsErr := os.Stat(homePath)
	fileExisted := existsErr == nil

	// Create file only in home directory
	err = os.WriteFile(homePath, []byte("home content"), 0o644)
	if err != nil {
		t.Skip("Cannot write to home directory")
	}
	if !fileExisted {
		t.Cleanup(func() {
			os.Remove(homePath)
		})
	}

	// When working directory is home, should only return one result
	additionalPrompts, err := readPromptFiles(homeDir, filename)
	require.NoError(t, err)
	require.Len(t, additionalPrompts, 1)
	assert.Equal(t, "home content", additionalPrompts[0])
}
