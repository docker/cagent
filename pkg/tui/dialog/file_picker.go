package dialog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/docker/go-units"

	"github.com/docker/cagent/pkg/fsx"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

type fileEntry struct {
	name  string
	path  string
	isDir bool
	size  int64
}

type filePickerDialog struct {
	BaseDialog
	textInput  textinput.Model
	currentDir string
	entries    []fileEntry
	filtered   []fileEntry
	selected   int
	offset     int
	keyMap     commandPaletteKeyMap
	err        error
}

// NewFilePickerDialog creates a new file picker dialog for attaching files.
// If initialPath is provided and is a directory, it starts in that directory.
// If initialPath is a file, it starts in the file's directory with the file pre-selected.
func NewFilePickerDialog(initialPath string) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to filter files…"
	ti.Focus()
	ti.CharLimit = 256
	ti.SetWidth(50)

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	startDir := cwd
	var selectFile string

	// Handle initial path if provided
	if initialPath != "" {
		// Make path absolute if relative
		if !filepath.IsAbs(initialPath) {
			initialPath = filepath.Join(cwd, initialPath)
		}

		info, err := os.Stat(initialPath)
		if err == nil {
			if info.IsDir() {
				startDir = initialPath
			} else {
				startDir = filepath.Dir(initialPath)
				selectFile = filepath.Base(initialPath)
			}
		} else {
			// Path doesn't exist, try to use parent directory
			parentDir := filepath.Dir(initialPath)
			if info, err := os.Stat(parentDir); err == nil && info.IsDir() {
				startDir = parentDir
			}
		}
	}

	d := &filePickerDialog{
		textInput:  ti,
		currentDir: startDir,
		keyMap:     defaultCommandPaletteKeyMap(),
	}

	d.loadDirectory()

	// If we have a file to select, find and select it
	if selectFile != "" {
		for i, entry := range d.filtered {
			if entry.name == selectFile {
				d.selected = i
				break
			}
		}
	}

	return d
}

func (d *filePickerDialog) loadDirectory() {
	d.entries = nil
	d.filtered = nil
	d.selected = 0
	d.offset = 0
	d.err = nil

	// Add parent directory entry if not at root
	if d.currentDir != "/" {
		d.entries = append(d.entries, fileEntry{
			name:  "..",
			path:  filepath.Dir(d.currentDir),
			isDir: true,
		})
	}

	// Try to use VCS matcher to filter out ignored files
	var shouldIgnore func(string) bool
	if vcsMatcher, err := fsx.NewVCSMatcher(d.currentDir); err == nil && vcsMatcher != nil {
		shouldIgnore = vcsMatcher.ShouldIgnore
	}

	dirEntries, err := os.ReadDir(d.currentDir)
	if err != nil {
		d.err = err
		return
	}

	// First pass: add directories
	for _, entry := range dirEntries {
		// Skip hidden files/directories
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		fullPath := filepath.Join(d.currentDir, entry.Name())

		// Skip ignored files
		if shouldIgnore != nil && shouldIgnore(fullPath) {
			continue
		}

		if entry.IsDir() {
			d.entries = append(d.entries, fileEntry{
				name:  entry.Name() + "/",
				path:  fullPath,
				isDir: true,
			})
		}
	}

	// Second pass: add all files (not just images)
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		fullPath := filepath.Join(d.currentDir, entry.Name())

		// Skip ignored files
		if shouldIgnore != nil && shouldIgnore(fullPath) {
			continue
		}

		info, err := entry.Info()
		size := int64(0)
		if err == nil {
			size = info.Size()
		}

		d.entries = append(d.entries, fileEntry{
			name:  entry.Name(),
			path:  fullPath,
			isDir: false,
			size:  size,
		})
	}

	d.filtered = d.entries
}

func (d *filePickerDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *filePickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		// Forward paste to text input
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		d.filterEntries()
		return d, cmd

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageUp):
			d.selected -= d.pageSize()
			if d.selected < 0 {
				d.selected = 0
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageDown):
			d.selected += d.pageSize()
			if d.selected >= len(d.filtered) {
				d.selected = max(0, len(d.filtered)-1)
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				entry := d.filtered[d.selected]
				if entry.isDir {
					// Navigate into directory
					d.currentDir = entry.path
					d.textInput.SetValue("")
					d.loadDirectory()
					return d, nil
				}
				// Select file
				return d, tea.Sequence(
					core.CmdHandler(CloseDialogMsg{}),
					core.CmdHandler(messages.InsertFileRefMsg{FilePath: entry.path}),
				)
			}
			return d, nil

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterEntries()
			return d, cmd
		}
	}

	return d, nil
}

func (d *filePickerDialog) filterEntries() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))
	if query == "" {
		d.filtered = d.entries
		d.selected = 0
		d.offset = 0
		return
	}

	d.filtered = nil
	for _, entry := range d.entries {
		// Always include parent directory in filter results
		if entry.name == ".." {
			d.filtered = append(d.filtered, entry)
			continue
		}

		if strings.Contains(strings.ToLower(entry.name), query) {
			d.filtered = append(d.filtered, entry)
		}
	}

	if d.selected >= len(d.filtered) {
		d.selected = 0
	}
	d.offset = 0
}

func (d *filePickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*80/100, 80), 60)
	maxHeight = min(d.Height()*70/100, 30)
	contentWidth = dialogWidth - 6
	return dialogWidth, maxHeight, contentWidth
}

func (d *filePickerDialog) View() string {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()

	d.textInput.SetWidth(contentWidth)

	// Show current directory
	displayDir := d.currentDir
	if len(displayDir) > contentWidth-4 {
		displayDir = "…" + displayDir[len(displayDir)-(contentWidth-5):]
	}
	dirLine := styles.MutedStyle.Render("📁 " + displayDir)

	var entryLines []string
	maxItems := maxHeight - 10

	// Adjust offset to keep selected item visible
	if d.selected < d.offset {
		d.offset = d.selected
	} else if d.selected >= d.offset+maxItems {
		d.offset = d.selected - maxItems + 1
	}

	// Render visible items based on offset
	visibleEnd := min(d.offset+maxItems, len(d.filtered))
	for i := d.offset; i < visibleEnd; i++ {
		entryLines = append(entryLines, d.renderEntry(d.filtered[i], i == d.selected, contentWidth))
	}

	// Show indicator if there are more items
	if visibleEnd < len(d.filtered) {
		entryLines = append(entryLines, styles.MutedStyle.Render(fmt.Sprintf("  … and %d more", len(d.filtered)-visibleEnd)))
	}

	if d.err != nil {
		entryLines = append(entryLines, "", styles.ErrorStyle.
			Align(lipgloss.Center).
			Width(contentWidth).
			Render(d.err.Error()))
	} else if len(d.filtered) == 0 {
		entryLines = append(entryLines, "", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No files found"))
	}

	content := NewContent(contentWidth).
		AddTitle("Attach File").
		AddSpace().
		AddContent(dirLine).
		AddContent(d.textInput.View()).
		AddSeparator().
		AddContent(strings.Join(entryLines, "\n")).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "select", "esc", "close").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *filePickerDialog) pageSize() int {
	_, maxHeight, _ := d.dialogSize()
	return max(1, maxHeight-10)
}

func (d *filePickerDialog) renderEntry(entry fileEntry, selected bool, maxWidth int) string {
	nameStyle, descStyle := styles.PaletteUnselectedActionStyle, styles.PaletteUnselectedDescStyle
	if selected {
		nameStyle, descStyle = styles.PaletteSelectedActionStyle, styles.PaletteSelectedDescStyle
	}

	var icon string
	if entry.isDir {
		icon = "📁 "
	} else {
		icon = "📄 "
	}

	name := entry.name
	maxNameLen := maxWidth - 20
	if len(name) > maxNameLen {
		name = name[:maxNameLen-1] + "…"
	}

	line := nameStyle.Render(icon + name)
	if !entry.isDir && entry.size > 0 {
		line += descStyle.Render(" " + units.HumanSize(float64(entry.size)))
	}

	return line
}

func (d *filePickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
