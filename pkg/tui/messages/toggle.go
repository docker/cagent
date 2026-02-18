package messages

// UI toggle messages control various UI state toggles.
type (
	// ToggleYoloMsg toggles YOLO mode (auto-approve tools).
	ToggleYoloMsg struct{}

	// ToggleThinkingMsg toggles extended thinking mode.
	ToggleThinkingMsg struct{}

	// ToggleThinkingResultMsg carries the async result of reasoning support check.
	// If Supported is true, thinking is toggled; otherwise a notification is shown.
	ToggleThinkingResultMsg struct {
		Supported bool
	}

	// ToggleHideToolResultsMsg toggles hiding of tool results.
	ToggleHideToolResultsMsg struct{}

	// ToggleSidebarMsg toggles sidebar visibility.
	// The top-level model also handles this to persist the collapsed state.
	ToggleSidebarMsg struct{}

	// SessionToggleChangedMsg is sent after any session toggle (YOLO, split diff, etc.)
	// changes so that components like the sidebar can invalidate their caches.
	SessionToggleChangedMsg struct{}

	// ShowCostDialogMsg shows the cost/usage dialog.
	ShowCostDialogMsg struct{}

	// ShowPermissionsDialogMsg shows the permissions dialog.
	ShowPermissionsDialogMsg struct{}
)
