package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

type ModelSelectorComponent struct {
	list      *SelectListComponent
	endpoints map[string]config.Endpoint
}

func NewModelSelector(endpoints map[string]config.Endpoint, currentEndpoint string) *ModelSelectorComponent {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		if name != currentEndpoint {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := endpoints[currentEndpoint]; ok {
		names = append([]string{currentEndpoint}, names...)
	}

	items := make([]SelectItem, len(names))
	for i, name := range names {
		ep := endpoints[name]
		desc := ep.DefaultModel
		if name == currentEndpoint {
			desc += " (current)"
		}
		items[i] = SelectItem{
			Value:       name,
			Label:       name,
			Description: desc,
		}
	}

	ms := &ModelSelectorComponent{
		list:      NewSelectList(items, nil),
		endpoints: endpoints,
	}
	return ms
}

func (ms *ModelSelectorComponent) SetOnSelect(fn func(endpoint, model string)) {
	ms.list.onSelect = func(item SelectItem) {
		ep := ms.endpoints[item.Value]
		fn(item.Value, ep.DefaultModel)
	}
}

func (ms *ModelSelectorComponent) SetOnCancel(fn func()) {
	ms.list.onCancel = fn
}

func (ms *ModelSelectorComponent) Render(width int) []string {
	return ms.list.Render(width)
}

func (ms *ModelSelectorComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	return ms.list.HandleInput(msg)
}
