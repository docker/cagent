// Package history persists the user's command/message history in an
// append-only file and provides cursor-based navigation and search over it.
package history

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/portcullis"

	"github.com/docker/docker-agent/pkg/paths"
)

// History is the in-memory view of a persistent message history. The cursor
// (used by [History.Previous] and [History.Next]) stays in [0, len(Messages)],
// where len(Messages) means "past the most recent entry".
type History struct {
	Messages []string

	path    string
	current int
}

// New loads history from paths.GetDataDir()/history when baseDir is empty.
// An explicit baseDir uses baseDir/.cagent/history instead.
func New(baseDir string) (*History, error) {
	dir := baseDir
	if dir == "" {
		dir = paths.GetDataDir()
	} else {
		dir = filepath.Join(baseDir, ".cagent") //rubocop:disable Lint/StatePathViaPathsPackage // preserve the caller-supplied root layout
	}
	h := &History{path: filepath.Join(dir, "history")}
	if err := h.migrateOldHistory(dir); err != nil {
		return nil, err
	}
	if err := h.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	h.current = len(h.Messages)
	return h, nil
}

// NewAtDir loads the history stored at dir/history, without the ".cagent"
// path segment New inserts. It is for embedders that keep the agent's
// state under their own directory layout and must not mix prompt history
// with a docker-agent installation on the same machine.
func NewAtDir(dir string) (*History, error) {
	h := &History{path: filepath.Join(dir, "history")}
	if err := h.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	h.current = len(h.Messages)
	return h, nil
}

// Add records a new message. Any prior occurrence of the same message is
// removed and the new one becomes the most recent entry. The message is
// scrubbed of secret material via [portcullis.Redact] before being stored
// in memory or written to disk so secrets pasted into the prompt never
// linger in the persistent history.
func (h *History) Add(message string) error {
	message = portcullis.Redact(message)
	h.addInMemory(message)
	h.current = len(h.Messages)
	return h.append(message)
}

// Previous moves the cursor one step toward older entries and returns the
// entry under it. At the oldest entry, the cursor stays put.
func (h *History) Previous() string {
	if len(h.Messages) == 0 {
		return ""
	}
	h.current = max(h.current-1, 0)
	return h.Messages[h.current]
}

// Next moves the cursor one step toward newer entries and returns the entry
// under it. Past the most recent entry, returns an empty string.
func (h *History) Next() string {
	if h.current >= len(h.Messages)-1 {
		h.current = len(h.Messages)
		return ""
	}
	h.current++
	return h.Messages[h.current]
}

// SetCurrent positions the cursor at index i, clamped to [0, len(Messages)].
// Keeping the cursor in this range guarantees that subsequent Previous and
// Next calls never index out of bounds.
func (h *History) SetCurrent(i int) {
	h.current = max(0, min(i, len(h.Messages)))
}

// LatestMatch returns the most recent entry that strictly extends prefix, or
// an empty string when none does.
func (h *History) LatestMatch(prefix string) string {
	for _, msg := range slices.Backward(h.Messages) {
		if strings.HasPrefix(msg, prefix) && len(msg) > len(prefix) {
			return msg
		}
	}
	return ""
}

// FindPrevContains searches backward from index from-1 for an entry containing
// query (case-insensitive). An empty query matches any entry. Pass
// len(Messages) to start from the most recent entry.
func (h *History) FindPrevContains(query string, from int) (msg string, idx int, ok bool) {
	query = strings.ToLower(query)
	for i := min(from-1, len(h.Messages)-1); i >= 0; i-- {
		if query == "" || strings.Contains(strings.ToLower(h.Messages[i]), query) {
			return h.Messages[i], i, true
		}
	}
	return "", -1, false
}

// FindNextContains searches forward from index from+1 for an entry containing
// query (case-insensitive). An empty query matches any entry. Pass -1 to
// start from the oldest entry.
func (h *History) FindNextContains(query string, from int) (msg string, idx int, ok bool) {
	query = strings.ToLower(query)
	for i := max(from+1, 0); i < len(h.Messages); i++ {
		if query == "" || strings.Contains(strings.ToLower(h.Messages[i]), query) {
			return h.Messages[i], i, true
		}
	}
	return "", -1, false
}

// addInMemory removes any prior occurrence of message and appends it as the
// most recent entry.
func (h *History) addInMemory(message string) {
	h.Messages = slices.DeleteFunc(h.Messages, func(m string) bool {
		return m == message
	})
	h.Messages = append(h.Messages, message)
}

// append writes message to the persistent history file as one JSON-encoded
// line.
func (h *History) append(message string) error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(encoded, '\n'))
	return err
}

// load reads the persistent history file and populates Messages, deduplicating
// entries while keeping the latest occurrence of each.
func (h *History) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}

	seenRaw := make(map[string]struct{})
	seenRedacted := make(map[string]struct{})
	// Walk newest-first so duplicates need neither rescanning nor removal.
	for _, line := range slices.Backward(bytes.Split(data, []byte("\n"))) {
		if len(line) == 0 {
			continue
		}
		var msg string
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if _, ok := seenRaw[msg]; ok {
			continue
		}
		seenRaw[msg] = struct{}{}
		// Distinct raw messages can become duplicates after redaction.
		msg = portcullis.Redact(msg)
		if _, ok := seenRedacted[msg]; ok {
			continue
		}
		seenRedacted[msg] = struct{}{}
		h.Messages = append(h.Messages, msg)
	}
	slices.Reverse(h.Messages)
	return nil
}

// migrateOldHistory imports messages from the legacy history.json file (if it
// exists) into the new line-oriented format and removes the old file.
func (h *History) migrateOldHistory(dir string) error {
	oldPath := filepath.Join(dir, "history.json")

	data, err := os.ReadFile(oldPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var old struct {
		Messages []string `json:"messages"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}

	for _, msg := range old.Messages {
		if err := h.append(portcullis.Redact(msg)); err != nil {
			return err
		}
	}
	return os.Remove(oldPath)
}
