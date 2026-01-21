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
	dotsStyle      lipgloss.Style
	messages       []string
	mode           Mode
	currentMessage string
	lightPosition  int
	frame          int
	id             int
	tag            int
	direction      int // 1 for forward, -1 for backward
	pauseFrames    int
	active         bool
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

func (s Spinner) Init() tea.Cmd {
	s.active = true
	return s.Tick()
}

func (s Spinner) Reset() Spinner {
	return New(s.mode, s.dotsStyle)
}

// Spinner updates are strictly scoped to their own tick messages.
// ID and tag checks ensure outdated or foreign ticks are ignored,
// preventing runaway update loops and stale updates after model replacement.
func (s Spinner) Update(message tea.Msg) (layout.Model, tea.Cmd) {
	// If spinner is inactive, ignore all updates and stop ticking.
	if !s.active {
		return s, nil
	}

	msg, ok := message.(tickMsg)
	if !ok {
		return s, nil
	}

	// Ignore ticks from other spinner instances.
	if msg.ID > 0 && msg.ID != s.id {
		return s, nil
	}

	// Ignore out-of-order or stale ticks.
	if msg.tag > 0 && msg.tag != s.tag {
		return s, nil
	}

	s.tag++
	s.frame++

	if s.pauseFrames > 0 {
		s.pauseFrames--
		if s.pauseFrames == 0 {
			s.direction = -1
		}
	} else {
		s.lightPosition += s.direction

		// Use rune count for proper Unicode character handling
		// when animating the highlight across the message.
		messageRuneCount := len([]rune(s.currentMessage))
		if s.direction == 1 && s.lightPosition > messageRuneCount+2 {
			s.pauseFrames = 6
		}

		if s.direction == -1 && s.lightPosition < -3 {
			s.direction = 1
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

// Tick schedules a periodic spinner update while the spinner is active.
// Returning nil when inactive prevents unnecessary wakeups and redraws.
// Bubble Tea automatically cancels pending ticks when the model
// is replaced, so this does not leak goroutines.
func (s Spinner) Tick() tea.Cmd {
	if !s.active {
		return nil
	}

	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{
			Time: t,
			ID:   s.id,
			tag:  s.tag,
		}
	})
}

// SetActive explicitly enables or disables spinner animation.
// When inactive, no new ticks are scheduled and updates are ignored.
func (s *Spinner) SetActive(active bool) {
	s.active = active
}

// render is called frequently while the spinner is active.
// The work here is intentionally lightweight (simple rune iteration),
// and upstream throttling limits how often this is invoked during streaming.
func (s Spinner) render() string {
	message := s.currentMessage
	output := make([]rune, 0, len(message))

	for i, char := range message {
		distance := abs(i - s.lightPosition)

		var style lipgloss.Style
		switch distance {
		case 0:
			style = styles.SpinnerTextBrightestStyle
		case 1:
			style = styles.SpinnerTextBrightStyle
		case 2:
			style = styles.SpinnerTextDimStyle
		default:
			style = styles.SpinnerTextDimmestStyle
		}

		output = append(output, []rune(style.Render(string(char)))...)
	}

	spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerChar := spinnerChars[s.frame%len(spinnerChars)]
	spinnerStyled := s.dotsStyle.Render(spinnerChar)

	switch s.mode {
	case ModeSpinnerOnly:
		return spinnerStyled
	case ModeMessageOnly:
		return string(output)
	}

	return spinnerStyled + " " + string(output)
}

func (s *Spinner) Render() string {
	return s.render()
}

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
