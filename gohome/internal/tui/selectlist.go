package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var selectHighlight = lipgloss.NewStyle().Reverse(true)
var selectDeleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

type SelectItem struct {
	Value       string
	Label       string
	Description string
	labelLower  string
	descLower   string
}

type SelectListComponent struct {
	allItems      []SelectItem
	filtered      []SelectItem
	selected      int
	query         string
	maxVisible    int
	confirmDelete int
	onSelect      func(SelectItem)
	onCancel      func()
	onDelete      func(SelectItem)
}

func initSelectItems(items []SelectItem) []SelectItem {
	for i := range items {
		items[i].labelLower = strings.ToLower(items[i].Label)
		items[i].descLower = strings.ToLower(items[i].Description)
	}
	return items
}

func NewSelectList(items []SelectItem, onSelect func(SelectItem)) *SelectListComponent {
	items = initSelectItems(items)
	sl := &SelectListComponent{
		allItems:      items,
		filtered:      append([]SelectItem{}, items...),
		maxVisible:    10,
		confirmDelete: -1,
		onSelect:      onSelect,
	}
	return sl
}

func (sl *SelectListComponent) Render(width int) []string {
	var lines []string
	lines = append(lines, "> "+sl.query+"\x1b[7m \x1b[0m")

	if len(sl.filtered) == 0 {
		lines = append(lines, "  (no matches)")
		return lines
	}

	total := len(sl.filtered)
	vis := sl.maxVisible
	if vis > total {
		vis = total
	}
	start := sl.selected - vis/2
	if start < 0 {
		start = 0
	}
	if start+vis > total {
		start = total - vis
	}
	if start < 0 {
		start = 0
	}
	end := start + vis

	showDesc := width > 40

	for i := start; i < end; i++ {
		item := sl.filtered[i]
		prefix := "   "
		if i == sl.selected {
			prefix = "-> "
		}

		label := item.Label
		line := prefix + label

		if showDesc && item.Description != "" {
			descMaxW := width - VisualWidth(line) - 2
			if descMaxW > 0 {
				desc := item.Description
				if VisualWidth(desc) > descMaxW {
					desc = TruncateText(desc, descMaxW)
				}
				padding := width - VisualWidth(line) - VisualWidth(desc)
				if padding < 1 {
					padding = 1
				}
				line = line + strings.Repeat(" ", padding) + desc
			}
		}

		if sl.confirmDelete == i {
			line = selectDeleteStyle.Render(prefix + label + "  delete? d to confirm")
		} else if i == sl.selected {
			line = selectHighlight.Render(line)
		}

		lines = append(lines, line)
	}

	if total > vis {
		lines = append(lines, fmt.Sprintf("  (%d/%d)", sl.selected+1, total))
	}

	return lines
}

func (sl *SelectListComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	if sl.confirmDelete >= 0 {
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'd' {
			if sl.onDelete != nil && sl.confirmDelete < len(sl.filtered) {
				sl.onDelete(sl.filtered[sl.confirmDelete])
			}
		}
		sl.confirmDelete = -1
		return nil
	}

	switch msg.Type {
	case tea.KeyUp:
		sl.selected--
		if sl.selected < 0 {
			sl.selected = len(sl.filtered) - 1
		}
		if sl.selected < 0 {
			sl.selected = 0
		}
	case tea.KeyDown:
		if len(sl.filtered) > 0 {
			sl.selected = (sl.selected + 1) % len(sl.filtered)
		}
	case tea.KeyEnter:
		if sl.onSelect != nil && sl.selected < len(sl.filtered) {
			sl.onSelect(sl.filtered[sl.selected])
		}
	case tea.KeyEsc:
		if sl.onCancel != nil {
			sl.onCancel()
		}
	case tea.KeyBackspace:
		if len(sl.query) > 0 {
			sl.query = sl.query[:len(sl.query)-1]
			sl.applyFilter()
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r == 'd' && sl.onDelete != nil && sl.query == "" {
				sl.confirmDelete = sl.selected
				return nil
			}
		}
		sl.query += string(msg.Runes)
		sl.applyFilter()
	}
	return nil
}

func (sl *SelectListComponent) applyFilter() {
	if sl.query == "" {
		sl.filtered = append([]SelectItem{}, sl.allItems...)
		sl.selected = 0
		return
	}
	q := strings.ToLower(sl.query)
	var out []SelectItem
	for _, item := range sl.allItems {
		if strings.Contains(item.labelLower, q) ||
			strings.Contains(item.descLower, q) {
			out = append(out, item)
		}
	}
	sl.filtered = out
	sl.selected = 0
}

func (sl *SelectListComponent) SetItems(items []SelectItem) {
	sl.allItems = initSelectItems(items)
	sl.applyFilter()
}

func (sl *SelectListComponent) SetQuery(q string) {
	sl.query = q
	sl.applyFilter()
}
