package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV2Commands_AllForms(t *testing.T) {
	cfg, err := LoadConfig("commands_v2.yaml", openRoot(t, "testdata"))
	require.NoError(t, err)
	// map form
	cmdsMap := cfg.Agents["root"].Commands
	require.Equal(t, "check disk", cmdsMap["df"])
	require.Equal(t, "list files", cmdsMap["ls"])
	// list form
	cmdsList := cfg.Agents["another_agent"].Commands
	require.Equal(t, "check disk", cmdsList["df"])
	require.Equal(t, "list files", cmdsList["ls"])
	// none
	require.Empty(t, cfg.Agents["none_agent"].Commands)
}

func TestMigrate_v1_Commands_AllForms(t *testing.T) {
	cfg, err := LoadConfig("commands_v1.yaml", openRoot(t, "testdata"))
	require.NoError(t, err)
	// map form
	cmdsMap := cfg.Agents["root"].Commands
	require.Equal(t, "check disk", cmdsMap["df"])
	require.Equal(t, "list files", cmdsMap["ls"])
	// list form
	cmdsList := cfg.Agents["another_agent"].Commands
	require.Equal(t, "check disk", cmdsList["df"])
	require.Equal(t, "list files", cmdsList["ls"])
	// none
	require.Empty(t, cfg.Agents["yet_another_agent"].Commands)
}

func TestMigrate_v0_Commands_AllForms(t *testing.T) {
	cfg, err := LoadConfig("commands_v0.yaml", openRoot(t, "testdata"))
	require.NoError(t, err)
	// map form
	cmdsMap := cfg.Agents["root"].Commands
	require.Equal(t, "check disk", cmdsMap["df"])
	require.Equal(t, "list files", cmdsMap["ls"])
	// list form
	cmdsList := cfg.Agents["another_agent"].Commands
	require.Equal(t, "check disk", cmdsList["df"])
	require.Equal(t, "list files", cmdsList["ls"])
	// none
	require.Empty(t, cfg.Agents["yet_another_agent"].Commands)
}
