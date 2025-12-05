package editor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor/completions"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// ansiRegexp matches ANSI escape sequences so they can be removed when
// computing layout measurements.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

const (
	// maxInlinePasteLines is the maximum number of lines for inline paste.
	// Pastes exceeding this are buffered to a temp file attachment.
	maxInlinePasteLines = 5
	// maxInlinePasteChars is the character limit for inline pastes.
	// This catches very long single-line pastes that would clutter the editor.
	maxInlinePasteChars = 500
)

type attachment struct {
	path        string // Path to file (temp for pastes, real for file refs)
	placeholder string // @paste-1 or @filename
	label       string // Display label like "paste-1 (21.1 KB)"
	sizeBytes   int
	isTemp      bool // True for paste temp files that need cleanup
}

// AttachmentPreview describes an attachment and its contents for dialog display.
type AttachmentPreview struct {
	Title   string
	Content string
}

// SendMsg represents a message to send
type SendMsg struct {
	Content        string // Full content sent to the agent (with file contents and pastes expanded)
	DisplayContent string // Compact version for UI display (with @filename and @paste-X placeholders)
}

// Editor represents an input editor component
type Editor interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	SetWorking(working bool) tea.Cmd
	AcceptSuggestion() bool
	Cleanup()
	GetSize() (width, height int)
	BannerHeight() int
	AttachmentAt(x int) (AttachmentPreview, bool)
}

// editor implements [Editor]
type editor struct {
	textarea textarea.Model
	hist     *history.History
	width    int
	height   int
	working  bool
	// completions are the available completions
	completions []completions.Completion

	// completionWord stores the word being completed
	completionWord    string
	currentCompletion completions.Completion

	suggestion    string
	hasSuggestion bool
	cursorHidden  bool
	// userTyped tracks whether the user has manually typed content (vs loaded from history)
	userTyped bool
	// keyboardEnhancementsSupported tracks whether the terminal supports keyboard enhancements
	keyboardEnhancementsSupported bool
	// pendingFileRef tracks the current @word being typed (for manual file ref detection).
	// Only set when cursor is in a word starting with @, cleared when cursor leaves.
	pendingFileRef string
	// banner renders pending attachments so the user can see what's queued.
	banner *attachmentBanner
	// attachments tracks all file attachments (pastes and file refs).
	attachments []attachment
	// pasteCounter tracks the next paste number for display purposes.
	pasteCounter int
}

// New creates a new editor component
func New(a *app.App, hist *history.History) Editor {
	ta := textarea.New()
	ta.SetStyles(styles.InputStyle)
	ta.Placeholder = "Type your message here..."
	ta.Prompt = "│ "
	ta.CharLimit = -1
	ta.SetWidth(50)
	ta.SetHeight(3) // Set minimum 3 lines for multi-line input
	ta.Focus()
	ta.ShowLineNumbers = false

	e := &editor{
		textarea:    ta,
		hist:        hist,
		completions: completions.Completions(a),
		// Default to no keyboard enhancements; ctrl+j will be used until we know otherwise
		keyboardEnhancementsSupported: false,
		banner:                        newAttachmentBanner(),
	}

	// Configure initial keybinding (ctrl+j for legacy terminals)
	e.configureNewlineKeybinding()

	return e
}

// Init initializes the component
func (e *editor) Init() tea.Cmd {
	return textarea.Blink
}

// stripANSI removes ANSI escape sequences from the provided string so width
// calculations can be performed on plain text.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// lineHasContent reports whether the rendered line has user input after the
// prompt has been stripped.
func lineHasContent(line, prompt string) bool {
	plain := stripANSI(line)
	if prompt != "" && strings.HasPrefix(plain, prompt) {
		plain = strings.TrimPrefix(plain, prompt)
	}

	return strings.TrimSpace(plain) != ""
}

// lastInputLine returns the content of the final line from the textarea value,
// which is the portion eligible for suggestions.
func lastInputLine(value string) string {
	if idx := strings.LastIndex(value, "\n"); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

// applySuggestionOverlay draws the inline suggestion on top of the textarea
// view using the configured ghost style.
func (e *editor) applySuggestionOverlay(view string) string {
	lines := strings.Split(view, "\n")
	targetLine := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if lineHasContent(lines[i], e.textarea.Prompt) {
			targetLine = i
			break
		}
	}

	if targetLine == -1 {
		return view
	}

	currentLine := lastInputLine(e.textarea.Value())
	promptWidth := runewidth.StringWidth(stripANSI(e.textarea.Prompt))
	textWidth := runewidth.StringWidth(currentLine)

	ghost := styles.SuggestionGhostStyle.Render(e.suggestion)

	baseLayer := lipgloss.NewLayer(view)
	overlay := lipgloss.NewLayer(ghost).
		X(promptWidth + textWidth).
		Y(targetLine)

	canvas := lipgloss.NewCanvas(baseLayer, overlay)
	return canvas.Render()
}

// refreshSuggestion updates the cached suggestion to reflect the current
// textarea value and available history entries.
func (e *editor) refreshSuggestion() {
	if e.hist == nil {
		e.clearSuggestion()
		return
	}

	current := e.textarea.Value()
	if current == "" {
		e.clearSuggestion()
		return
	}

	match := e.hist.LatestMatch(current)

	if match == "" || match == current || len(match) <= len(current) {
		e.clearSuggestion()
		return
	}

	e.suggestion = match[len(current):]
	if e.suggestion == "" {
		e.clearSuggestion()
		return
	}

	e.hasSuggestion = true
	e.setCursorHidden(true)
}

// clearSuggestion removes any pending suggestion and restores the cursor.
func (e *editor) clearSuggestion() {
	if !e.hasSuggestion && !e.cursorHidden {
		return
	}
	e.hasSuggestion = false
	e.suggestion = ""
	e.setCursorHidden(false)
}

// setCursorHidden toggles the virtual cursor so the ghost suggestion can be
// displayed without visual conflicts.
func (e *editor) setCursorHidden(hidden bool) {
	if e.cursorHidden == hidden {
		return
	}

	e.cursorHidden = hidden
	e.textarea.SetVirtualCursor(!hidden)
}

// AcceptSuggestion applies the current suggestion into the textarea value and
// returns true when a suggestion was committed.
func (e *editor) AcceptSuggestion() bool {
	if !e.hasSuggestion || e.suggestion == "" {
		return false
	}

	current := e.textarea.Value()
	e.textarea.SetValue(current + e.suggestion)
	e.textarea.MoveToEnd()

	e.clearSuggestion()

	return true
}

// configureNewlineKeybinding sets up the appropriate newline keybinding
// based on terminal keyboard enhancement support.
func (e *editor) configureNewlineKeybinding() {
	// Configure textarea's InsertNewline binding based on terminal capabilities
	if e.keyboardEnhancementsSupported {
		// Modern terminals: bind both shift+enter and ctrl+j
		e.textarea.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j")
		e.textarea.KeyMap.InsertNewline.SetEnabled(true)
	} else {
		// Legacy terminals: only ctrl+j works
		e.textarea.KeyMap.InsertNewline.SetKeys("ctrl+j")
		e.textarea.KeyMap.InsertNewline.SetEnabled(true)
	}
}

// Update handles messages and updates the component state
func (e *editor) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	defer e.updateBanner()

	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if e.handlePaste(msg.Content) {
			return e, nil
		}
	case tea.KeyboardEnhancementsMsg:
		// Track keyboard enhancement support and configure newline keybinding accordingly
		e.keyboardEnhancementsSupported = msg.Flags != 0
		e.configureNewlineKeybinding()
		return e, nil
	case tea.WindowSizeMsg:
		e.textarea.SetWidth(msg.Width - 2)
		return e, nil

	// Handle mouse events
	case tea.MouseWheelMsg:
		// Forward mouse wheel as cursor movements to textarea for scrolling
		// This bypasses history navigation and allows viewport scrolling
		switch msg.Button.String() {
		case "wheelup":
			// Move cursor up (scrolls viewport if needed)
			e.textarea.CursorUp()
		case "wheeldown":
			// Move cursor down (scrolls viewport if needed)
			e.textarea.CursorDown()
		}
		return e, nil

	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		var cmd tea.Cmd
		e.textarea, cmd = e.textarea.Update(msg)
		// Give focus to editor on click
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return e, tea.Batch(cmd, e.Focus())
		}
		return e, cmd

	case completion.SelectedMsg:
		currentValue := e.textarea.Value()
		lastIdx := strings.LastIndex(currentValue, e.completionWord)
		if e.currentCompletion.AutoSubmit() {
			if lastIdx >= 0 {
				newValue := currentValue[:lastIdx-1]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
			}
			if msg.Execute != nil {
				return e, msg.Execute()
			}
		} else {
			if lastIdx >= 0 {
				newValue := currentValue[:lastIdx-1] + msg.Value + currentValue[lastIdx+len(e.completionWord):]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
			}
			// Track file references when using @ completion (but not paste placeholders)
			if e.currentCompletion != nil && e.currentCompletion.Trigger() == "@" && !strings.HasPrefix(msg.Value, "@paste-") {
				e.addFileAttachment(msg.Value)
			}
			// Clear history suggestion after selecting a completion
			e.clearSuggestion()
			return e, nil
		}
		return e, nil
	case completion.ClosedMsg:
		e.completionWord = ""
		return e, nil
	case tea.KeyPressMsg:
		if key.Matches(msg, e.textarea.KeyMap.Paste) {
			return e.handleClipboardPaste()
		}

		switch msg.String() {
		// Handle send/newline keys:
		// - Enter: submit current input (if textarea inserted a newline, submit previous buffer).
		// - Shift+Enter: insert newline when keyboard enhancements are supported.
		// - Ctrl+J: fallback to insert '\n' when keyboard enhancements are not supported.

		case "enter", "shift+enter", "ctrl+j":
			if !e.textarea.Focused() {
				return e, nil
			}

			// Let textarea process the key - it handles newlines via InsertNewline binding
			prev := e.textarea.Value()
			e.textarea, _ = e.textarea.Update(msg)
			value := e.textarea.Value()

			// If textarea inserted a newline (shift+enter or ctrl+j), just refresh and return
			if value != prev && msg.String() != "enter" {
				e.refreshSuggestion()
				return e, nil
			}

			// If plain enter and textarea inserted a newline, submit the previous value
			if value != prev && msg.String() == "enter" {
				if prev != "" && !e.working {
					e.tryAddFileRef(e.pendingFileRef) // Add any pending @filepath before send
					e.pendingFileRef = ""
					displayContent := prev
					sendContent := e.expandAllAttachments(prev)
					e.textarea.SetValue(prev)
					e.textarea.MoveToEnd()
					e.textarea.Reset()
					e.userTyped = false
					e.refreshSuggestion()
					return e, core.CmdHandler(SendMsg{Content: sendContent, DisplayContent: displayContent})
				}
				return e, nil
			}

			// Normal enter submit: send current value
			if value != "" && !e.working {
				slog.Debug(value)
				e.tryAddFileRef(e.pendingFileRef) // Add any pending @filepath before send
				e.pendingFileRef = ""
				displayContent := value
				sendContent := e.expandAllAttachments(value)
				e.textarea.Reset()
				e.userTyped = false
				e.refreshSuggestion()
				return e, core.CmdHandler(SendMsg{Content: sendContent, DisplayContent: displayContent})
			}

			return e, nil
		case "ctrl+c":
			return e, tea.Quit
		case "up":
			// Only navigate history if the user hasn't manually typed content
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Previous())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
			// Otherwise, let the textarea handle cursor navigation
		case "down":
			// Only navigate history if the user hasn't manually typed content
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Next())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
			// Otherwise, let the textarea handle cursor navigation
		default:
			for _, completion := range e.completions {
				if msg.String() == completion.Trigger() {
					if completion.RequiresEmptyEditor() && e.textarea.Value() != "" {
						continue
					}
					cmds = append(cmds, e.startCompletion(completion))
				}
			}
		}
	}

	prevValue := e.textarea.Value()
	var cmd tea.Cmd
	e.textarea, cmd = e.textarea.Update(msg)
	cmds = append(cmds, cmd)

	// If the value changed due to user input (not history navigation), mark as user typed
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		// Check if content changed and it wasn't a history navigation key
		if e.textarea.Value() != prevValue && keyMsg.String() != "up" && keyMsg.String() != "down" {
			e.userTyped = true
		}

		// Also check if textarea became empty - reset userTyped flag
		if e.textarea.Value() == "" {
			e.userTyped = false
		}

		currentWord := e.textarea.Word()

		// Track manual @filepath refs - only runs when we're in/leaving an @ word
		if e.pendingFileRef != "" && currentWord != e.pendingFileRef {
			// Left the @ word - try to add it as file ref
			e.tryAddFileRef(e.pendingFileRef)
			e.pendingFileRef = ""
		}
		if e.pendingFileRef == "" && strings.HasPrefix(currentWord, "@") && len(currentWord) > 1 {
			// Entered an @ word - start tracking
			e.pendingFileRef = currentWord
		} else if e.pendingFileRef != "" && strings.HasPrefix(currentWord, "@") {
			// Still in @ word but it changed (user typing more) - update tracking
			e.pendingFileRef = currentWord
		}

		if keyMsg.String() == "space" {
			e.completionWord = ""
			e.currentCompletion = nil
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}

		if e.currentCompletion != nil && strings.HasPrefix(currentWord, e.currentCompletion.Trigger()) {
			e.completionWord = strings.TrimPrefix(currentWord, e.currentCompletion.Trigger())
			cmds = append(cmds, core.CmdHandler(completion.QueryMsg{Query: e.completionWord}))
		} else {
			e.completionWord = ""
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}
	}

	e.refreshSuggestion()

	return e, tea.Batch(cmds...)
}

func (e *editor) handleClipboardPaste() (layout.Model, tea.Cmd) {
	content, err := clipboard.ReadAll()
	if err != nil {
		slog.Warn("failed to read clipboard", "error", err)
		return e, nil
	}

	// handlePaste returns true if content was buffered to disk (large paste),
	// false if it's small enough for inline insertion.
	if !e.handlePaste(content) {
		e.textarea.InsertString(content)
	}
	return e, textarea.Blink
}

func (e *editor) startCompletion(c completions.Completion) tea.Cmd {
	e.currentCompletion = c
	items := c.Items()

	// Prepend paste placeholders for @ trigger so users can easily reference them
	if c.Trigger() == "@" {
		pasteItems := e.getPasteCompletionItems()
		if len(pasteItems) > 0 {
			items = append(pasteItems, items...)
		}
	}

	return core.CmdHandler(completion.OpenMsg{
		Items: items,
	})
}

// getPasteCompletionItems returns completion items for paste attachments only.
func (e *editor) getPasteCompletionItems() []completion.Item {
	var items []completion.Item
	for _, att := range e.attachments {
		if !att.isTemp {
			continue // Only show pastes, not file refs
		}
		name := strings.TrimPrefix(att.placeholder, "@")
		items = append(items, completion.Item{
			Label:       name,
			Description: formatBytes(att.sizeBytes),
			Value:       att.placeholder,
			Pinned:      true,
		})
	}
	return items
}

// View renders the component
func (e *editor) View() string {
	view := e.textarea.View()

	if e.hasSuggestion && e.suggestion != "" {
		view = e.applySuggestionOverlay(view)
	}

	bannerView := e.banner.View()
	if bannerView != "" {
		// Banner is shown - no extra top padding needed
		view = lipgloss.JoinVertical(lipgloss.Left, bannerView, view)
	}

	return styles.EditorStyle.Render(view)
}

// SetSize sets the dimensions of the component
func (e *editor) SetSize(width, height int) tea.Cmd {
	e.width = width
	e.height = max(height, 1)

	e.textarea.SetWidth(max(width, 10))
	e.updateTextareaHeight()

	return nil
}

func (e *editor) updateTextareaHeight() {
	available := e.height
	if e.banner != nil {
		available -= e.banner.Height()
	}

	if available < 1 {
		available = 1
	}

	e.textarea.SetHeight(available)
}

// BannerHeight returns the current height of the attachment banner (0 if hidden)
func (e *editor) BannerHeight() int {
	if e.banner == nil {
		return 0
	}
	return e.banner.Height()
}

// GetSize returns the total dimensions allocated for the editor (banner + textarea).
func (e *editor) GetSize() (width, height int) {
	return e.width, e.height
}

// AttachmentAt returns preview information for the attachment rendered at the given X position.
func (e *editor) AttachmentAt(x int) (AttachmentPreview, bool) {
	if e.banner == nil || e.banner.Height() == 0 {
		return AttachmentPreview{}, false
	}

	item, ok := e.banner.HitTest(x)
	if !ok {
		return AttachmentPreview{}, false
	}

	for _, att := range e.attachments {
		if att.placeholder != item.placeholder {
			continue
		}

		data, err := os.ReadFile(att.path)
		if err != nil {
			slog.Warn("failed to read attachment preview", "path", att.path, "error", err)
			return AttachmentPreview{}, false
		}

		return AttachmentPreview{
			Title:   item.label,
			Content: string(data),
		}, true
	}

	return AttachmentPreview{}, false
}

// Focus gives focus to the component
func (e *editor) Focus() tea.Cmd {
	return e.textarea.Focus()
}

// Blur removes focus from the component
func (e *editor) Blur() tea.Cmd {
	e.textarea.Blur()
	return nil
}

func (e *editor) SetWorking(working bool) tea.Cmd {
	e.working = working
	return nil
}

// tryAddFileRef checks if word is a valid @filepath and adds it as attachment.
// Called when cursor leaves a word to detect manually-typed file references.
func (e *editor) tryAddFileRef(word string) {
	// Must start with @ and look like a path (contains / or .)
	if !strings.HasPrefix(word, "@") || len(word) < 2 {
		return
	}

	// Don't track paste placeholders as file refs
	if strings.HasPrefix(word, "@paste-") {
		return
	}

	path := word[1:] // strip @
	if !strings.ContainsAny(path, "/.") {
		return // not a path-like reference (e.g., @username)
	}

	e.addFileAttachment(word)
}

// addFileAttachment adds a file reference as an attachment if valid.
func (e *editor) addFileAttachment(placeholder string) {
	path := strings.TrimPrefix(placeholder, "@")

	// Check if it's an existing file (not directory)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}

	// Avoid duplicates
	for _, att := range e.attachments {
		if att.placeholder == placeholder {
			return
		}
	}

	e.attachments = append(e.attachments, attachment{
		path:        path,
		placeholder: placeholder,
		label:       fmt.Sprintf("%s (%s)", filepath.Base(path), formatBytes(int(info.Size()))),
		sizeBytes:   int(info.Size()),
		isTemp:      false,
	})
}

// expandAllAttachments collects attachments, keeping placeholders in the message
// and appending contents in an <attachments> section.
func (e *editor) expandAllAttachments(content string) string {
	var expanded []string

	for _, att := range e.attachments {
		if !strings.Contains(content, att.placeholder) {
			if att.isTemp {
				_ = os.Remove(att.path)
			}
			continue
		}

		data, err := os.ReadFile(att.path)
		if err != nil {
			slog.Warn("failed to read attachment", "path", att.path, "error", err)
			if att.isTemp {
				_ = os.Remove(att.path)
			}
			continue
		}

		expanded = append(expanded, fmt.Sprintf("<%s>\n%s\n</%s>",
			att.placeholder, string(data), att.placeholder))

		if att.isTemp {
			_ = os.Remove(att.path)
		}
	}
	e.attachments = nil

	if len(expanded) > 0 {
		content += "\n\n<attachments>\n" + strings.Join(expanded, "\n\n") + "\n</attachments>"
	}

	return content
}

// Cleanup removes any temporary paste files that haven't been sent yet.
func (e *editor) Cleanup() {
	for _, att := range e.attachments {
		if att.isTemp {
			_ = os.Remove(att.path)
		}
	}
	e.attachments = nil
}

func (e *editor) handlePaste(content string) bool {
	// Count lines (newlines + 1 for content without trailing newline)
	lines := strings.Count(content, "\n") + 1
	if strings.HasSuffix(content, "\n") {
		lines-- // Don't count trailing newline as extra line
	}

	// Allow inline if within both limits
	if lines <= maxInlinePasteLines && len(content) <= maxInlinePasteChars {
		return false
	}

	e.pasteCounter++
	att, err := createPasteAttachment(content, e.pasteCounter)
	if err != nil {
		slog.Warn("failed to buffer paste", "error", err)
		// Still return true to prevent the large paste from falling through
		// to textarea.Update(), which would block the UI for seconds.
		return true
	}

	e.textarea.InsertString(att.placeholder)
	e.attachments = append(e.attachments, att)

	return true
}

func (e *editor) updateBanner() {
	if e.banner == nil {
		return
	}

	value := e.textarea.Value()
	var items []bannerItem

	for _, att := range e.attachments {
		if strings.Contains(value, att.placeholder) {
			items = append(items, bannerItem{
				label:       att.label,
				placeholder: att.placeholder,
			})
		}
	}

	e.banner.SetItems(items)
	e.updateTextareaHeight()
}

func createPasteAttachment(content string, num int) (attachment, error) {
	pasteDir := filepath.Join(paths.GetDataDir(), "pastes")
	if err := os.MkdirAll(pasteDir, 0o755); err != nil {
		return attachment{}, fmt.Errorf("create paste dir: %w", err)
	}

	file, err := os.CreateTemp(pasteDir, "paste-*.txt")
	if err != nil {
		return attachment{}, fmt.Errorf("create paste file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return attachment{}, fmt.Errorf("write paste file: %w", err)
	}

	displayName := fmt.Sprintf("paste-%d", num)
	return attachment{
		path:        file.Name(),
		placeholder: "@" + displayName,
		label:       fmt.Sprintf("%s (%s)", displayName, formatBytes(len(content))),
		sizeBytes:   len(content),
		isTemp:      true,
	}, nil
}

func formatBytes(n int) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)

	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
