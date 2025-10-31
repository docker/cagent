package runtime

import (
	"sort"
	"sync"
)

// usageTracker maintains per-session usage metrics for runtime streams.
type usageTracker struct {
	mu              sync.RWMutex         // Guards concurrent access to the tracker.
	rows            map[string]*usageRow // Recorded usage rows keyed by session ID.
	activeSessions  map[string]struct{}  // Set of currently active session IDs.
	maxContextLimit int                  // Highest context limit seen across sessions.
	nextCreateOrder int                  // Incrementing value preserving creation order.
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

	// Monotonic creation order to support stable, user-friendly sorting
	createdOrder int
}

func newUsageTracker() *usageTracker {
	return &usageTracker{
		rows:           make(map[string]*usageRow),
		activeSessions: make(map[string]struct{}),
	}
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
		// Seed a new row so later deltas have somewhere to land.
		row = &usageRow{SessionID: sessID, createdOrder: t.nextCreateOrder}
		t.nextCreateOrder++
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
		if contextLimit > t.maxContextLimit {
			t.maxContextLimit = contextLimit
		}
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
		// Lazily create a row when usage arrives before metadata.
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
		// Ensure unknown sessions still toggle active state safely.
		row = &usageRow{SessionID: sessID}
		t.rows[sessID] = row
	}

	row.Active = active
	if active {
		t.activeSessions[sessID] = struct{}{}
	} else {
		delete(t.activeSessions, sessID)
	}
}

type usageSnapshot struct {
	Rows           []*SessionUsage // Flattened session hierarchy for UI rendering.
	TotalInput     int             // Aggregate input tokens across all sessions.
	TotalOutput    int             // Aggregate output tokens across all sessions.
	TotalCost      float64         // Aggregate cost across all sessions.
	ActiveSessions []string        // Sorted list of active session IDs.
	ContextLimit   int             // Default context limit when none of the rows specify one.
}

func (t *usageTracker) snapshot(defaultContextLimit int) usageSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.rows) == 0 {
		return usageSnapshot{
			ContextLimit: defaultContextLimit,
		}
	}

	// Build adjacency lists so we can walk parent/child relationships deterministically.
	children := make(map[string][]*usageRow, len(t.rows))
	roots := make([]*usageRow, 0, len(t.rows))
	for _, row := range t.rows {
		parentID := row.ParentSessionID
		if parentID == "" || t.rows[parentID] == nil {
			roots = append(roots, row)
			continue
		}
		children[parentID] = append(children[parentID], row)
	}

	sort.SliceStable(roots, func(i, j int) bool {
		return roots[i].createdOrder < roots[j].createdOrder
	})
	for parentID := range children {
		kids := children[parentID]
		sort.SliceStable(kids, func(i, j int) bool {
			return kids[i].createdOrder < kids[j].createdOrder
		})
		children[parentID] = kids
	}

	var (
		result      []*SessionUsage
		totalInput  int
		totalOutput int
		totalCost   float64
		visited     = make(map[string]bool, len(t.rows))
	)

	// Depth-first traversal builds a flattened list while preserving hierarchy depth.
	var traverse func(row *usageRow, depth int)
	traverse = func(row *usageRow, depth int) {
		if row == nil {
			return
		}
		if visited[row.SessionID] {
			return
		}
		visited[row.SessionID] = true

		result = append(result, &SessionUsage{
			SessionID:       row.SessionID,
			AgentName:       row.AgentName,
			Title:           row.Title,
			ParentSessionID: row.ParentSessionID,
			Depth:           depth,
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			Cost:            row.Cost,
			ContextLimit:    row.ContextLimit,
			Active:          row.Active,
		})

		totalInput += row.InputTokens
		totalOutput += row.OutputTokens
		totalCost += row.Cost

		for _, child := range children[row.SessionID] {
			traverse(child, depth+1)
		}
	}

	for _, root := range roots {
		traverse(root, 0)
	}
	for _, row := range t.rows {
		// Include any sessions whose parents vanished (defensive against data races).
		if !visited[row.SessionID] {
			traverse(row, 0)
		}
	}

	active := make([]string, 0, len(t.activeSessions))
	for id := range t.activeSessions {
		active = append(active, id)
	}
	sort.Strings(active) // Sort active sessions for deterministic output.

	// Fall back to the largest known context limit unless the caller overrides it.
	contextLimit := t.maxContextLimit
	if contextLimit == 0 {
		contextLimit = defaultContextLimit
	}

	return usageSnapshot{
		Rows:           result,
		TotalInput:     totalInput,
		TotalOutput:    totalOutput,
		TotalCost:      totalCost,
		ActiveSessions: active,
		ContextLimit:   contextLimit,
	}
}
