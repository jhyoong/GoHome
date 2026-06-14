package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

type ModelSelectorComponent struct {
	list    *SelectListComponent
	configs map[string]config.ModelConfig
}

func NewModelSelector(configs map[string]config.ModelConfig, current string) *ModelSelectorComponent {
	names := make([]string, 0, len(configs))
	for name := range configs {
		if name != current {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := configs[current]; ok {
		names = append([]string{current}, names...)
	}

	items := make([]SelectItem, len(names))
	for i, name := range names {
		ep := configs[name]
		desc := ep.ModelName
		if name == current {
			desc += " (current)"
		}
		items[i] = SelectItem{
			Value:       name,
			Label:       name,
			Description: desc,
		}
	}

	ms := &ModelSelectorComponent{
		list:    NewSelectList(items, nil),
		configs: configs,
	}
	return ms
}

func (ms *ModelSelectorComponent) SetOnSelect(fn func(endpoint, model string)) {
	ms.list.onSelect = func(item SelectItem) {
		ep := ms.configs[item.Value]
		fn(item.Value, ep.ModelName)
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
