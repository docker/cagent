package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)
	assert.Empty(t, config.Aliases)
}

func TestConfig_LoadWithNilAliases(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config file without aliases field
	require.NoError(t, os.WriteFile(configFile, []byte("# empty config\n"), 0o644))

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)
	assert.NotNil(t, config.Aliases)
	assert.Empty(t, config.Aliases)
}

func TestConfig_SetGetAlias(t *testing.T) {
	t.Parallel()

	config := &Config{Aliases: make(map[string]*Alias)}

	err := config.SetAlias("test", &Alias{Path: "agentcatalog/test-agent"})
	require.NoError(t, err)

	alias, ok := config.GetAlias("test")
	assert.True(t, ok)
	assert.Equal(t, "agentcatalog/test-agent", alias.Path)

	_, ok = config.GetAlias("nonexistent")
	assert.False(t, ok)
}

func TestConfig_SetAlias_Validation(t *testing.T) {
	t.Parallel()

	config := &Config{Aliases: make(map[string]*Alias)}

	tests := []struct {
		name      string
		aliasName string
		path      string
		wantErr   string
	}{
		{"empty name", "", "some/path", "alias name cannot be empty"},
		{"empty path", "valid", "", "agent path cannot be empty"},
		{"starts with hyphen", "-invalid", "some/path", "invalid alias name"},
		{"starts with underscore", "_invalid", "some/path", "invalid alias name"},
		{"contains slash", "in/valid", "some/path", "invalid alias name"},
		{"contains space", "in valid", "some/path", "invalid alias name"},
		{"contains dot", "in.valid", "some/path", "invalid alias name"},
		{"path traversal attempt", "../etc/passwd", "some/path", "invalid alias name"},
		{"valid simple", "myalias", "some/path", ""},
		{"valid with hyphen", "my-alias", "some/path", ""},
		{"valid with underscore", "my_alias", "some/path", ""},
		{"valid with numbers", "alias123", "some/path", ""},
		{"valid starts with number", "123alias", "some/path", ""},
		{"valid default", "default", "some/path", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := config.SetAlias(tt.aliasName, &Alias{Path: tt.path})
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAliasName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wantErr bool
	}{
		{"", true},
		{"-starts-with-hyphen", true},
		{"_starts-with-underscore", true},
		{"has space", true},
		{"has/slash", true},
		{"has.dot", true},
		{"has:colon", true},
		{"valid", false},
		{"valid-name", false},
		{"valid_name", false},
		{"ValidName", false},
		{"valid123", false},
		{"123valid", false},
		{"a", false},
		{"A", false},
		{"1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAliasName(tt.name)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_DeleteAlias(t *testing.T) {
	t.Parallel()

	config := &Config{
		Aliases: map[string]*Alias{
			"code":    {Path: "agentcatalog/notion-expert"},
			"myagent": {Path: "/path/to/myagent.yaml"},
		},
	}

	assert.True(t, config.DeleteAlias("code"))
	assert.Len(t, config.Aliases, 1)

	assert.False(t, config.DeleteAlias("nonexistent"))
}

func TestConfig_SaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Aliases: map[string]*Alias{
			"code":    {Path: "agentcatalog/notion-expert"},
			"myagent": {Path: "/path/to/myagent.yaml"},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.Equal(t, CurrentVersion, loaded.Version)
	assert.Equal(t, config.Aliases["code"].Path, loaded.Aliases["code"].Path)
	assert.Equal(t, config.Aliases["myagent"].Path, loaded.Aliases["myagent"].Path)
}

func TestConfig_MigrateFromLegacy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	legacyFile := filepath.Join(tmpDir, "aliases.yaml")

	// Create legacy aliases file
	legacyContent := `code: agentcatalog/notion-expert
myagent: /path/to/myagent.yaml
`
	require.NoError(t, os.WriteFile(legacyFile, []byte(legacyContent), 0o644))

	// Load config - should migrate from legacy and persist
	config, err := loadFrom(configFile, legacyFile)
	require.NoError(t, err)

	assert.Len(t, config.Aliases, 2)
	assert.Equal(t, "agentcatalog/notion-expert", config.Aliases["code"].Path)

	// Verify migration was persisted
	assert.FileExists(t, configFile)

	// Verify legacy file was deleted (not renamed to .bak)
	assert.NoFileExists(t, legacyFile)
	assert.NoFileExists(t, legacyFile+".bak")
}

func TestConfig_MigrateFromLegacy_MalformedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	legacyFile := filepath.Join(tmpDir, "aliases.yaml")

	// Create malformed legacy aliases file
	require.NoError(t, os.WriteFile(legacyFile, []byte("not: valid: yaml: content"), 0o644))

	// Load config - should not fail, just skip migration
	config, err := loadFrom(configFile, legacyFile)
	require.NoError(t, err)
	assert.Empty(t, config.Aliases)

	// Legacy file should remain (not renamed since migration failed)
	assert.FileExists(t, legacyFile)
}

func TestConfig_NoMigrationWhenAliasesExist(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	legacyFile := filepath.Join(tmpDir, "aliases.yaml")

	// Create config with existing alias - use new struct format
	require.NoError(t, os.WriteFile(configFile, []byte("aliases:\n  existing:\n    path: already-here\n"), 0o644))

	// Create legacy file
	require.NoError(t, os.WriteFile(legacyFile, []byte("code: should-not-migrate\n"), 0o644))

	config, err := loadFrom(configFile, legacyFile)
	require.NoError(t, err)

	assert.Len(t, config.Aliases, 1)
	assert.Equal(t, "already-here", config.Aliases["existing"].Path)
	_, hasCode := config.Aliases["code"]
	assert.False(t, hasCode)
}

func TestConfig_MigrateWhenConfigEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	legacyFile := filepath.Join(tmpDir, "aliases.yaml")

	// Create empty config
	require.NoError(t, os.WriteFile(configFile, []byte("aliases: {}\n"), 0o644))

	// Create legacy file
	require.NoError(t, os.WriteFile(legacyFile, []byte("code: agentcatalog/notion-expert\n"), 0o644))

	config, err := loadFrom(configFile, legacyFile)
	require.NoError(t, err)

	assert.Len(t, config.Aliases, 1)
	assert.Equal(t, "agentcatalog/notion-expert", config.Aliases["code"].Path)
}

func TestConfig_NoLegacyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	nonExistentLegacy := filepath.Join(tmpDir, "aliases.yaml")

	// Load config with non-existent legacy path
	config, err := loadFrom(configFile, nonExistentLegacy)
	require.NoError(t, err)

	// Aliases should be empty
	assert.Empty(t, config.Aliases)
}

func TestConfig_AtomicWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Aliases: map[string]*Alias{
			"test": {Path: "agentcatalog/test-agent"},
		},
	}

	// Save should succeed
	require.NoError(t, config.saveTo(configFile))

	// Verify file exists and has correct content
	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)
	assert.Equal(t, "agentcatalog/test-agent", loaded.Aliases["test"].Path)

	// Verify no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "config.yaml", entries[0].Name())
}

func TestConfig_AtomicWrite_Permissions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Aliases: map[string]*Alias{
			"test": {Path: "agentcatalog/test-agent"},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	// Verify file permissions are 0600
	info, err := os.Stat(configFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestConfig_AliasWithOptions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Aliases: map[string]*Alias{
			"yolo-agent":  {Path: "agentcatalog/coder", Yolo: true},
			"model-agent": {Path: "agentcatalog/coder", Model: "openai/gpt-4o-mini"},
			"both":        {Path: "agentcatalog/coder", Yolo: true, Model: "anthropic/claude-sonnet-4-0"},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	// Verify yolo option
	yoloAlias, ok := loaded.GetAlias("yolo-agent")
	require.True(t, ok)
	assert.Equal(t, "agentcatalog/coder", yoloAlias.Path)
	assert.True(t, yoloAlias.Yolo)
	assert.Empty(t, yoloAlias.Model)

	// Verify model option
	modelAlias, ok := loaded.GetAlias("model-agent")
	require.True(t, ok)
	assert.Equal(t, "agentcatalog/coder", modelAlias.Path)
	assert.False(t, modelAlias.Yolo)
	assert.Equal(t, "openai/gpt-4o-mini", modelAlias.Model)

	// Verify both options
	bothAlias, ok := loaded.GetAlias("both")
	require.True(t, ok)
	assert.Equal(t, "agentcatalog/coder", bothAlias.Path)
	assert.True(t, bothAlias.Yolo)
	assert.Equal(t, "anthropic/claude-sonnet-4-0", bothAlias.Model)
}

func TestConfig_SetAliasWithOptions(t *testing.T) {
	t.Parallel()

	config := &Config{Aliases: make(map[string]*Alias)}

	// Set alias with yolo option
	err := config.SetAlias("yolo-test", &Alias{
		Path: "agentcatalog/test",
		Yolo: true,
	})
	require.NoError(t, err)

	alias, ok := config.GetAlias("yolo-test")
	require.True(t, ok)
	assert.True(t, alias.Yolo)

	// Set alias with model option
	err = config.SetAlias("model-test", &Alias{
		Path:  "agentcatalog/test",
		Model: "openai/gpt-4o",
	})
	require.NoError(t, err)

	alias, ok = config.GetAlias("model-test")
	require.True(t, ok)
	assert.Equal(t, "openai/gpt-4o", alias.Model)
}

func TestConfig_ModelsGateway(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		ModelsGateway: "https://models.example.com",
		Aliases: map[string]*Alias{
			"test": {Path: "agentcatalog/test-agent"},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.Equal(t, "https://models.example.com", loaded.ModelsGateway)
	assert.Equal(t, "agentcatalog/test-agent", loaded.Aliases["test"].Path)
}

func TestConfig_ModelsGateway_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.Empty(t, config.ModelsGateway)
}

func TestConfig_ModelsGateway_OnlyGateway(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config file with only models_gateway
	require.NoError(t, os.WriteFile(configFile, []byte("models_gateway: https://my-gateway.example.com\n"), 0o644))

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.Equal(t, "https://my-gateway.example.com", config.ModelsGateway)
	assert.Empty(t, config.Aliases)
}

func TestConfig_Version(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config without version
	config := &Config{
		Aliases: map[string]*Alias{
			"test": {Path: "agentcatalog/test-agent"},
		},
	}

	// Save should set version to CurrentVersion
	require.NoError(t, config.saveTo(configFile))
	assert.Equal(t, CurrentVersion, config.Version)

	// Load should read the version
	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, loaded.Version)

	// Verify version is written to file
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "version: v1")
}

func TestConfig_Version_LoadLegacyWithoutVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Create config file without version field (simulates old config)
	require.NoError(t, os.WriteFile(configFile, []byte("aliases:\n  test:\n    path: agentcatalog/test\n"), 0o644))

	// Load should work and version should be empty (not automatically upgraded on read)
	config, err := loadFrom(configFile, "")
	require.NoError(t, err)
	assert.Empty(t, config.Version)
	assert.Equal(t, "agentcatalog/test", config.Aliases["test"].Path)

	// Saving should add the version
	require.NoError(t, config.saveTo(configFile))
	assert.Equal(t, CurrentVersion, config.Version)
}

func TestConfig_Settings_HideToolResults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Settings: &Settings{
			HideToolResults: true,
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.NotNil(t, loaded.Settings)
	assert.True(t, loaded.Settings.HideToolResults)
}

func TestConfig_Settings_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)

	// GetSettings should return an empty Settings struct, not nil
	settings := config.GetSettings()
	assert.NotNil(t, settings)
	assert.False(t, settings.HideToolResults)
}

func TestConfig_Settings_GetSettingsNil(t *testing.T) {
	t.Parallel()

	config := &Config{Aliases: make(map[string]*Alias)}

	// GetSettings should return an empty Settings struct when Settings is nil
	settings := config.GetSettings()
	assert.NotNil(t, settings)
	assert.False(t, settings.HideToolResults)
}

func TestConfig_AliasWithHideToolResults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		Aliases: map[string]*Alias{
			"hidden": {Path: "agentcatalog/coder", HideToolResults: true},
			"full":   {Path: "agentcatalog/coder", Yolo: true, Model: "openai/gpt-4o", HideToolResults: true},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	// Verify hide_tool_results option
	hiddenAlias, ok := loaded.GetAlias("hidden")
	require.True(t, ok)
	assert.Equal(t, "agentcatalog/coder", hiddenAlias.Path)
	assert.True(t, hiddenAlias.HideToolResults)
	assert.False(t, hiddenAlias.Yolo)
	assert.Empty(t, hiddenAlias.Model)

	// Verify all options together
	fullAlias, ok := loaded.GetAlias("full")
	require.True(t, ok)
	assert.True(t, fullAlias.HideToolResults)
	assert.True(t, fullAlias.Yolo)
	assert.Equal(t, "openai/gpt-4o", fullAlias.Model)
}

func TestAlias_HasOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		alias    *Alias
		expected bool
	}{
		{"nil alias", nil, false},
		{"empty alias", &Alias{Path: "test"}, false},
		{"yolo only", &Alias{Path: "test", Yolo: true}, true},
		{"model only", &Alias{Path: "test", Model: "openai/gpt-4o"}, true},
		{"hide_tool_results only", &Alias{Path: "test", HideToolResults: true}, true},
		{"all options", &Alias{Path: "test", Yolo: true, Model: "openai/gpt-4o", HideToolResults: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.alias.HasOptions())
		})
	}
}

func TestConfig_CredentialHelper(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config := &Config{
		CredentialHelper: &CredentialHelper{
			Command: "my-credential-helper",
			Args:    []string{"get-token"},
		},
	}

	require.NoError(t, config.saveTo(configFile))

	loaded, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.NotNil(t, loaded.CredentialHelper)
	assert.Equal(t, "my-credential-helper", loaded.CredentialHelper.Command)
	assert.Equal(t, []string{"get-token"}, loaded.CredentialHelper.Args)
}

func TestConfig_CredentialHelper_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	config, err := loadFrom(configFile, "")
	require.NoError(t, err)

	assert.Nil(t, config.CredentialHelper)
}
