package aliases

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliases_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	aliasesFile := filepath.Join(tmpDir, "aliases.yaml")

	s, err := loadFrom(aliasesFile)

	require.NoError(t, err)
	assert.Empty(t, s)
}

func TestAliases_SetGet(t *testing.T) {
	tmpDir := t.TempDir()
	aliasesFile := filepath.Join(tmpDir, "aliases.yaml")

	s, err := loadFrom(aliasesFile)
	require.NoError(t, err)
	assert.Empty(t, s)

	s.Set("test", "agentcatalog/test-agent")

	value, ok := s.Get("test")
	assert.True(t, ok)
	assert.Equal(t, "agentcatalog/test-agent", value)
}

func TestAliases_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	aliasesFile := filepath.Join(tmpDir, "aliases.yaml")

	s := &Aliases{
		"code":    "agentcatalog/notion-expert",
		"myagent": "/path/to/myagent.yaml",
	}

	err := s.saveTo(aliasesFile)
	require.NoError(t, err)

	loaded, err := loadFrom(aliasesFile)
	require.NoError(t, err)

	assert.Len(t, *loaded, 2)
	assert.Equal(t, "agentcatalog/notion-expert", (*loaded)["code"])
	assert.Equal(t, "/path/to/myagent.yaml", (*loaded)["myagent"])
}

func TestAliases_Delete(t *testing.T) {
	s := &Aliases{
		"code":    "agentcatalog/notion-expert",
		"myagent": "/path/to/myagent.yaml",
	}

	deleted := s.Delete("code")
	assert.True(t, deleted)

	_, ok := s.Get("code")
	assert.False(t, ok)
	assert.Len(t, *s, 1)

	deleted = s.Delete("nonexistent")
	assert.False(t, deleted)
}

func TestAliases_List(t *testing.T) {
	s := &Aliases{
		"code":    "agentcatalog/notion-expert",
		"myagent": "/path/to/myagent.yaml",
	}

	list := s.List()
	assert.Len(t, list, 2)
	assert.Equal(t, "agentcatalog/notion-expert", list["code"])
}

func TestAliases_GetUnknown(t *testing.T) {
	s := &Aliases{
		"code": "agentcatalog/notion-expert",
	}

	_, ok := s.Get("nonexistent")
	assert.False(t, ok)
}
