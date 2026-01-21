package spinner

import (
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

type Mode int

const (
	ModeBoth Mode = iota
	ModeSpinnerOnly
)

var lastID atomic.Int64

type tickMsg struct {
	tag int
	id  int
}

type Spinner struct {
	dotsStyle           lipgloss.Style
	styledSpinnerFrames []string // pre-rendered spinner frames
	mode                Mode
	currentMessage      string
	lightPosition       int
	frame               int
	id                  int
	tag                 int
	direction           int // 1 for forward, -1 for backward
	pauseFrames         int
}

// Default messages for the spinner
var defaultMessages = []string{
	"Working",
	"Reticulating splines",
	"Computing",
	"Thinking",
	"Processing",
	"Analyzing",
	"Calibrating",
	"Initializing",
	"Generating",
	"Evaluating",
	"Synthesizing",
	"Optimizing",
	"Consulting the oracle",
	"Summoning electrons",
	"Warming up the flux capacitor",
	"Reversing the polarity",
	"Spinning up the hamster wheels",
	"Herding cats",
	"Untangling yarn",
	"Aligning the cosmos",
	"Brewing digital coffee",
	"Wrangling bits and bytes",
	"Charging the crystals",
	"Consulting the rubber duck",
	"Feeding the gremlins",
	"Polishing the pixels",
	"Calibrating the thrusters",
}

func New(mode Mode, dotsStyle lipgloss.Style) Spinner {
	// Pre-render all spinner frames for fast lookup during render
	styledFrames := make([]string, len(spinnerChars))
	for i, char := range spinnerChars {
		styledFrames[i] = dotsStyle.Render(char)
	}

	return Spinner{
		dotsStyle:           dotsStyle,
		styledSpinnerFrames: styledFrames,
		mode:                mode,
		currentMessage:      defaultMessages[rand.IntN(len(defaultMessages))],
		lightPosition:       -3,
		id:                  int(lastID.Add(1)),
		direction:           1,
	}
}

func (s Spinner) Reset() Spinner {
	return New(s.mode, s.dotsStyle)
}

func (s Spinner) Update(message tea.Msg) (layout.Model, tea.Cmd) {
	msg, ok := message.(tickMsg)
	if !ok || (msg.id > 0 && msg.id != s.id) || (msg.tag > 0 && msg.tag != s.tag) {
		return s, nil
	}

	s.tag++
	s.frame++

	// Light animation only needed for ModeBoth
	if s.mode == ModeBoth {
		if s.pauseFrames > 0 {
			s.pauseFrames--
			if s.pauseFrames == 0 {
				s.direction = -1
			}
		} else {
			s.lightPosition += s.direction
			if s.direction == 1 && s.lightPosition > len([]rune(s.currentMessage))+2 {
				s.pauseFrames = 6
			} else if s.direction == -1 && s.lightPosition < -3 {
				s.direction = 1
			}
		}
	}

	return s, s.Tick()
}

func (s Spinner) View() string {
	spinner := s.styledSpinnerFrames[s.frame%len(s.styledSpinnerFrames)]
	if s.mode == ModeSpinnerOnly {
		return spinner
	}
	return spinner + " " + s.renderMessage()
}

func (s Spinner) SetSize(_, _ int) tea.Cmd { return nil }
func (s Spinner) Init() tea.Cmd            { return s.Tick() }
func (s Spinner) Tick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{id: s.id, tag: s.tag}
	})
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// lightStyles maps distance from light position to style (0=brightest, 1=bright, 2=dim, 3+=dimmest).
var lightStyles = []lipgloss.Style{
	styles.SpinnerTextBrightestStyle,
	styles.SpinnerTextBrightStyle,
	styles.SpinnerTextDimStyle,
	styles.SpinnerTextDimmestStyle,
}

func (s Spinner) renderMessage() string {
	var out strings.Builder
	for i, char := range s.currentMessage {
		dist := min(max(i-s.lightPosition, s.lightPosition-i), len(lightStyles)-1)
		out.WriteString(lightStyles[dist].Render(string(char)))
	}
	return out.String()
}
