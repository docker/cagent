package runtime

import "sync"

// usageTracker maintains per-session usage metrics for runtime streams.
type usageTracker struct {
	mu   sync.RWMutex
	rows map[string]*usageRow
}

type usageRow struct {
	SessionID       string
	AgentName       string
	ParentSessionID string
	Title           string

	// Provider metadata
	ContextLimit int

	// Usage totals scoped to this session only (excludes child totals).
	InputTokens  int
	OutputTokens int
	Cost         float64

	Active bool
}

func newUsageTracker() *usageTracker {
	return &usageTracker{rows: make(map[string]*usageRow)}
}

// registerSession ensures a row exists for the given session.
func (t *usageTracker) registerSession(sessID, agentName, parentID, title string, contextLimit int) {
	if sessID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	row, ok := t.rows[sessID]
	if !ok {
		row = &usageRow{SessionID: sessID}
		t.rows[sessID] = row
	}

	if agentName != "" {
		row.AgentName = agentName
	}
	if parentID != "" {
		row.ParentSessionID = parentID
	}
	if title != "" {
		row.Title = title
	}
	if contextLimit > 0 {
		row.ContextLimit = contextLimit
	}
}

func (t *usageTracker) addDelta(sessID string, inputDelta, outputDelta int, costDelta float64) {
	if sessID == "" || (inputDelta == 0 && outputDelta == 0 && costDelta == 0) {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	row, ok := t.rows[sessID]
	if !ok {
		row = &usageRow{SessionID: sessID}
		t.rows[sessID] = row
	}

	row.InputTokens += inputDelta
	row.OutputTokens += outputDelta
	row.Cost += costDelta
}

func (t *usageTracker) markActive(sessID string, active bool) {
	if sessID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	row, ok := t.rows[sessID]
	if !ok {
		row = &usageRow{SessionID: sessID}
		t.rows[sessID] = row
	}

	row.Active = active
}

func (t *usageTracker) totals() (input, output int, cost float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, row := range t.rows {
		input += row.InputTokens
		output += row.OutputTokens
		cost += row.Cost
	}
	return
}

func (t *usageTracker) snapshot() []*usageRow {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rows := make([]*usageRow, 0, len(t.rows))
	for _, row := range t.rows {
		copyRow := *row
		rows = append(rows, &copyRow)
	}
	return rows
}
