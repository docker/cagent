package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Skill
		wantOK  bool
	}{
		{
			name: "valid frontmatter",
			content: `---
name: my-skill
description: A test skill
---

# Skill Content`,
			want:   Skill{Name: "my-skill", Description: "A test skill"},
			wantOK: true,
		},
		{
			name: "quoted values",
			content: `---
name: "quoted-name"
description: 'single quoted desc'
---

Body`,
			want:   Skill{Name: "quoted-name", Description: "single quoted desc"},
			wantOK: true,
		},
		{
			name:    "no frontmatter",
			content: "# Just content\n\nNo frontmatter here.",
			want:    Skill{},
			wantOK:  false,
		},
		{
			name: "only description",
			content: `---
description: Just a description
---

Content`,
			want:   Skill{Description: "Just a description"},
			wantOK: true,
		},
		{
			name:    "windows line endings",
			content: "---\r\nname: windows\r\ndescription: Windows skill\r\n---\r\n\r\nBody",
			want:    Skill{Name: "windows", Description: "Windows skill"},
			wantOK:  true,
		},
		{
			name:    "unclosed frontmatter",
			content: "---\nname: unclosed\ndescription: No closing\n\nBody",
			want:    Skill{},
			wantOK:  false,
		},
		{
			name: "all optional fields",
			content: `---
name: full-skill
description: A complete skill
license: Apache-2.0
compatibility: Requires docker and git
metadata:
  author: test-org
  version: "1.0"
allowed-tools:
  - Bash(git:*)
  - Read
  - Write
---

Body`,
			want: Skill{
				Name:          "full-skill",
				Description:   "A complete skill",
				License:       "Apache-2.0",
				Compatibility: "Requires docker and git",
				Metadata:      map[string]string{"author": "test-org", "version": "1.0"},
				AllowedTools:  []string{"Bash(git:*)", "Read", "Write"},
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseFrontmatter(tt.content)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want.Name, got.Name)
				assert.Equal(t, tt.want.Description, got.Description)
				assert.Equal(t, tt.want.License, got.License)
				assert.Equal(t, tt.want.Compatibility, got.Compatibility)
				assert.Equal(t, tt.want.Metadata, got.Metadata)
				assert.Equal(t, tt.want.AllowedTools, got.AllowedTools)
			}
		})
	}
}

func TestLoadSkillsFromDir_Flat(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "pdf-extractor")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: pdf-extractor
description: Extract text from PDF files
---

# PDF Extraction

Use pdftotext to extract content.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))

	skills := loadSkillsFromDir(tmpDir, false)

	require.Len(t, skills, 1)
	assert.Equal(t, "pdf-extractor", skills[0].Name)
	assert.Equal(t, "Extract text from PDF files", skills[0].Description)
	assert.Equal(t, filepath.Join(skillDir, "SKILL.md"), skills[0].FilePath)
	assert.Equal(t, skillDir, skills[0].BaseDir)
}

func TestLoadSkillsFromDir_Recursive(t *testing.T) {
	tmpDir := t.TempDir()

	nestedDir := filepath.Join(tmpDir, "db", "migrate")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))

	skillContent := `---
name: migrate
description: Database migration helper
---

# DB Migration

Run migrations with care.
`
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "SKILL.md"), []byte(skillContent), 0o644))

	skills := loadSkillsFromDir(tmpDir, true)

	require.Len(t, skills, 1)
	assert.Equal(t, "migrate", skills[0].Name)
	assert.Equal(t, "Database migration helper", skills[0].Description)
	assert.Equal(t, filepath.Join(nestedDir, "SKILL.md"), skills[0].FilePath)
	assert.Equal(t, nestedDir, skills[0].BaseDir)
}

func TestLoadSkillsFromDir_SkipHiddenAndSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	hiddenDir := filepath.Join(tmpDir, ".hidden-skill")
	require.NoError(t, os.MkdirAll(hiddenDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hiddenDir, "SKILL.md"), []byte("---\ndescription: Hidden\n---\n"), 0o644))

	skills := loadSkillsFromDir(tmpDir, false)
	assert.Empty(t, skills)
}

func TestLoadSkillsFromDir_SkipNoDescription(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "no-desc")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: no-description
---

# No Description

This skill has no description field.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))

	skills := loadSkillsFromDir(tmpDir, false)
	assert.Empty(t, skills)
}

func TestLoadSkillsFromDir_AllOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "full-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: full-skill
description: A complete skill with all fields
license: Apache-2.0
compatibility: Requires docker
metadata:
  author: test-org
  version: "2.0"
allowed-tools:
  - Bash(git:*)
  - Read
---

# Full Skill
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))

	skills := loadSkillsFromDir(tmpDir, false)

	require.Len(t, skills, 1)
	assert.Equal(t, "full-skill", skills[0].Name)
	assert.Equal(t, "A complete skill with all fields", skills[0].Description)
	assert.Equal(t, "Apache-2.0", skills[0].License)
	assert.Equal(t, "Requires docker", skills[0].Compatibility)
	assert.Equal(t, map[string]string{"author": "test-org", "version": "2.0"}, skills[0].Metadata)
	assert.Equal(t, []string{"Bash(git:*)", "Read"}, skills[0].AllowedTools)
}

func TestLoadSkillsFromDir_NonExistentDir(t *testing.T) {
	skills := loadSkillsFromDir("/nonexistent/path/12345", false)
	assert.Empty(t, skills)
}

func TestBuildSkillsPrompt(t *testing.T) {
	skills := []Skill{
		{
			Name:        "pdf-extractor",
			Description: "Extract text from PDFs",
			FilePath:    "/home/user/.claude/skills/pdf-extractor/SKILL.md",
			BaseDir:     "/home/user/.claude/skills/pdf-extractor",
		},
		{
			Name:        "code-review",
			Description: "Perform code reviews",
			FilePath:    "/project/.claude/skills/code-review/SKILL.md",
			BaseDir:     "/project/.claude/skills/code-review",
		},
	}

	prompt := BuildSkillsPrompt(skills)

	assert.Contains(t, prompt, "<available_skills>")
	assert.Contains(t, prompt, "</available_skills>")
	assert.Contains(t, prompt, "<skill>")
	assert.Contains(t, prompt, "</skill>")
	assert.Contains(t, prompt, "<name>pdf-extractor</name>")
	assert.Contains(t, prompt, "<description>Extract text from PDFs</description>")
	assert.Contains(t, prompt, "<location>/home/user/.claude/skills/pdf-extractor/SKILL.md</location>")
	assert.Contains(t, prompt, "<name>code-review</name>")
	assert.Contains(t, prompt, "<description>Perform code reviews</description>")
	assert.Contains(t, prompt, "<location>/project/.claude/skills/code-review/SKILL.md</location>")
	assert.Contains(t, prompt, "use the read_file tool to load the skill's SKILL.md file")
	assert.Contains(t, prompt, "description indicates what it does and when to use it")
}

func TestBuildSkillsPrompt_Empty(t *testing.T) {
	prompt := BuildSkillsPrompt(nil)
	assert.Empty(t, prompt)

	prompt = BuildSkillsPrompt([]Skill{})
	assert.Empty(t, prompt)
}

func TestLoad_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	claudeProjectDir := filepath.Join(tmpDir, ".claude", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(claudeProjectDir, 0o755))

	skillContent := `---
name: test-skill
description: Test project skill
---

# Test Skill
`
	require.NoError(t, os.WriteFile(filepath.Join(claudeProjectDir, "SKILL.md"), []byte(skillContent), 0o644))

	skills := Load()

	found := false
	for _, s := range skills {
		if s.Name == "test-skill" {
			found = true
			assert.Equal(t, "Test project skill", s.Description)
			break
		}
	}
	assert.True(t, found, "Expected to find test-skill from project directory")
}
