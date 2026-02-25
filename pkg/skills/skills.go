package skills

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/docker/cagent/pkg/paths"
)

const skillFile = "SKILL.md"

// Skill represents a loaded skill with its metadata and content location.
type Skill struct {
	Name            string            `yaml:"name"`
	Description     string            `yaml:"description"`
	ArgsDescription string            `yaml:"args"`
	FilePath        string            `yaml:"-"`
	BaseDir         string            `yaml:"-"`
	Files           []string          `yaml:"-"`
	License         string            `yaml:"license"`
	Compatibility   string            `yaml:"compatibility"`
	Metadata        map[string]string `yaml:"metadata"`
	AllowedTools    []string          `yaml:"allowed-tools"`
}

// HasArgs returns true if the skill declares an args description,
// indicating it accepts arguments when invoked as a slash command.
func (s Skill) HasArgs() bool {
	return s.ArgsDescription != ""
}

// Load discovers and loads skills from the given sources.
// Each source is either "local" (for filesystem-based skills) or an HTTP/HTTPS URL
// (for remote skills per the well-known skills discovery spec).
//
// Local skills are loaded from (in order, later overrides earlier):
//
// Global locations (from home directory):
//   - ~/.codex/skills/ (recursive)
//   - ~/.claude/skills/ (flat)
//   - ~/.agents/skills/ (recursive)
//
// Project locations (from git root up to cwd, closest wins):
//   - .claude/skills/ (flat, only at cwd)
//   - .agents/skills/ (flat, scanned from git root to cwd)
func Load(sources []string) []Skill {
	skillMap := make(map[string]Skill)

	for _, source := range sources {
		switch {
		case source == "local":
			for _, skill := range loadLocalSkills() {
				skillMap[skill.Name] = skill
			}
		case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
			for _, skill := range loadRemoteSkills(source) {
				skillMap[source+"/"+skill.Name] = skill
			}
		}
	}

	result := make([]Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}
	return result
}

// loadLocalSkills loads skills from standard filesystem locations.
func loadLocalSkills() []Skill {
	skillMap := make(map[string]Skill)

	homeDir := paths.GetHomeDir()
	if homeDir != "" {
		// Load from codex directory (recursive)
		for _, skill := range loadSkillsFromDir(filepath.Join(homeDir, ".codex", "skills"), true) {
			skillMap[skill.Name] = skill
		}
		// Load from claude user directory (flat)
		for _, skill := range loadSkillsFromDir(filepath.Join(homeDir, ".claude", "skills"), false) {
			skillMap[skill.Name] = skill
		}
		// Load from agents user directory (recursive)
		for _, skill := range loadSkillsFromDir(filepath.Join(homeDir, ".agents", "skills"), true) {
			skillMap[skill.Name] = skill
		}
	}

	// Load from project directories
	if cwd, err := os.Getwd(); err == nil {
		// Load .claude/skills from cwd only (backward compatibility)
		for _, skill := range loadSkillsFromDir(filepath.Join(cwd, ".claude", "skills"), false) {
			skillMap[skill.Name] = skill
		}

		// Load .agents/skills from git root up to cwd (closest wins)
		// We iterate from root to cwd so that later (closer) directories override earlier ones
		for _, dir := range projectSearchDirs(cwd) {
			for _, skill := range loadSkillsFromDir(filepath.Join(dir, ".agents", "skills"), false) {
				skillMap[skill.Name] = skill
			}
		}
	}

	result := make([]Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}
	return result
}

// projectSearchDirs returns directories from git root to cwd (inclusive).
// If not in a git repo, returns only cwd.
// The returned slice is ordered from root to cwd so that closer directories
// can override skills from parent directories.
func projectSearchDirs(cwd string) []string {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return []string{cwd}
	}

	// Find git root by walking up
	gitRoot := findGitRoot(absPath)
	if gitRoot == "" {
		// Not in a git repo, just return cwd
		return []string{absPath}
	}

	// Build list of directories from git root to cwd
	var dirs []string
	current := absPath
	for {
		dirs = append(dirs, current)
		if current == gitRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding git root (shouldn't happen)
			break
		}
		current = parent
	}

	// Reverse so we go from root to cwd (earlier entries get overridden by later)
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	return dirs
}

// findGitRoot finds the git repository root by looking for .git directory or file.
// Returns empty string if not in a git repository.
func findGitRoot(dir string) string {
	current := dir
	for {
		gitPath := filepath.Join(current, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			// .git can be a directory (normal repo) or a file (worktree/submodule)
			if info.IsDir() || info.Mode().IsRegular() {
				return current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			return ""
		}
		current = parent
	}
}

// loadSkillsFromDir loads skills from a directory.
// If recursive is true, it walks all subdirectories looking for SKILL.md files.
// If recursive is false, it only looks for SKILL.md in immediate subdirectories.
func loadSkillsFromDir(dir string, recursive bool) []Skill {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	if recursive {
		return loadSkillsRecursive(dir)
	}
	return loadSkillsFlat(dir)
}

// loadSkillsFlat loads skills from immediate subdirectories only (Claude format).
func loadSkillsFlat(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() || isHiddenOrSymlink(entry) {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())
		skillFilePath := filepath.Join(skillDir, skillFile)

		skill, ok := loadSkillFile(skillFilePath, entry.Name())
		if ok {
			skills = append(skills, skill)
		}
	}
	return skills
}

// loadSkillsRecursive loads skills from all subdirectories (Codex format).
func loadSkillsRecursive(dir string) []Skill {
	var skills []Skill

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if isHiddenOrSymlink(d) || d.Name() != skillFile {
			return nil
		}

		skillDir := filepath.Dir(path)
		dirName := filepath.Base(skillDir)

		if skill, ok := loadSkillFile(path, dirName); ok {
			skills = append(skills, skill)
		}
		return nil
	})

	return skills
}

// loadSkillFile reads and parses a SKILL.md file.
func loadSkillFile(path, dirName string) (Skill, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}

	skill, ok := parseFrontmatter(string(content))
	if !ok || !isValidSkill(skill) {
		return Skill{}, false
	}

	skill.Name = cmp.Or(skill.Name, dirName)
	skill.FilePath = path
	skill.BaseDir = filepath.Dir(path)

	return skill, true
}

// parseFrontmatter extracts YAML frontmatter from a markdown file.
// Returns the parsed Skill and whether parsing was successful.
func parseFrontmatter(content string) (Skill, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	if !strings.HasPrefix(content, "---") {
		return Skill{}, false
	}

	endIndex := strings.Index(content[3:], "\n---")
	if endIndex == -1 {
		return Skill{}, false
	}

	frontmatterBlock := content[4 : endIndex+3]

	var skill Skill
	if err := yaml.Unmarshal([]byte(frontmatterBlock), &skill); err != nil {
		return Skill{}, false
	}

	return skill, true
}

// isValidSkill validates skill constraints.
func isValidSkill(skill Skill) bool {
	// Description and name is required
	if skill.Description == "" || skill.Name == "" {
		return false
	}

	return true
}

// isHiddenOrSymlink returns true for hidden files/dirs or symlinks.
func isHiddenOrSymlink(entry fs.DirEntry) bool {
	return strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0
}
