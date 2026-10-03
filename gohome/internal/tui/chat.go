package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	diffBoxDefault = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
	diffBoxDenied = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("1")).
			Padding(0, 1)
)

// toolBlockStyle returns a lipgloss style for tool blocks based on status.
// The block has a dark background and a left thick border whose color
// reflects the tool execution status.
func toolBlockStyle(status string) lipgloss.Style {
	var borderColor lipgloss.Color
	switch status {
	case "error":
		borderColor = lipgloss.Color("1") // red
	case "success":
		borderColor = lipgloss.Color("2") // green
	default:
		borderColor = lipgloss.Color("3") // yellow (pending)
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderRight(false).
		BorderTop(false).
		BorderBottom(false).
		BorderForeground(borderColor)
}

// ChatComponent renders a timeline of entries with markdown support and scrolling.
type ChatComponent struct {
	timeline   *[]TimelineEntry
	scrollTop  int
	maxHeight  int
	autoScroll bool
	cursor     int
}

// NewChat creates a new ChatComponent backed by the given timeline pointer.
func NewChat(timeline *[]TimelineEntry, maxHeight int) *ChatComponent {
	return &ChatComponent{
		timeline:   timeline,
		maxHeight:  maxHeight,
		autoScroll: true,
		cursor:     -1,
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

// ScrollDown scrolls down by n lines. If the viewport reaches the bottom,
// auto-scroll is re-enabled so new content is tracked automatically.
func (c *ChatComponent) ScrollDown(n int) {
	c.scrollTop += n
	c.autoScroll = false
}

// ReEnableAutoScrollIfAtBottom checks whether the viewport has scrolled to the
// bottom of the content and re-enables auto-scroll if so. Call after ScrollDown
// or mouse wheel down. maxWidth is needed to compute the total line count.
func (c *ChatComponent) ReEnableAutoScrollIfAtBottom(maxWidth int) {
	if c.autoScroll || c.timeline == nil || c.maxHeight <= 0 {
		return
	}
	total := c.countLines(maxWidth)
	if total <= c.maxHeight || c.scrollTop >= total-c.maxHeight {
		c.autoScroll = true
	}
}

// ScrollToBottom re-enables auto-scroll so new content keeps the view at the bottom.
func (c *ChatComponent) ScrollToBottom() {
	c.autoScroll = true
}

// IsAutoScroll reports whether auto-scroll is active.
func (c *ChatComponent) IsAutoScroll() bool { return c.autoScroll }

// ScrollTop returns the current scroll offset (top visible line index).
func (c *ChatComponent) ScrollTop() int { return c.scrollTop }

// ScrollInfo returns the line position the user has reached and the total
// line count at the given width. When the cursor is active it returns the
// bottom line of the cursor entry; otherwise it returns the bottom line of
// the viewport. Used by the status bar to display scroll position.
func (c *ChatComponent) ScrollInfo(maxWidth int) (currentLine, totalLines int) {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return 0, 0
	}
	totalLines = c.countLines(maxWidth)
	if totalLines <= c.maxHeight {
		return totalLines, totalLines
	}

	if c.cursor >= 0 && c.cursor < len(*c.timeline) {
		pos := 0
		hasOutput := false
		lastVisibleKind := ""
		for i := 0; i <= c.cursor; i++ {
			e := &(*c.timeline)[i]
			n := c.entryLineCount(e, maxWidth)
			if n > 0 {
				if hasOutput && needsSeparator(e.Kind, lastVisibleKind) {
					pos++
				}
				hasOutput = true
				lastVisibleKind = e.Kind
			}
			pos += n
		}
		return pos, totalLines
	}

	scrollTop := c.scrollTop
	if c.autoScroll {
		scrollTop = totalLines - c.maxHeight
	} else if scrollTop > totalLines-c.maxHeight {
		scrollTop = totalLines - c.maxHeight
	}
	if scrollTop < 0 {
		scrollTop = 0
	}
	viewportBottom := scrollTop + c.maxHeight
	if viewportBottom > totalLines {
		viewportBottom = totalLines
	}
	return viewportBottom, totalLines
}

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
	hasOutput := false
	lastVisibleKind := ""
	for i := 0; i < c.cursor; i++ {
		e := &(*c.timeline)[i]
		n := c.entryLineCount(e, maxWidth)
		if n > 0 {
			if hasOutput && needsSeparator(e.Kind, lastVisibleKind) {
				cursorTop++ // separator
			}
			hasOutput = true
			lastVisibleKind = e.Kind
		}
		cursorTop += n
	}
	cursorEntry := &(*c.timeline)[c.cursor]
	cursorHeight := c.entryLineCount(cursorEntry, maxWidth)
	if cursorHeight > 0 && hasOutput && needsSeparator(cursorEntry.Kind, lastVisibleKind) {
		cursorTop++ // separator before cursor entry
	}

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
	return len(c.entryLines(e, maxWidth))
}

// entryLines returns the cached rendered lines for e, rendering and caching
// them first if the cache is stale. Lines are rendered with a blank marker;
// Render swaps in the cursor marker at output time so cursor moves do not
// invalidate the cache.
func (c *ChatComponent) entryLines(e *TimelineEntry, maxWidth int) []string {
	if !e.cacheValid(maxWidth) {
		lines := c.renderEntry(e, maxWidth, "  ")
		if lines == nil {
			lines = []string{}
		}
		e.cachedLines = lines
		e.cachedWidth = maxWidth
		e.cachedExpanded = e.Expanded
		e.cachedText = e.Text
		e.cachedResult = e.ToolResult
		e.cachedDiffStatus = e.Status
	}
	return e.cachedLines
}

func needsSeparator(kind, lastVisibleKind string) bool {
	return kind != KindAssistant || lastVisibleKind != KindAssistant
}

// countLines returns the total number of rendered lines for all timeline entries
// at the given maxWidth. Delegates entirely to entryLineCount to avoid
// double-rendering.
func (c *ChatComponent) countLines(maxWidth int) int {
	if c.timeline == nil {
		return 0
	}
	count := 0
	hasOutput := false
	lastVisibleKind := ""
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		n := c.entryLineCount(e, maxWidth)
		if n > 0 {
			if hasOutput && needsSeparator(e.Kind, lastVisibleKind) {
				count++
			}
			count += n
			hasOutput = true
			lastVisibleKind = e.Kind
		}
	}
	return count
}

// Render converts the current timeline to a slice of display lines, applying
// scroll and height constraints. maxWidth is the terminal column width.
// Uses a two-pass approach: pass 1 computes line offsets via entryLineCount
// (cheap, cache-aware), pass 2 only renders entries overlapping the visible
// window, avoiding work for offscreen entries.
func (c *ChatComponent) Render(maxWidth int) []string {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return nil
	}

	tl := *c.timeline

	// Pass 1: compute cumulative line offsets per entry.
	type entryMeta struct {
		startLine int
		lineCount int
		sepBefore bool
	}
	metas := make([]entryMeta, len(tl))
	runningLine := 0
	hasOutput := false
	lastVisibleKind := ""
	for i := range tl {
		e := &tl[i]
		n := c.entryLineCount(e, maxWidth)
		sep := false
		if n > 0 && hasOutput && needsSeparator(e.Kind, lastVisibleKind) {
			sep = true
			runningLine++
		}
		metas[i] = entryMeta{startLine: runningLine, lineCount: n, sepBefore: sep}
		runningLine += n
		if n > 0 {
			hasOutput = true
			lastVisibleKind = e.Kind
		}
	}
	total := runningLine

	// Determine visible window.
	viewStart, viewEnd := 0, total
	if c.maxHeight > 0 && total > c.maxHeight {
		if c.autoScroll {
			viewStart = total - c.maxHeight
		} else {
			maxScroll := total - c.maxHeight
			if c.scrollTop > maxScroll {
				c.scrollTop = maxScroll
			}
			if c.scrollTop < 0 {
				c.scrollTop = 0
			}
			viewStart = c.scrollTop
		}
		viewEnd = viewStart + c.maxHeight
		if viewEnd > total {
			viewEnd = total
		}
	}

	// Pass 2: render only entries that overlap the visible window.
	var all []string
	allStartLine := -1 // line position of all[0] in the total line space
	for i := range tl {
		e := &tl[i]
		em := metas[i]
		entryEnd := em.startLine + em.lineCount

		if entryEnd <= viewStart || em.startLine >= viewEnd {
			continue
		}

		if em.sepBefore {
			if allStartLine < 0 {
				allStartLine = em.startLine - 1
			}
			all = append(all, "")
		}

		if allStartLine < 0 {
			allStartLine = em.startLine
		}

		lines := c.entryLines(e, maxWidth)
		if i == c.cursor && len(lines) > 0 {
			// Every entry's first line starts with the 2-column marker.
			all = append(all, "> "+strings.TrimPrefix(lines[0], "  "))
			all = append(all, lines[1:]...)
		} else {
			all = append(all, lines...)
		}
	}

	// Trim to visible height by slicing precisely at the viewport boundary.
	// The rendered entries may start before viewStart (partial overlap at top)
	// or extend beyond viewEnd (partial overlap at bottom).
	if c.maxHeight > 0 && len(all) > c.maxHeight {
		if allStartLine < 0 {
			allStartLine = viewStart
		}
		skip := viewStart - allStartLine
		if skip < 0 {
			skip = 0
		}
		end := skip + c.maxHeight
		if end > len(all) {
			end = len(all)
		}
		if skip > len(all) {
			skip = len(all)
		}
		all = all[skip:end]
	}

	// Apply gradient fade to boundary lines when content overflows.
	if total > c.maxHeight && c.maxHeight > 0 && len(all) > 0 {
		effectiveTop := viewStart
		if effectiveTop > 0 {
			all[0] = ansiDim + all[0] + ansiReset
		}
		if viewEnd < total {
			last := len(all) - 1
			all[last] = ansiDim + all[last] + ansiReset
		}
	}

	return all
}

// renderEntry produces the display lines for a single timeline entry.
func (c *ChatComponent) renderEntry(e *TimelineEntry, maxWidth int, marker string) []string {
	var lines []string

	switch e.Kind {
	case KindUser:
		text := WrapText(e.Text, maxWidth-4)
		styled := userBlockStyle.Width(maxWidth - 3).Render(strings.Join(text, "\n"))
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
			if strings.TrimSpace(e.Text) == "" {
				break
			}
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
		trimmed := strings.TrimSpace(e.Text)
		if trimmed == "" {
			break
		}
		wrapped := WrapText(trimmed, maxWidth-2)
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
			var toolLines []string
			line := renderToolSummary(*e, maxWidth-8)
			toolLines = append(toolLines, ansiDim+line+ansiReset)
			if !e.Expanded {
				if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
					for _, pl := range pv {
						for _, wl := range WrapText(pl, maxWidth-15) {
							toolLines = append(toolLines, "  "+ansiDim+wl+ansiReset)
						}
					}
					if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
						hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", total-maxPreviewLines)
						toolLines = append(toolLines, "  "+ansiDim+hint+ansiReset)
					}
				}
			} else {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-11) {
						toolLines = append(toolLines, "  "+ansiDim+l+ansiReset)
					}
				}
				if e.ToolResult != "" {
					toolLines = append(toolLines, "  "+ansiDim+"result:"+ansiReset)
					for _, l := range WrapText(e.ToolResult, maxWidth-13) {
						toolLines = append(toolLines, "    "+ansiDim+l+ansiReset)
					}
				}
			}
			if e.Duration > 0 && e.Status != "pending" {
				durStr := formatDuration(e.Duration)
				toolLines = append(toolLines, ansiDim+"Took "+durStr+ansiReset)
			}
			styled := toolBlockStyle(e.Status).Width(maxWidth - 7).Render(strings.Join(toolLines, "\n"))
			for j, l := range strings.Split(styled, "\n") {
				if j == 0 {
					lines = append(lines, marker+"    "+l)
				} else {
					lines = append(lines, "      "+l)
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
			var toolLines []string
			line := renderToolSummary(*e, maxWidth-4)
			toolLines = append(toolLines, line)
			if !e.Expanded {
				if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
					for _, pl := range pv {
						for _, wl := range WrapText(pl, maxWidth-9) {
							toolLines = append(toolLines, "  "+ansiDim+wl+ansiReset)
						}
					}
					if total := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n")); total > maxPreviewLines {
						hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", total-maxPreviewLines)
						toolLines = append(toolLines, "  "+ansiDim+hint+ansiReset)
					}
				}
			} else {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
						toolLines = append(toolLines, "  "+l)
					}
				}
				if e.ToolResult != "" {
					toolLines = append(toolLines, "  result:")
					for _, l := range WrapText(e.ToolResult, maxWidth-9) {
						toolLines = append(toolLines, "    "+l)
					}
				}
			}
			if e.Duration > 0 && e.Status != "pending" {
				durStr := formatDuration(e.Duration)
				toolLines = append(toolLines, ansiDim+"Took "+durStr+ansiReset)
			}
			styled := toolBlockStyle(e.Status).Width(maxWidth - 4).Render(strings.Join(toolLines, "\n"))
			for j, l := range strings.Split(styled, "\n") {
				if j == 0 {
					lines = append(lines, marker+l)
				} else {
					lines = append(lines, "  "+l)
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

	case KindStats:
		if e.TurnStats != nil {
			line := formatTurnStats(e.TurnStats)
			lines = append(lines, marker+ansiDim+line+ansiReset)
		}
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

// formatDuration formats a duration for display: milliseconds if < 1s,
// otherwise seconds with one decimal place.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// formatTurnStats formats a TurnStatsData into a single-line summary showing
// TPS, token counts, optional cache info, and elapsed time.
func formatTurnStats(s *TurnStatsData) string {
	tps := fmt.Sprintf("%.1f TPS", s.TPS)
	tokens := fmt.Sprintf("%s output, %s input", formatTokens(s.OutputTokens), formatTokens(s.InputTokens))
	if s.CacheReadTokens > 0 || s.CacheWriteTokens > 0 {
		tokens += fmt.Sprintf(" (%s cached)", formatTokens(s.CacheReadTokens+s.CacheWriteTokens))
	}
	elapsed := formatDuration(s.Elapsed)
	return tps + " | " + tokens + " | " + elapsed
}

// extractToolArg parses the JSON input for a tool call and returns the most
// relevant argument for display. Known fields: "command" (shell), "file_path"
// (read/write/edit), "prompt" (subagent). Falls back to shortSummary on parse
// failure or unknown tools.
func extractToolArg(toolName, inputJSON string) string {
	inputJSON = strings.TrimSpace(inputJSON)
	if inputJSON == "" {
		return ""
	}

	var key string
	switch toolName {
	case "shell":
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
// using contextual display (e.g. "$ cmd" for shell, file paths for read/edit/write).
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
	case "shell":
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
