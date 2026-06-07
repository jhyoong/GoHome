package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	userPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	expandedBg  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
)

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

// ScrollDown scrolls down by n lines, disabling auto-scroll.
func (c *ChatComponent) ScrollDown(n int) {
	c.scrollTop += n
	c.autoScroll = false
}

// ScrollToBottom re-enables auto-scroll so new content keeps the view at the bottom.
func (c *ChatComponent) ScrollToBottom() {
	c.autoScroll = true
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

// countLines returns the total number of rendered lines for all timeline entries
// at the given maxWidth, without applying scroll constraints.
func (c *ChatComponent) countLines(maxWidth int) int {
	if c.timeline == nil {
		return 0
	}
	count := 0
	for _, e := range *c.timeline {
		switch e.Kind {
		case KindUser:
			count += len(WrapText(e.Text, maxWidth-len("you: ")-2))
		case KindAssistant:
			lines := RenderMarkdown(e.Text, maxWidth-2)
			if len(lines) == 0 {
				lines = WrapText(e.Text, maxWidth-2)
			}
			count += len(lines)
		case KindThinking:
			if e.Expanded {
				lines := RenderMarkdown(e.Text, maxWidth-4)
				if len(lines) == 0 {
					lines = WrapText(e.Text, maxWidth-4)
				}
				count += 1 + len(lines)
			} else {
				count++
			}
		case KindTool:
			count++
			if e.Expanded {
				if e.Text != "" {
					count += len(WrapText("args: "+e.Text, maxWidth-7))
				}
				if e.ToolResult != "" {
					count++ // "result:" line
					count += len(WrapText(e.ToolResult, maxWidth-9))
				}
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

	// Render all entries into lines.
	var all []string
	for i, e := range *c.timeline {
		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		var entryLines []string
		switch e.Kind {
		case KindUser:
			prefix := userPrefix.Render("you:")
			text := WrapText(e.Text, maxWidth-len("you: ")-2)
			for j, l := range text {
				if j == 0 {
					entryLines = append(entryLines, marker+prefix+" "+l)
				} else {
					entryLines = append(entryLines, "      "+l)
				}
			}

		case KindAssistant:
			mdLines := RenderMarkdown(e.Text, maxWidth-2)
			if len(mdLines) == 0 {
				mdLines = WrapText(e.Text, maxWidth-2)
			}
			for j, l := range mdLines {
				if j == 0 {
					entryLines = append(entryLines, marker+l)
				} else {
					entryLines = append(entryLines, "  "+l)
				}
			}

		case KindThinking:
			if e.Expanded {
				mdLines := RenderMarkdown(e.Text, maxWidth-4)
				if len(mdLines) == 0 {
					mdLines = WrapText(e.Text, maxWidth-4)
				}
				entryLines = append(entryLines, marker+expandedBg.Render(ansiDim+ansiItalic+"Thinking..."+ansiReset))
				for _, l := range mdLines {
					entryLines = append(entryLines, expandedBg.Render("    "+ansiDim+ansiItalic+l+ansiReset))
				}
			} else {
				label := "Thinking..."
				if n := strings.Count(strings.TrimSpace(e.Text), "\n"); n > 0 {
					label = fmt.Sprintf("Thinking... (%d lines)", n+1)
				}
				entryLines = append(entryLines, marker+ansiDim+ansiItalic+label+ansiReset)
			}

		case KindTool:
			line := renderToolLine(e, maxWidth-2)
			entryLines = append(entryLines, marker+line)
			if e.Expanded {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
						entryLines = append(entryLines, expandedBg.Render("       "+l))
					}
				}
				if e.ToolResult != "" {
					entryLines = append(entryLines, expandedBg.Render("       result:"))
					for _, l := range WrapText(e.ToolResult, maxWidth-9) {
						entryLines = append(entryLines, expandedBg.Render("         "+l))
					}
				}
			}

		case KindNotice:
			line := noticeStyle.Render(fmt.Sprintf("[notice] %s", e.Text))
			entryLines = append(entryLines, marker+line)
		}

		all = append(all, entryLines...)
	}

	// Apply scroll and height constraints.
	total := len(all)
	if c.maxHeight <= 0 || total <= c.maxHeight {
		return all
	}

	if c.autoScroll {
		// Show last maxHeight lines.
		return all[total-c.maxHeight:]
	}

	// Clamp scrollTop to valid range.
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

// renderToolLine builds the collapsed single-line representation of a tool entry.
func renderToolLine(e TimelineEntry, maxWidth int) string {
	arg := shortArg(e.Text)
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

	line := st.Render(fmt.Sprintf("[tool] %s", e.ToolName))
	if arg != "" {
		line += " " + arg
	}
	if e.Status == "error" && result != "" {
		line += "  ->  ERROR: " + result
	} else if result != "" {
		line += "  ->  " + result
	}
	if VisualWidth(StripAnsi(line)) > maxWidth {
		line = TruncateText(line, maxWidth)
	}
	return line
}
