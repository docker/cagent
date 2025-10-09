package builtin

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// vcsIgnorePatterns holds common VCS patterns to ignore
var vcsIgnorePatterns = []string{
	".git/*",
	".svn/*",
	".hg/*",
	".bzr/*",
	"CVS/*",
	"_darcs/*",
}

// parseGitignoreFile reads and parses a .gitignore file, returning a slice of patterns
func parseGitignoreFile(gitignorePath string) ([]string, error) {
	file, err := os.Open(gitignorePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Convert .gitignore patterns to our pattern format
		pattern := normalizeGitignorePattern(line)
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}

	return patterns, scanner.Err()
}

// normalizeGitignorePattern converts a .gitignore pattern to our internal format
func normalizeGitignorePattern(pattern string) string {
	// Remove leading slash (we work with relative paths)
	pattern = strings.TrimPrefix(pattern, "/")

	// Handle directory patterns
	if strings.HasSuffix(pattern, "/") {
		// Directory pattern - add wildcard to match contents
		return strings.TrimSuffix(pattern, "/") + "/*"
	}

	// For patterns that should match directories and files with the same name,
	// we need to handle both cases in our matching logic
	return pattern
}

// findGitignoreFiles searches for .gitignore files from the given directory upwards
// and returns all patterns found
func findGitignorePatterns(startDir string) []string {
	var allPatterns []string

	// Add common VCS patterns
	allPatterns = append(allPatterns, vcsIgnorePatterns...)

	current, err := filepath.Abs(startDir)
	if err != nil {
		return allPatterns
	}

	// Walk up the directory tree looking for .gitignore files
	for {
		gitignorePath := filepath.Join(current, ".gitignore")
		if patterns, err := parseGitignoreFile(gitignorePath); err == nil {
			allPatterns = append(allPatterns, patterns...)
		}

		// Check if this is the repository root (has .git directory)
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return allPatterns
}
