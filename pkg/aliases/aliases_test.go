package aliases

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliases(t *testing.T) {
	tmpDir := t.TempDir()
	aliasesFile := filepath.Join(tmpDir, "aliases.yaml")

	t.Run("Load empty aliases", func(t *testing.T) {
		s, err := LoadFrom(aliasesFile)
		require.NoError(t, err)
		assert.Empty(t, s.Aliases)
	})

	t.Run("Set and Get alias", func(t *testing.T) {
		s, err := LoadFrom(aliasesFile)
		require.NoError(t, err)

		s.Set("test", "agentcatalog/test-agent")

		value, ok := s.Get("test")
		assert.True(t, ok)
		assert.Equal(t, "agentcatalog/test-agent", value)
	})

	t.Run("Save and load aliases", func(t *testing.T) {
		s := &Aliases{
			Aliases: map[string]string{
				"code":    "agentcatalog/notion-expert",
				"myagent": "/path/to/myagent.yaml",
			},
		}

		err := s.SaveTo(aliasesFile)
		require.NoError(t, err)

		loaded, err := LoadFrom(aliasesFile)
		require.NoError(t, err)

		assert.Len(t, loaded.Aliases, 2)
		assert.Equal(t, "agentcatalog/notion-expert", loaded.Aliases["code"])
		assert.Equal(t, "/path/to/myagent.yaml", loaded.Aliases["myagent"])
	})

	t.Run("Delete alias", func(t *testing.T) {
		s := &Aliases{
			Aliases: map[string]string{
				"code":    "agentcatalog/notion-expert",
				"myagent": "/path/to/myagent.yaml",
			},
		}

		deleted := s.Delete("code")
		assert.True(t, deleted)

		_, ok := s.Get("code")
		assert.False(t, ok)

		assert.Len(t, s.Aliases, 1)

		deleted = s.Delete("nonexistent")
		assert.False(t, deleted)
	})

	t.Run("List aliases", func(t *testing.T) {
		s := &Aliases{
			Aliases: map[string]string{
				"code":    "agentcatalog/notion-expert",
				"myagent": "/path/to/myagent.yaml",
			},
		}

		list := s.List()
		assert.Len(t, list, 2)
		assert.Equal(t, "agentcatalog/notion-expert", list["code"])
	})

	t.Run("Get non-existent alias", func(t *testing.T) {
		s := &Aliases{
			Aliases: map[string]string{
				"code": "agentcatalog/notion-expert",
			},
		}

		_, ok := s.Get("nonexistent")
		assert.False(t, ok)
	})
}
