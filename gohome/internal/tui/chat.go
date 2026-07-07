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
	if e.cacheValid(maxWidth) {
		return len(e.cachedLines)
	}
	switch e.Kind {
	case KindUser:
		return len(WrapText(e.Text, maxWidth-3))
	case KindAssistant:
		lines := RenderMarkdown(e.Text, maxWidth-2)
		if len(lines) == 0 {
			if strings.TrimSpace(e.Text) == "" {
				return 0
			}
			lines = WrapText(e.Text, maxWidth-2)
		}
		return len(lines)
	case KindThinking:
		trimmed := strings.TrimSpace(e.Text)
		if trimmed == "" {
			return 0
		}
		return len(WrapText(trimmed, maxWidth-2))
	case KindTool:
		rendered := c.renderEntry(e, maxWidth, "  ")
		return len(rendered)
	case KindNotice:
		return 1
	case KindStats:
		return 1
	}
	return 1
}

func needsSeparator(kind, lastVisibleKind string) bool {
	return kind != KindAssistant || lastVisibleKind != KindAssistant
}

// countLines returns the total number of rendered lines for all timeline entries
// at the given maxWidth. Uses cached line counts when available.
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
				count++ // separator blank line
			}
			hasOutput = true
			lastVisibleKind = e.Kind
		}

		if e.cacheValid(maxWidth) {
			count += len(e.cachedLines)
			continue
		}
		switch e.Kind {
		case KindUser:
			count += len(WrapText(e.Text, maxWidth-4))
		case KindAssistant:
			lines := RenderMarkdown(e.Text, maxWidth-2)
			if len(lines) == 0 {
				if strings.TrimSpace(e.Text) != "" {
					lines = WrapText(e.Text, maxWidth-2)
				}
			}
			count += len(lines)
		case KindThinking:
			trimmed := strings.TrimSpace(e.Text)
			if trimmed != "" {
				count += len(WrapText(trimmed, maxWidth-2))
			}
		case KindTool:
			rendered := c.renderEntry(e, maxWidth, "  ")
			count += len(rendered)
		case KindNotice:
			count++
		case KindStats:
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
	hasOutput := false
	lastVisibleKind := ""
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

		if len(e.cachedLines) > 0 {
			if hasOutput && needsSeparator(e.Kind, lastVisibleKind) {
				all = append(all, "")
			}
			hasOutput = true
			lastVisibleKind = e.Kind
		}

		all = append(all, e.cachedLines...)
	}

	// Apply scroll and height constraints.
	total := len(all)
	var visible []string
	if c.maxHeight <= 0 || total <= c.maxHeight {
		visible = all
	} else if c.autoScroll {
		visible = all[total-c.maxHeight:]
	} else {
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
		visible = all[c.scrollTop:end]
	}

	return visible
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
