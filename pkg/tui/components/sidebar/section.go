package sidebar

import "strings"

// Section represents a self-contained sidebar section that can render itself
// and handle click detection within its bounds.
type Section interface {
	// Render returns the rendered content and the number of lines it occupies.
	Render(contentWidth int) (content string, lineCount int)

	// HandleClick checks if a click at the given line (relative to section start)
	// should be handled. Returns a ClickResult if handled, nil otherwise.
	HandleClick(lineInSection int) *ClickResult
}

// SectionRenderer helps render multiple sections and track their positions.
type SectionRenderer struct {
	lines       []string
	sections    []sectionInfo
	currentLine int
}

type sectionInfo struct {
	section   Section
	startLine int
	lineCount int
}

// NewSectionRenderer creates a new section renderer.
func NewSectionRenderer() *SectionRenderer {
	return &SectionRenderer{}
}

// AddSection renders a section and tracks its position.
func (r *SectionRenderer) AddSection(section Section, contentWidth int) {
	content, lineCount := section.Render(contentWidth)
	if lineCount == 0 {
		return
	}

	r.sections = append(r.sections, sectionInfo{
		section:   section,
		startLine: r.currentLine,
		lineCount: lineCount,
	})

	// Split content into lines and append
	if content != "" {
		r.lines = append(r.lines, strings.Split(content, "\n")...)
	}
	r.currentLine += lineCount
}

// AddContent adds raw content without section tracking (for legacy sections).
func (r *SectionRenderer) AddContent(content string) {
	if content == "" {
		return
	}
	lines := strings.Split(content, "\n")
	r.lines = append(r.lines, lines...)
	r.currentLine += len(lines)
}

// GetLines returns all rendered lines.
func (r *SectionRenderer) GetLines() []string {
	return r.lines
}

// HandleClick finds which section was clicked and delegates to it.
func (r *SectionRenderer) HandleClick(contentY int) *ClickResult {
	for _, info := range r.sections {
		if contentY >= info.startLine && contentY < info.startLine+info.lineCount {
			lineInSection := contentY - info.startLine
			return info.section.HandleClick(lineInSection)
		}
	}
	return nil
}
