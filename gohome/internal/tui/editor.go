package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"
)

const (
	editorMinHeight = 3
	editorMaxRatio  = 0.3
)

var (
	editorBorder     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	editorBashBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// visualRow represents one screen line produced by soft-wrapping a logical line.
type visualRow struct {
	logicalLine int // index into EditorComponent.lines
	startCol    int // rune offset where this visual row begins
	runeLen     int // number of runes in this visual row
}

// EditorComponent is a multi-line text editor implementing Interactive.
type EditorComponent struct {
	lines      []string
	cursorLine int
	cursorCol  int
	scrollTop  int
	width      int
	termHeight int
	desiredCol int // sticky visual column for Up/Down; -1 means unset
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
		desiredCol: -1,
		history:    NewHistory(100),
	}
}

// wrapLine splits a single logical line into visual rows that fit within width
// terminal columns. It wraps at word boundaries (spaces) when possible, falling
// back to character-boundary breaks for words wider than width. Uses
// uniseg.StringWidth for correct wide-character handling.
func wrapLine(line string, logicalIdx int, width int) []visualRow {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	if len(runes) == 0 {
		return []visualRow{{logicalLine: logicalIdx, startCol: 0, runeLen: 0}}
	}

	var rows []visualRow
	start := 0

	for start < len(runes) {
		visualW := 0
		end := start
		lastSpace := -1

		for end < len(runes) {
			rw := uniseg.StringWidth(string(runes[end : end+1]))
			if visualW+rw > width {
				break
			}
			if runes[end] == ' ' {
				lastSpace = end
			}
			visualW += rw
			end++
		}

		if end == len(runes) {
			rows = append(rows, visualRow{logicalLine: logicalIdx, startCol: start, runeLen: end - start})
			break
		}

		if lastSpace > start {
			breakAt := lastSpace + 1
			rows = append(rows, visualRow{logicalLine: logicalIdx, startCol: start, runeLen: breakAt - start})
			start = breakAt
		} else {
			if end == start {
				end = start + 1
			}
			rows = append(rows, visualRow{logicalLine: logicalIdx, startCol: start, runeLen: end - start})
			start = end
		}
	}

	return rows
}

// buildVisualLayout wraps all logical lines and returns the full visual row list.
func (e *EditorComponent) buildVisualLayout(width int) []visualRow {
	wrapW := width - 1
	if wrapW < 1 {
		wrapW = 1
	}
	var rows []visualRow
	for i, line := range e.lines {
		rows = append(rows, wrapLine(line, i, wrapW)...)
	}
	return rows
}

// findVisualRow returns the visual row index containing the cursor position.
func findVisualRow(rows []visualRow, logLine, logCol int) int {
	for i, r := range rows {
		if r.logicalLine != logLine {
			continue
		}
		if logCol >= r.startCol && logCol < r.startCol+r.runeLen {
			return i
		}
		if r.runeLen == 0 && logCol == r.startCol {
			return i
		}
	}
	// Cursor is at end of line -- return the last visual row for this logical line.
	last := 0
	for i, r := range rows {
		if r.logicalLine == logLine {
			last = i
		}
	}
	return last
}

// visualColForCursor returns the visual column of the cursor within its visual row.
func visualColForCursor(rows []visualRow, line string, vrIdx int, logCol int) int {
	vr := rows[vrIdx]
	localCol := logCol - vr.startCol
	if localCol < 0 {
		localCol = 0
	}
	runes := []rune(line)
	end := vr.startCol + localCol
	if end > len(runes) {
		end = len(runes)
	}
	return uniseg.StringWidth(string(runes[vr.startCol:end]))
}

// logColFromVisualCol maps a visual column width back to a rune offset within
// a visual row, used when moving the cursor vertically between rows.
func logColFromVisualCol(line string, vr visualRow, targetVisualCol int) int {
	runes := []rune(line)
	end := vr.startCol + vr.runeLen
	if end > len(runes) {
		end = len(runes)
	}
	w := 0
	for i := vr.startCol; i < end; i++ {
		rw := uniseg.StringWidth(string(runes[i : i+1]))
		if w+rw > targetVisualCol {
			return i
		}
		w += rw
	}
	return end
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
	e.desiredCol = -1
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
	e.desiredCol = -1
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

// InsertText inserts a block of text at the current cursor position.
// It strips carriage returns, replaces tabs with four spaces, and splits on
// newlines so multi-line pastes are handled correctly.
func (e *EditorComponent) InsertText(s string) {
	e.desiredCol = -1
	if e.browsing {
		e.history.StopBrowsing()
		e.browsing = false
	}

	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")

	parts := strings.Split(s, "\n")
	if len(parts) == 0 {
		return
	}

	line := []rune(e.lines[e.cursorLine])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}

	before := string(line[:col])
	after := string(line[col:])

	if len(parts) == 1 {
		e.lines[e.cursorLine] = before + parts[0] + after
		e.cursorCol = col + utf8.RuneCountInString(parts[0])
		return
	}

	// First part appends to current line's prefix.
	e.lines[e.cursorLine] = before + parts[0]

	// Middle parts are new lines.
	newLines := make([]string, 0, len(e.lines)+len(parts)-1)
	newLines = append(newLines, e.lines[:e.cursorLine+1]...)
	newLines = append(newLines, parts[1:len(parts)-1]...)

	// Last part gets the suffix from the original line.
	lastPart := parts[len(parts)-1]
	newLines = append(newLines, lastPart+after)
	newLines = append(newLines, e.lines[e.cursorLine+1:]...)

	e.lines = newLines
	e.cursorLine = e.cursorLine + len(parts) - 1
	e.cursorCol = utf8.RuneCountInString(lastPart)
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
	e.width = width
	maxH := e.maxHeight()

	borderStyle := editorBorder
	if len(e.lines) > 0 && strings.HasPrefix(strings.TrimSpace(e.lines[0]), "!") {
		borderStyle = editorBashBorder
	}

	borderLine := func(indicator string) string {
		base := strings.Repeat("─", width-len(indicator))
		return borderStyle.Render(base + indicator)
	}

	rows := e.buildVisualLayout(width)
	totalRows := len(rows)

	e.clampScrollVisual(rows, maxH)

	end := e.scrollTop + maxH
	if end > totalRows {
		end = totalRows
	}
	visible := rows[e.scrollTop:end]

	topIndicator := ""
	if e.scrollTop > 0 {
		topIndicator = "^"
	}

	botIndicator := ""
	if end < totalRows {
		botIndicator = "v"
	}

	out := []string{borderLine(topIndicator)}
	for _, vr := range visible {
		runes := []rune(e.lines[vr.logicalLine])
		endCol := vr.startCol + vr.runeLen
		if endCol > len(runes) {
			endCol = len(runes)
		}
		text := string(runes[vr.startCol:endCol])

		if vr.logicalLine == e.cursorLine &&
			e.cursorCol >= vr.startCol && e.cursorCol <= vr.startCol+vr.runeLen {
			localCol := e.cursorCol - vr.startCol
			out = append(out, renderCursorInRow(text, localCol))
		} else {
			out = append(out, text)
		}
	}

	for len(out)-1 < editorMinHeight {
		out = append(out, "")
	}
	out = append(out, borderLine(botIndicator))

	return out
}

// renderCursorInRow inserts reverse-video styling at localCol within a visual row's text.
func renderCursorInRow(text string, localCol int) string {
	runes := []rune(text)
	if localCol > len(runes) {
		localCol = len(runes)
	}

	before := string(runes[:localCol])
	if localCol < len(runes) {
		cursorChar := string(runes[localCol])
		after := string(runes[localCol+1:])
		return before + "\x1b[7m" + cursorChar + "\x1b[0m" + after
	}
	return before + "\x1b[7m \x1b[0m"
}

// clampScroll is a legacy helper for callers that don't have the visual layout.
// It builds the layout and delegates to clampScrollVisual.
func (e *EditorComponent) clampScroll() {
	rows := e.buildVisualLayout(e.width)
	e.clampScrollVisual(rows, e.maxHeight())
}

// clampScrollVisual ensures scrollTop keeps the cursor's visual row visible.
func (e *EditorComponent) clampScrollVisual(rows []visualRow, maxH int) {
	vrIdx := findVisualRow(rows, e.cursorLine, e.cursorCol)
	if vrIdx < e.scrollTop {
		e.scrollTop = vrIdx
	}
	if vrIdx >= e.scrollTop+maxH {
		e.scrollTop = vrIdx - maxH + 1
	}
	if e.scrollTop < 0 {
		e.scrollTop = 0
	}
}

// HandleInput handles keyboard input and returns a tea.Cmd.
func (e *EditorComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	// Bracketed paste: insert all runes as text block.
	if msg.Paste {
		e.InsertText(string(msg.Runes))
		return nil
	}

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
		rows := e.buildVisualLayout(e.width)
		vrIdx := findVisualRow(rows, e.cursorLine, e.cursorCol)
		if vrIdx == 0 {
			if !e.browsing {
				e.history.StartBrowsing(e.Value())
				e.browsing = true
			}
			prev := e.history.Prev()
			e.SetValue(prev)
			e.desiredCol = -1
		} else {
			if e.desiredCol < 0 {
				e.desiredCol = visualColForCursor(rows, e.lines[e.cursorLine], vrIdx, e.cursorCol)
			}
			target := rows[vrIdx-1]
			e.cursorLine = target.logicalLine
			e.cursorCol = logColFromVisualCol(e.lines[target.logicalLine], target, e.desiredCol)
			e.clampScroll()
		}
		return nil

	case tea.KeyDown:
		rows := e.buildVisualLayout(e.width)
		vrIdx := findVisualRow(rows, e.cursorLine, e.cursorCol)
		if vrIdx >= len(rows)-1 {
			if e.browsing {
				next := e.history.Next()
				e.SetValue(next)
				if !e.history.Browsing() {
					e.browsing = false
				}
			}
			e.desiredCol = -1
		} else {
			if e.desiredCol < 0 {
				e.desiredCol = visualColForCursor(rows, e.lines[e.cursorLine], vrIdx, e.cursorCol)
			}
			target := rows[vrIdx+1]
			e.cursorLine = target.logicalLine
			e.cursorCol = logColFromVisualCol(e.lines[target.logicalLine], target, e.desiredCol)
			e.clampScroll()
		}
		return nil

	case tea.KeyLeft:
		e.desiredCol = -1
		if e.cursorCol > 0 {
			e.cursorCol--
		} else if e.cursorLine > 0 {
			e.cursorLine--
			e.cursorCol = utf8.RuneCountInString(e.lines[e.cursorLine])
			e.clampScroll()
		}
		return nil

	case tea.KeyRight:
		e.desiredCol = -1
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
		e.desiredCol = -1
		e.cursorCol = 0
		return nil

	case tea.KeyCtrlE, tea.KeyEnd:
		e.desiredCol = -1
		e.cursorCol = utf8.RuneCountInString(e.lines[e.cursorLine])
		return nil

	case tea.KeyCtrlK:
		e.desiredCol = -1
		line := []rune(e.lines[e.cursorLine])
		e.lines[e.cursorLine] = string(line[:e.cursorCol])
		return nil

	case tea.KeyCtrlU:
		e.desiredCol = -1
		line := []rune(e.lines[e.cursorLine])
		e.lines[e.cursorLine] = string(line[e.cursorCol:])
		e.cursorCol = 0
		return nil

	case tea.KeyBackspace:
		e.desiredCol = -1
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
		e.desiredCol = -1
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
