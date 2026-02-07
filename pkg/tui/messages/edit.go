package messages

// EditUserMessageMsg requests entering edit mode for a user message.
type EditUserMessageMsg struct {
	MsgIndex        int    // TUI message index (directly usable, no re-computation needed)
	SessionPosition int    // Session position for branching
	OriginalContent string // Original message content
}

// BranchFromEditMsg requests branching from a session position with new content.
type BranchFromEditMsg struct {
	ParentSessionID  string
	BranchAtPosition int
	Content          string
	Attachments      map[string]string
}

// InvalidateStatusBarMsg signals that the statusbar cache should be invalidated.
// This is emitted when bindings change (e.g., entering/exiting inline edit mode).
type InvalidateStatusBarMsg struct{}
