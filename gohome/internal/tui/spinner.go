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
	frame    int
	active   bool
	message  string
	onCancel func()
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
	s.ClearOnCancel()
}

func (s *SpinnerComponent) SetOnCancel(fn func()) { s.onCancel = fn }
func (s *SpinnerComponent) ClearOnCancel()        { s.onCancel = nil }

func (s *SpinnerComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc && s.onCancel != nil {
		s.onCancel()
	}
	return nil
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
	if s.onCancel != nil {
		line += "  " + lipgloss.NewStyle().Faint(true).Render("(Esc to cancel)")
	}
	return []string{line}
}

func SpinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}
