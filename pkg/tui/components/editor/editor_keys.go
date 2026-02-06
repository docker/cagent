package editor

import (
	"os"
	"strings"
)

// KeyBinding represents a single key or key combination
// (e.g. "enter", "ctrl+o", "shift+enter").
type KeyBinding string

// EditorKeyBindings defines the configurable keybindings used by the editor.
type EditorKeyBindings struct {
	// SendKey is the primary key used to SEND the message.
	SendKey KeyBinding

	// NewlineKeys are keys that INSERT a newline instead of sending.
	// Multiple keys are allowed to provide user-friendly fallbacks.
	NewlineKeys []KeyBinding
}

const (
	// Sensible defaults with minimal conflicts.
	DefaultSendKey     = "enter"
	DefaultNewlineKeys = "ctrl+j"

	// Environment variables for quick overrides and testing.
	EnvSendKey    = "CAGENT_SEND_KEY"
	EnvNewlineKey = "CAGENT_NEWLINE_KEY"
)

// LoadEditorKeyBindings loads the editor keybindings with the following priority:
// 1. Environment variables (comma-separated for multiple newline keys)
// 2. Defaults
func LoadEditorKeyBindings() EditorKeyBindings {
	b := EditorKeyBindings{
		SendKey:     DefaultSendKey,
		NewlineKeys: parseKeyList(DefaultNewlineKeys),
	}

	// Override send key (single key for now, but can evolve in the future).
	if val := readEnvKey(EnvSendKey); isValidKey(val) {
		b.SendKey = KeyBinding(val)
	}

	// Override newline keys (supports multiple comma-separated values).
	if val := readEnvKey(EnvNewlineKey); val != "" {
		keys := strings.Split(val, ",")
		parsed := parseKeyList(keys...)
		if len(parsed) > 0 {
			b.NewlineKeys = parsed
		}
	}

	return b
}

// Helpers

func readEnvKey(name string) string {
	if v, ok := os.LookupEnv(name); ok {
		return normalizeKey(v)
	}
	return ""
}

func normalizeKey(k string) string {
	k = strings.TrimSpace(k)
	k = strings.ToLower(k)

	// Normalize common user input variations.
	k = strings.ReplaceAll(k, " ", "+")
	k = strings.ReplaceAll(k, "control+", "ctrl+")
	k = strings.ReplaceAll(k, "return", "enter")

	return k
}

func isValidKey(k string) bool {
	if k == "" {
		return false
	}

	// Block dangerous keys that may exit or break the TUI.
	forbidden := map[string]bool{
		"ctrl+c": true,
		"ctrl+d": true,
		"ctrl+z": true,
		"esc":    true,
		"q":      true, // may trigger quit in some modes
	}
	if forbidden[k] {
		return false
	}

	// Accept only plausible key formats.
	return strings.Contains(k, "enter") ||
		strings.HasPrefix(k, "ctrl+") ||
		strings.HasPrefix(k, "alt+") ||
		strings.HasPrefix(k, "shift+")
}

func parseKeyList(keys ...string) []KeyBinding {
	var result []KeyBinding
	seen := make(map[string]struct{})

	for _, raw := range keys {
		norm := normalizeKey(raw)
		if norm == "" || !isValidKey(norm) {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}

		seen[norm] = struct{}{}
		result = append(result, KeyBinding(norm))
	}

	return result
}

// HelpString returns a human-readable description of the active keybindings.
// Intended for runtime help overlays, status bars or `?` help screens.
func (b EditorKeyBindings) HelpString() string {
	var sb strings.Builder

	sb.WriteString("Key bindings:\n")
	sb.WriteString("  Send message: ")
	sb.WriteString(string(b.SendKey))
	sb.WriteString("\n")

	if len(b.NewlineKeys) > 0 {
		sb.WriteString("  Insert newline: ")
		for i, k := range b.NewlineKeys {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(string(k))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
