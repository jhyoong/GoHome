package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

type SessionBrowserComponent struct {
	list     *SelectListComponent
	listings []session.Listing
}

func NewSessionBrowser(listings []session.Listing) *SessionBrowserComponent {
	items := make([]SelectItem, len(listings))
	for i, l := range listings {
		label := l.Title
		if label == "" {
			label = l.ID
		}
		if len([]rune(label)) > 40 {
			label = string([]rune(label)[:40])
		}
		items[i] = SelectItem{
			Value:       l.ID,
			Label:       label,
			Description: relativeTime(l.LastActive),
		}
	}
	sb := &SessionBrowserComponent{
		list:     NewSelectList(items, nil),
		listings: listings,
	}
	return sb
}

func (sb *SessionBrowserComponent) SetOnSelect(fn func(id string)) {
	sb.list.onSelect = func(item SelectItem) {
		fn(item.Value)
	}
}

func (sb *SessionBrowserComponent) SetOnCancel(fn func()) {
	sb.list.onCancel = fn
}

func (sb *SessionBrowserComponent) SetOnDelete(fn func(listing session.Listing)) {
	sb.list.onDelete = func(item SelectItem) {
		for _, l := range sb.listings {
			if l.ID == item.Value {
				fn(l)
				break
			}
		}
		var remaining []session.Listing
		for _, l := range sb.listings {
			if l.ID != item.Value {
				remaining = append(remaining, l)
			}
		}
		sb.listings = remaining
		items := make([]SelectItem, len(remaining))
		for i, l := range remaining {
			label := l.Title
			if label == "" {
				label = l.ID
			}
			if len([]rune(label)) > 40 {
				label = string([]rune(label)[:40])
			}
			items[i] = SelectItem{
				Value:       l.ID,
				Label:       label,
				Description: relativeTime(l.LastActive),
			}
		}
		sb.list.SetItems(items)
	}
}

func (sb *SessionBrowserComponent) Render(width int) []string {
	return sb.list.Render(width)
}

func (sb *SessionBrowserComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	return sb.list.HandleInput(msg)
}

func (sb *SessionBrowserComponent) SetFilter(q string) {
	sb.list.SetQuery(q)
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}
