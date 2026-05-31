package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	editorMinHeight = 3
	editorMaxRatio  = 0.3
)

var (
	editorBorder     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	editorBashBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// EditorComponent is a multi-line text editor implementing Interactive.
type EditorComponent struct {
	lines      []string
	cursorLine int
	cursorCol  int
	scrollTop  int
	width      int
	termHeight int
	history    *History
	browsing   bool
	onSubmit   func(string)
}

// NewEditor creates a new EditorComponent with the given dimensions.
func NewEditor(width, termHeight int) *EditorComponent {
	return &EditorComponent{
		lines:      []string{""},
		width:      width,
		termHeight: termHeight,
		history:    NewHistory(100),
	}
}

// SetSubmitHandler sets the callback invoked when the user submits text.
func (e *EditorComponent) SetSubmitHandler(fn func(string)) {
	e.onSubmit = fn
}

// SetTermHeight updates the terminal height (for resize events).
func (e *EditorComponent) SetTermHeight(h int) {
	e.termHeight = h
}

// maxHeight returns the maximum number of content lines to display.
func (e *EditorComponent) maxHeight() int {
	h := int(float64(e.termHeight) * editorMaxRatio)
	if h < editorMinHeight {
		h = editorMinHeight
	}
	return h
}

// Value returns the editor content as a single string with newlines.
func (e *EditorComponent) Value() string {
	return strings.Join(e.lines, "\n")
}

// SetValue sets the editor content, splitting on newlines, and moves the
// cursor to the end of the last line.
func (e *EditorComponent) SetValue(s string) {
	if s == "" {
		e.lines = []string{""}
		e.cursorLine = 0
		e.cursorCol = 0
		e.scrollTop = 0
		return
	}
	e.lines = strings.Split(s, "\n")
	e.cursorLine = len(e.lines) - 1
	e.cursorCol = utf8.RuneCountInString(e.lines[e.cursorLine])
	e.clampScroll()
}

// InsertRune inserts a rune at the current cursor position.
func (e *EditorComponent) InsertRune(r rune) {
	// Stop history browsing when user types.
	if e.browsing {
		e.history.StopBrowsing()
		e.browsing = false
	}
	line := []rune(e.lines[e.cursorLine])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}
	newLine := make([]rune, 0, len(line)+1)
	newLine = append(newLine, line[:col]...)
	newLine = append(newLine, r)
	newLine = append(newLine, line[col:]...)
	e.lines[e.cursorLine] = string(newLine)
	e.cursorCol = col + 1
}

// InsertNewline splits the current line at the cursor position.
func (e *EditorComponent) InsertNewline() {
	if e.browsing {
		e.history.StopBrowsing()
		e.browsing = false
	}
	line := []rune(e.lines[e.cursorLine])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}
	before := string(line[:col])
	after := string(line[col:])
	e.lines[e.cursorLine] = before
	// Insert the new line after current.
	newLines := make([]string, 0, len(e.lines)+1)
	newLines = append(newLines, e.lines[:e.cursorLine+1]...)
	newLines = append(newLines, after)
	newLines = append(newLines, e.lines[e.cursorLine+1:]...)
	e.lines = newLines
	e.cursorLine++
	e.cursorCol = 0
	e.clampScroll()
}

// Submit returns the trimmed content and true if non-empty, clears the editor,
// and adds the text to history. Returns ("", false) if the content is empty.
func (e *EditorComponent) Submit() (string, bool) {
	text := strings.TrimSpace(e.Value())
	if text == "" {
		return "", false
	}
	e.history.Add(text)
	e.SetValue("")
	return text, true
}

// Render returns terminal lines for the editor at the given width.
// It includes a top border, content lines (scrolled), and a bottom border.
func (e *EditorComponent) Render(width int) []string {
	maxH := e.maxHeight()

	// Determine border style based on content.
	borderStyle := editorBorder
	if len(e.lines) > 0 && strings.HasPrefix(strings.TrimSpace(e.lines[0]), "!") {
		borderStyle = editorBashBorder
	}

	borderLine := func(indicator string) string {
		base := strings.Repeat("─", width-len(indicator))
		return borderStyle.Render(base + indicator)
	}

	// Determine visible content slice.
	totalLines := len(e.lines)
	e.clampScroll()

	end := e.scrollTop + maxH
	if end > totalLines {
		end = totalLines
	}
	visibleLines := e.lines[e.scrollTop:end]

	// Build top border with scroll indicator.
	topIndicator := ""
	if e.scrollTop > 0 {
		topIndicator = "^"
	}
	topBorder := borderLine(topIndicator)

	// Build bottom border with scroll indicator.
	botIndicator := ""
	if end < totalLines {
		botIndicator = "v"
	}
	botBorder := borderLine(botIndicator)

	// Render content lines.
	out := []string{topBorder}
	for i, ln := range visibleLines {
		lineIdx := e.scrollTop + i
		if lineIdx == e.cursorLine {
			out = append(out, e.renderWithCursor(ln))
		} else {
			out = append(out, ln)
		}
	}
	// Pad to at least editorMinHeight content lines.
	for len(out)-1 < editorMinHeight {
		if len(out)-1 == 0 && e.cursorLine == 0 && len(e.lines) == 1 {
			// Already rendered cursor line above; add empty lines.
		}
		out = append(out, "")
	}
	out = append(out, botBorder)

	return out
}

// renderWithCursor inserts reverse-video styling at the cursor column position.
func (e *EditorComponent) renderWithCursor(line string) string {
	runes := []rune(line)
	col := e.cursorCol
	if col > len(runes) {
		col = len(runes)
	}

	before := string(runes[:col])
	if col < len(runes) {
		cursorChar := string(runes[col])
		after := string(runes[col+1:])
		return before + "\x1b[7m" + cursorChar + "\x1b[0m" + after
	}
	// Cursor is past the end of the line: append reverse-video space.
	return before + "\x1b[7m \x1b[0m"
}

// clampScroll ensures scrollTop keeps the cursor line visible.
func (e *EditorComponent) clampScroll() {
	maxH := e.maxHeight()
	if e.cursorLine < e.scrollTop {
		e.scrollTop = e.cursorLine
	}
	if e.cursorLine >= e.scrollTop+maxH {
		e.scrollTop = e.cursorLine - maxH + 1
	}
	if e.scrollTop < 0 {
		e.scrollTop = 0
	}
}

// HandleInput handles keyboard input and returns a tea.Cmd.
func (e *EditorComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		if msg.Alt {
			e.InsertNewline()
			return nil
		}
		// Normal Enter: submit.
		text, ok := e.Submit()
		if ok && e.onSubmit != nil {
			e.onSubmit(text)
		}
		return nil

	case tea.KeyShiftDown:
		e.InsertNewline()
		return nil

	case tea.KeyUp:
		if e.cursorLine == 0 {
			// Browse history.
			if !e.browsing {
				e.history.StartBrowsing(e.Value())
				e.browsing = true
			}
			prev := e.history.Prev()
			e.SetValue(prev)
		} else {
			e.cursorLine--
			lineLen := utf8.RuneCountInString(e.lines[e.cursorLine])
			if e.cursorCol > lineLen {
				e.cursorCol = lineLen
			}
			e.clampScroll()
		}
		return nil

	case tea.KeyDown:
		if e.cursorLine >= len(e.lines)-1 {
			if e.browsing {
				next := e.history.Next()
				e.SetValue(next)
				if !e.history.Browsing() {
					e.browsing = false
				}
			}
		} else {
			e.cursorLine++
			lineLen := utf8.RuneCountInString(e.lines[e.cursorLine])
			if e.cursorCol > lineLen {
				e.cursorCol = lineLen
			}
			e.clampScroll()
		}
		return nil

	case tea.KeyLeft:
		if e.cursorCol > 0 {
			e.cursorCol--
		} else if e.cursorLine > 0 {
			e.cursorLine--
			e.cursorCol = utf8.RuneCountInString(e.lines[e.cursorLine])
			e.clampScroll()
		}
		return nil

	case tea.KeyRight:
		lineLen := utf8.RuneCountInString(e.lines[e.cursorLine])
		if e.cursorCol < lineLen {
			e.cursorCol++
		} else if e.cursorLine < len(e.lines)-1 {
			e.cursorLine++
			e.cursorCol = 0
			e.clampScroll()
		}
		return nil

	case tea.KeyCtrlA, tea.KeyHome:
		e.cursorCol = 0
		return nil

	case tea.KeyCtrlE, tea.KeyEnd:
		e.cursorCol = utf8.RuneCountInString(e.lines[e.cursorLine])
		return nil

	case tea.KeyCtrlK:
		// Kill to end of line.
		line := []rune(e.lines[e.cursorLine])
		e.lines[e.cursorLine] = string(line[:e.cursorCol])
		return nil

	case tea.KeyCtrlU:
		// Kill from start of line to cursor.
		line := []rune(e.lines[e.cursorLine])
		e.lines[e.cursorLine] = string(line[e.cursorCol:])
		e.cursorCol = 0
		return nil

	case tea.KeyBackspace:
		if e.cursorCol > 0 {
			line := []rune(e.lines[e.cursorLine])
			newLine := make([]rune, 0, len(line)-1)
			newLine = append(newLine, line[:e.cursorCol-1]...)
			newLine = append(newLine, line[e.cursorCol:]...)
			e.lines[e.cursorLine] = string(newLine)
			e.cursorCol--
		} else if e.cursorLine > 0 {
			// Join with previous line.
			prev := e.lines[e.cursorLine-1]
			cur := e.lines[e.cursorLine]
			newCol := utf8.RuneCountInString(prev)
			e.lines = append(e.lines[:e.cursorLine-1], append([]string{prev + cur}, e.lines[e.cursorLine+1:]...)...)
			e.cursorLine--
			e.cursorCol = newCol
			e.clampScroll()
		}
		return nil

	case tea.KeyDelete:
		line := []rune(e.lines[e.cursorLine])
		if e.cursorCol < len(line) {
			newLine := make([]rune, 0, len(line)-1)
			newLine = append(newLine, line[:e.cursorCol]...)
			newLine = append(newLine, line[e.cursorCol+1:]...)
			e.lines[e.cursorLine] = string(newLine)
		} else if e.cursorLine < len(e.lines)-1 {
			// Join with next line.
			cur := e.lines[e.cursorLine]
			next := e.lines[e.cursorLine+1]
			e.lines = append(e.lines[:e.cursorLine], append([]string{cur + next}, e.lines[e.cursorLine+2:]...)...)
		}
		return nil

	case tea.KeySpace:
		e.InsertRune(' ')
		return nil

	case tea.KeyRunes:
		for _, r := range msg.Runes {
			e.InsertRune(r)
		}
		return nil
	}

	return nil
}
