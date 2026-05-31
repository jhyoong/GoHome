package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	userPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
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
		case "user":
			prefix := userPrefix.Render("you:")
			text := WrapText(e.Text, maxWidth-len("you: ")-2)
			for j, l := range text {
				if j == 0 {
					entryLines = append(entryLines, marker+prefix+" "+l)
				} else {
					entryLines = append(entryLines, "      "+l)
				}
			}

		case "assistant":
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

		case "tool":
			line := renderToolLine(e, maxWidth-2)
			entryLines = append(entryLines, marker+line)
			if e.Expanded {
				if e.Text != "" {
					for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
						entryLines = append(entryLines, "       "+l)
					}
				}
				if e.ToolResult != "" {
					entryLines = append(entryLines, "       result:")
					for _, l := range WrapText(e.ToolResult, maxWidth-9) {
						entryLines = append(entryLines, "         "+l)
					}
				}
			}

		case "notice":
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
	line := toolStyle.Render(fmt.Sprintf("[tool] %s", e.ToolName))
	if arg != "" {
		line += " " + arg
	}
	if result != "" {
		line += "  ->  " + result
	}
	if VisualWidth(StripAnsi(line)) > maxWidth {
		line = TruncateText(line, maxWidth)
	}
	return line
}
