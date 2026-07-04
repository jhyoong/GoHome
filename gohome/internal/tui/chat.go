package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxPreviewLines = 3

var (
	userBlockStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).
			BorderRight(false).
			BorderTop(false).
			BorderBottom(false).
			BorderForeground(lipgloss.Color("12"))
	noticeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	expandedBg     = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	diffBoxDefault = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
	diffBoxDenied = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("1")).
			Padding(0, 1)
)

// ChatComponent renders a timeline of entries with markdown support and scrolling.
type ChatComponent struct {
	timeline   *[]TimelineEntry
	scrollTop  int
	maxHeight  int
	autoScroll bool
	cursor     int
	lastCursor int
}

// NewChat creates a new ChatComponent backed by the given timeline pointer.
func NewChat(timeline *[]TimelineEntry, maxHeight int) *ChatComponent {
	return &ChatComponent{
		timeline:   timeline,
		maxHeight:  maxHeight,
		autoScroll: true,
		cursor:     -1,
		lastCursor: -1,
	}
}

// SetMaxHeight updates the visible height of the component.
func (c *ChatComponent) SetMaxHeight(h int) { c.maxHeight = h }

// SetCursor sets the index of the highlighted timeline entry.
func (c *ChatComponent) SetCursor(idx int) { c.cursor = idx }

// SetTimeline updates the timeline pointer backing this component.
func (c *ChatComponent) SetTimeline(t *[]TimelineEntry) { c.timeline = t }

// ScrollUp scrolls up by n lines, disabling auto-scroll.
func (c *ChatComponent) ScrollUp(n int) {
	c.scrollTop -= n
	if c.scrollTop < 0 {
		c.scrollTop = 0
	}
	c.autoScroll = false
}

// ScrollDown scrolls down by n lines, disabling auto-scroll.
func (c *ChatComponent) ScrollDown(n int) {
	c.scrollTop += n
	c.autoScroll = false
}

// ScrollToBottom re-enables auto-scroll so new content keeps the view at the bottom.
func (c *ChatComponent) ScrollToBottom() {
	c.autoScroll = true
}

// IsAutoScroll reports whether auto-scroll is active.
func (c *ChatComponent) IsAutoScroll() bool { return c.autoScroll }

// DisableAutoScroll turns off auto-scroll, anchoring scrollTop to the current
// effective position so the viewport does not jump. maxWidth is the terminal
// column width used to compute the pre-expansion line count.
func (c *ChatComponent) DisableAutoScroll(maxWidth int) {
	if !c.autoScroll {
		return
	}
	// When autoScroll is true the view shows the last maxHeight lines.
	// Compute total line count so we can anchor scrollTop accordingly.
	if c.timeline == nil || len(*c.timeline) == 0 || c.maxHeight <= 0 {
		c.autoScroll = false
		return
	}
	total := c.countLines(maxWidth)
	if total > c.maxHeight {
		c.scrollTop = total - c.maxHeight
	} else {
		c.scrollTop = 0
	}
	c.autoScroll = false
}

// EnsureCursorVisible adjusts scrollTop so the cursor entry is within the
// visible viewport. Call after changing the cursor via arrow keys.
func (c *ChatComponent) EnsureCursorVisible(maxWidth int) {
	if c.timeline == nil || c.cursor < 0 || c.cursor >= len(*c.timeline) {
		return
	}
	if c.maxHeight <= 0 {
		return
	}

	cursorTop := 0
	for i := 0; i < c.cursor; i++ {
		cursorTop += c.entryLineCount(&(*c.timeline)[i], maxWidth)
	}
	cursorHeight := c.entryLineCount(&(*c.timeline)[c.cursor], maxWidth)

	total := c.countLines(maxWidth)
	if total <= c.maxHeight {
		return
	}

	effectiveTop := c.scrollTop
	if c.autoScroll {
		effectiveTop = total - c.maxHeight
	}

	needsAdjust := false
	if cursorHeight > c.maxHeight {
		// Entry taller than viewport: pin to the top of the entry to avoid
		// oscillating between top and bottom on repeated key presses.
		if cursorTop < effectiveTop || cursorTop >= effectiveTop+c.maxHeight {
			c.scrollTop = cursorTop
			needsAdjust = true
		}
	} else if cursorTop < effectiveTop {
		c.scrollTop = cursorTop
		needsAdjust = true
	} else if cursorTop+cursorHeight > effectiveTop+c.maxHeight {
		c.scrollTop = cursorTop + cursorHeight - c.maxHeight
		needsAdjust = true
	}

	if needsAdjust {
		c.autoScroll = false
	}
}

// entryLineCount returns the number of rendered lines for a single timeline entry.
func (c *ChatComponent) entryLineCount(e *TimelineEntry, maxWidth int) int {
	if e.cacheValid(maxWidth) {
		return len(e.cachedLines)
	}
	switch e.Kind {
	case KindUser:
		return len(WrapText(e.Text, maxWidth-3))
	case KindAssistant:
		lines := RenderMarkdown(e.Text, maxWidth-2)
		if len(lines) == 0 {
			lines = WrapText(e.Text, maxWidth-2)
		}
		return len(lines)
	case KindThinking:
		return len(WrapText(e.Text, maxWidth-2))
	case KindTool:
		count := 1
		if !e.Expanded {
			if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
				indent := maxWidth - 9
				if e.Shadow {
					indent = maxWidth - 15
				}
				for _, pl := range pv {
					count += len(WrapText(pl, indent))
				}
				if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
					count++
				}
			}
		} else {
			if e.Shadow {
				if e.Text != "" {
					count += len(WrapText("args: "+e.Text, maxWidth-11))
				}
				if e.ToolResult != "" {
					count++
					count += len(WrapText(e.ToolResult, maxWidth-13))
				}
			} else {
				if e.Text != "" {
					count += len(WrapText("args: "+e.Text, maxWidth-7))
				}
				if e.ToolResult != "" {
					count++
					count += len(WrapText(e.ToolResult, maxWidth-9))
				}
			}
		}
		if e.DiffPreview != "" {
			indent := 2
			if e.Shadow {
				indent = 6
			}
			diffLines := renderDiffBox(e.DiffPreview, e.Status, maxWidth, indent)
			count += len(diffLines)
		}
		return count
	case KindNotice:
		return 1
	}
	return 1
}

// countLines returns the total number of rendered lines for all timeline entries
// at the given maxWidth. Uses cached line counts when available.
func (c *ChatComponent) countLines(maxWidth int) int {
	if c.timeline == nil {
		return 0
	}
	count := 0
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		if e.cacheValid(maxWidth) {
			count += len(e.cachedLines)
			continue
		}
		switch e.Kind {
		case KindUser:
			count += len(WrapText(e.Text, maxWidth-3))
		case KindAssistant:
			lines := RenderMarkdown(e.Text, maxWidth-2)
			if len(lines) == 0 {
				lines = WrapText(e.Text, maxWidth-2)
			}
			count += len(lines)
		case KindThinking:
			count += len(WrapText(e.Text, maxWidth-2))
		case KindTool:
			count++
			if !e.Expanded {
				if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
					indent := maxWidth - 9
					if e.Shadow {
						indent = maxWidth - 15
					}
					for _, pl := range pv {
						count += len(WrapText(pl, indent))
					}
					if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
						count++
					}
				}
			} else {
				if e.Shadow {
					if e.Text != "" {
						count += len(WrapText("args: "+e.Text, maxWidth-11))
					}
					if e.ToolResult != "" {
						count++
						count += len(WrapText(e.ToolResult, maxWidth-13))
					}
				} else {
					if e.Text != "" {
						count += len(WrapText("args: "+e.Text, maxWidth-7))
					}
					if e.ToolResult != "" {
						count++
						count += len(WrapText(e.ToolResult, maxWidth-9))
					}
				}
			}
			if e.DiffPreview != "" {
				indent := 2
				if e.Shadow {
					indent = 6
				}
				diffLines := renderDiffBox(e.DiffPreview, e.Status, maxWidth, indent)
				count += len(diffLines)
			}
		case KindNotice:
			count++
		}
	}
	return count
}

// Render converts the current timeline to a slice of display lines, applying
// scroll and height constraints. maxWidth is the terminal column width.
func (c *ChatComponent) Render(maxWidth int) []string {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return nil
	}

	// Invalidate cache for entries whose cursor marker changed.
	if c.lastCursor != c.cursor && c.timeline != nil {
		tl := *c.timeline
		if c.lastCursor >= 0 && c.lastCursor < len(tl) {
			tl[c.lastCursor].cachedLines = nil
		}
		if c.cursor >= 0 && c.cursor < len(tl) {
			tl[c.cursor].cachedLines = nil
		}
		c.lastCursor = c.cursor
	}

	// Render all entries into lines, using cache when valid.
	var all []string
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		if !e.cacheValid(maxWidth) {
			e.cachedLines = c.renderEntry(e, maxWidth, marker)
			e.cachedWidth = maxWidth
			e.cachedExpanded = e.Expanded
			e.cachedText = e.Text
			e.cachedResult = e.ToolResult
			e.cachedDiffStatus = e.Status
		}

		all = append(all, e.cachedLines...)
	}

	// Apply scroll and height constraints.
	total := len(all)
	if c.maxHeight <= 0 || total <= c.maxHeight {
		return all
	}

	if c.autoScroll {
		return all[total-c.maxHeight:]
	}

	maxScroll := total - c.maxHeight
	if c.scrollTop > maxScroll {
		c.scrollTop = maxScroll
	}
	if c.scrollTop < 0 {
		c.scrollTop = 0
	}

	end := c.scrollTop + c.maxHeight
	if end > total {
		end = total
	}
	return all[c.scrollTop:end]
}

// renderEntry produces the display lines for a single timeline entry.
func (c *ChatComponent) renderEntry(e *TimelineEntry, maxWidth int, marker string) []string {
	var lines []string

	switch e.Kind {
	case KindUser:
		text := WrapText(e.Text, maxWidth-3)
		styled := userBlockStyle.Width(maxWidth - 2).Render(strings.Join(text, "\n"))
		for j, l := range strings.Split(styled, "\n") {
			if j == 0 {
				lines = append(lines, marker+l)
			} else {
				lines = append(lines, "  "+l)
			}
		}

	case KindAssistant:
		mdLines := RenderMarkdown(e.Text, maxWidth-2)
		if len(mdLines) == 0 {
			mdLines = WrapText(e.Text, maxWidth-2)
		}
		for j, l := range mdLines {
			if j == 0 {
				lines = append(lines, marker+l)
			} else {
				lines = append(lines, "  "+l)
			}
		}

	case KindThinking:
		wrapped := WrapText(e.Text, maxWidth-2)
		for j, l := range wrapped {
			styled := ansiDim + ansiItalic + l + ansiReset
			if j == 0 {
				lines = append(lines, marker+styled)
			} else {
				lines = append(lines, "  "+styled)
			}
		}

	case KindTool:
		if e.Shadow {
			line := renderToolSummary(*e, maxWidth-6)
			lines = append(lines, marker+"    "+ansiDim+line+ansiReset)
			if !e.Expanded {
				if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
					for _, pl := range pv {
						for _, wl := range WrapText(pl, maxWidth-15) {
							lines = append(lines, "             "+ansiDim+wl+ansiReset)
						}
					}
					if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
						hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", total-maxPreviewLines)
						lines = append(lines, "             "+ansiDim+hint+ansiReset)
					}
				}
			} else {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-11) {
						lines = append(lines, expandedBg.Render("           "+ansiDim+l+ansiReset))
					}
				}
				if e.ToolResult != "" {
					lines = append(lines, expandedBg.Render("           "+ansiDim+"result:"+ansiReset))
					for _, l := range WrapText(e.ToolResult, maxWidth-13) {
						lines = append(lines, expandedBg.Render("             "+ansiDim+l+ansiReset))
					}
				}
			}
			if e.DiffPreview != "" {
				diffLines := renderDiffBox(e.DiffPreview, e.Status, maxWidth, 6)
				for i, l := range diffLines {
					diffLines[i] = ansiDim + l + ansiReset
				}
				lines = append(lines, diffLines...)
			}
		} else {
			line := renderToolSummary(*e, maxWidth-2)
			lines = append(lines, marker+line)
			if !e.Expanded {
				if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
					for _, pl := range pv {
						for _, wl := range WrapText(pl, maxWidth-9) {
							lines = append(lines, "       "+ansiDim+wl+ansiReset)
						}
					}
					if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
						hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", total-maxPreviewLines)
						lines = append(lines, "       "+ansiDim+hint+ansiReset)
					}
				}
			} else {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
						lines = append(lines, expandedBg.Render("       "+l))
					}
				}
				if e.ToolResult != "" {
					lines = append(lines, expandedBg.Render("       result:"))
					for _, l := range WrapText(e.ToolResult, maxWidth-9) {
						lines = append(lines, expandedBg.Render("         "+l))
					}
				}
			}
			// Diff box for edit tools (always visible).
			if e.DiffPreview != "" {
				diffLines := renderDiffBox(e.DiffPreview, e.Status, maxWidth, 2)
				lines = append(lines, diffLines...)
			}
		}

	case KindNotice:
		line := noticeStyle.Render(fmt.Sprintf("[notice] %s", e.Text))
		lines = append(lines, marker+line)
	}

	return lines
}

// renderDiffBox renders the diff preview as a bordered box with colored lines.
func renderDiffBox(diff string, status string, maxWidth int, indent int) []string {
	if diff == "" {
		return nil
	}

	boxStyle := diffBoxDefault
	if status == "error" {
		boxStyle = diffBoxDenied
	}

	// Color the diff lines.
	var colored []string
	for _, line := range strings.Split(diff, "\n") {
		if containsDiffMarker(line, "  - ") {
			colored = append(colored, "\x1b[31m"+line+"\x1b[0m")
		} else if containsDiffMarker(line, "  + ") {
			colored = append(colored, "\x1b[32m"+line+"\x1b[0m")
		} else {
			colored = append(colored, ansiDim+line+ansiReset)
		}
	}

	inner := strings.Join(colored, "\n")
	if status == "error" {
		inner = ansiDim + inner + ansiReset
	}

	boxWidth := maxWidth - indent - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	rendered := boxStyle.Width(boxWidth).Render(inner)

	prefix := strings.Repeat(" ", indent)
	var result []string
	for _, l := range strings.Split(rendered, "\n") {
		result = append(result, prefix+l)
	}
	return result
}

func containsDiffMarker(line, marker string) bool {
	return strings.Contains(line, marker)
}

// previewLines returns the last maxLines lines from s for use as a dimmed
// preview below collapsed tool entries. If s has 0 or 1 lines, it returns nil
// (single-line results are already shown in the arrow summary). If s has 2-3
// lines, all lines are returned. If more than maxLines, only the last maxLines
// are returned.
func previewLines(s string, maxLines int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return nil
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	for i, l := range lines {
		if strings.ContainsRune(l, '\t') {
			lines[i] = expandTabs(l, 4)
		}
	}
	return lines
}

// expandTabs replaces each tab with spaces to align to the next tabStop boundary,
// skipping ANSI escape sequences when computing column position.
func expandTabs(s string, tabStop int) string {
	if tabStop <= 0 {
		tabStop = 4
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	col := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			loc := ansiEscape.FindStringIndex(s[i:])
			if loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		if s[i] == '\t' {
			spaces := tabStop - (col % tabStop)
			for j := 0; j < spaces; j++ {
				b.WriteByte(' ')
			}
			col += spaces
			i++
			continue
		}
		b.WriteByte(s[i])
		col++
		i++
	}
	return b.String()
}

// extractToolArg parses the JSON input for a tool call and returns the most
// relevant argument for display. Known fields: "command" (bash), "file_path"
// (read/write/edit), "prompt" (subagent). Falls back to shortSummary on parse
// failure or unknown tools.
func extractToolArg(toolName, inputJSON string) string {
	inputJSON = strings.TrimSpace(inputJSON)
	if inputJSON == "" {
		return ""
	}

	var key string
	switch toolName {
	case "bash":
		key = "command"
	case "read":
		key = "file_path"
	case "write":
		key = "file_path"
	case "edit":
		key = "file_path"
	case "subagent":
		key = "prompt"
	}

	if key != "" {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(inputJSON), &m); err == nil {
			if raw, ok := m[key]; ok {
				var val string
				if err2 := json.Unmarshal(raw, &val); err2 == nil {
					return val
				}
			}
		}
	}

	// Fallback for unknown tools or parse failures.
	return shortSummary(inputJSON)
}

// renderToolSummary builds the collapsed single-line representation of a tool entry
// using contextual display (e.g. "$ cmd" for bash, file paths for read/edit/write).
func renderToolSummary(e TimelineEntry, maxWidth int) string {
	arg := extractToolArg(e.ToolName, e.Text)
	result := shortSummary(e.ToolResult)

	var st lipgloss.Style
	switch e.Status {
	case "error":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	case "success":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	default: // "pending" or ""
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	}

	// Build the contextual prefix based on tool type.
	var prefix string
	switch e.ToolName {
	case "bash":
		prefix = "$ " + arg
	case "read":
		prefix = arg
	case "write":
		prefix = "write " + arg
	case "edit":
		prefix = "edit " + arg
	case "subagent":
		prefix = "subagent: " + arg
	default:
		if arg != "" {
			prefix = e.ToolName + ": " + arg
		} else {
			prefix = e.ToolName
		}
	}

	line := st.Render(prefix)
	if e.Status == "error" && result != "" {
		line += " -> ERROR: " + result
	} else if result != "" {
		line += " -> " + result
	}
	if VisualWidth(StripAnsi(line)) > maxWidth {
		line = TruncateText(line, maxWidth)
	}
	return line
}
