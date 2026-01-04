package spinner

import (
	"math/rand/v2"
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
	ModeMessageOnly
)

var lastID int64

func nextID() int {
	return int(atomic.AddInt64(&lastID, 1))
}

type tickMsg struct {
	Time time.Time
	tag  int
	ID   int
}

// IsTickMsg returns true if the message is a spinner tick message.
// This is used to allow spinner animations to continue even when dialogs are open.
func IsTickMsg(msg tea.Msg) bool {
	_, ok := msg.(tickMsg)
	return ok
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
	"Dividing by zero",
	"Herding cats",
	"Untangling yarn",
}

func New(mode Mode, dotsStyle lipgloss.Style) Spinner {
	return Spinner{
		dotsStyle:      dotsStyle,
		messages:       defaultMessages,
		mode:           mode,
		currentMessage: defaultMessages[rand.IntN(len(defaultMessages))],
		lightPosition:  -3,
		frame:          0,
		id:             nextID(),
		direction:      1,
		pauseFrames:    0,
	}
}

func (s Spinner) Init() tea.Cmd {
	return s.Tick()
}

func (s Spinner) Reset() Spinner {
	return New(s.mode, s.dotsStyle)
}

func (s Spinner) Update(message tea.Msg) (layout.Model, tea.Cmd) {
	msg, ok := message.(tickMsg)
	if !ok {
		return s, nil
	}

	if msg.ID > 0 && msg.ID != s.id {
		return s, nil
	}
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

		// Use rune count for proper Unicode character handling in light animation
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
	return s.render()
}

func (s Spinner) SetSize(_, _ int) tea.Cmd {
	return nil
}

func (s Spinner) Tick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{
			Time: t,
			ID:   s.id,
			tag:  s.tag,
		}
	})
}

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

func (s *Spinner) SetMessage(message string) {
	s.currentMessage = message
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
