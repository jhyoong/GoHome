package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 80 * time.Millisecond

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

type spinnerTickMsg struct{}

type SpinnerComponent struct {
	frame   int
	active  bool
	message string
}

func NewSpinner() *SpinnerComponent {
	return &SpinnerComponent{}
}

func (s *SpinnerComponent) Start(message string) {
	s.active = true
	s.message = message
	s.frame = 0
}

func (s *SpinnerComponent) Stop() {
	s.active = false
}

func (s *SpinnerComponent) SetMessage(msg string) {
	s.message = msg
}

func (s *SpinnerComponent) Tick() {
	s.frame = (s.frame + 1) % len(spinnerFrames)
}

func (s *SpinnerComponent) Active() bool {
	return s.active
}

func (s *SpinnerComponent) Render(width int) []string {
	if !s.active {
		return nil
	}
	frame := spinnerFrames[s.frame%len(spinnerFrames)]
	line := spinnerStyle.Render(frame) + " " + s.message
	return []string{line}
}

func SpinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}
