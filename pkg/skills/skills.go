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
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	FilePath      string            `yaml:"-"`
	BaseDir       string            `yaml:"-"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  []string          `yaml:"allowed-tools"`
}

// Load discovers and loads all skills from standard locations.
// Skills are loaded from (in order, later overrides earlier):
//   - ~/.codex/skills/ (recursive)
//   - ~/.claude/skills/ (flat)
//   - ./.claude/skills/ (flat, project-local)
func Load() []Skill {
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
	}

	// Load from project directory (flat)
	if cwd, err := os.Getwd(); err == nil {
		for _, skill := range loadSkillsFromDir(filepath.Join(cwd, ".claude", "skills"), false) {
			skillMap[skill.Name] = skill
		}
	}

	result := make([]Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}
	return result
}

// BuildSkillsPrompt generates a prompt section describing available skills.
func BuildSkillsPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("The following skills provide specialized instructions for specific tasks. ")
	sb.WriteString("Each skill's description indicates what it does and when to use it.\n\n")
	sb.WriteString("When a user's request matches a skill's description, use the read_file tool to load the skill's SKILL.md file from the location path. ")
	sb.WriteString("The file contains detailed instructions to follow for that task.\n\n")

	sb.WriteString("\n\n<available_skills>\n")
	for _, skill := range skills {
		sb.WriteString("  <skill>\n")
		sb.WriteString("    <name>")
		sb.WriteString(skill.Name)
		sb.WriteString("</name>\n")
		sb.WriteString("    <description>")
		sb.WriteString(skill.Description)
		sb.WriteString("</description>\n")
		sb.WriteString("    <location>")
		sb.WriteString(skill.FilePath)
		sb.WriteString("</location>\n")
		sb.WriteString("  </skill>\n")
	}
	sb.WriteString("</available_skills>")

	return sb.String()
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
