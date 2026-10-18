package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func threeItems() []SelectItem {
	return []SelectItem{
		{Value: "a", Label: "Alpha", Description: "first"},
		{Value: "b", Label: "Beta", Description: "second"},
		{Value: "c", Label: "Charlie", Description: "third"},
	}
}

func TestSelectListRenderShowsItems(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(80)
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (search + 3 items), got %d", len(lines))
	}
	plain := StripAnsi(lines[1])
	if !strings.HasPrefix(plain, "->") {
		t.Errorf("selected item should start with '->': %q", plain)
	}
	if !strings.Contains(plain, "Alpha") {
		t.Errorf("first item should contain 'Alpha': %q", plain)
	}
	plain2 := StripAnsi(lines[2])
	if strings.HasPrefix(plain2, "->") {
		t.Errorf("second item should not start with '->': %q", plain2)
	}
}

func TestSelectListDescriptionShownWhenWide(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(80)
	plain := StripAnsi(lines[1])
	if !strings.Contains(plain, "first") {
		t.Errorf("description should appear at width=80: %q", plain)
	}
}

func TestSelectListDescriptionHiddenWhenNarrow(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(30)
	plain := StripAnsi(lines[1])
	if strings.Contains(plain, "first") {
		t.Errorf("description should be hidden at width=30: %q", plain)
	}
}

func TestSelectListMoveDown(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if sl.selected != 1 {
		t.Errorf("after down, selected=%d, want 1", sl.selected)
	}
}

func TestSelectListMoveUpWraps(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	if sl.selected != 2 {
		t.Errorf("after up from 0, selected=%d, want 2 (wrap)", sl.selected)
	}
}

func TestSelectListMoveDownWraps(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.selected = 2
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if sl.selected != 0 {
		t.Errorf("after down from last, selected=%d, want 0 (wrap)", sl.selected)
	}
}

func TestSelectListEnterCallsOnSelect(t *testing.T) {
	var got SelectItem
	sl := NewSelectList(threeItems(), nil)
	sl.onSelect = func(item SelectItem) { got = item }
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if got.Value != "b" {
		t.Errorf("onSelect got %q, want 'b'", got.Value)
	}
}

func TestSelectListEscapeCallsOnCancel(t *testing.T) {
	called := false
	sl := NewSelectList(threeItems(), nil)
	sl.onCancel = func() { called = true }
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !called {
		t.Error("onCancel should have been called")
	}
}

func TestSelectListFilterByQuery(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if len(sl.filtered) != 1 {
		t.Fatalf("filter 'be' should match 1 item, got %d", len(sl.filtered))
	}
	if sl.filtered[0].Value != "b" {
		t.Errorf("filtered item should be Beta, got %q", sl.filtered[0].Value)
	}
}

func TestSelectListBackspaceClearsFilter(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if len(sl.filtered) != 0 {
		t.Fatalf("filter 'z' should match 0 items, got %d", len(sl.filtered))
	}
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(sl.filtered) != 3 {
		t.Errorf("after backspace, should show all 3 items, got %d", len(sl.filtered))
	}
}

func TestSelectListScrollIndicator(t *testing.T) {
	items := make([]SelectItem, 20)
	for i := range items {
		items[i] = SelectItem{Value: string(rune('a' + i)), Label: string(rune('A' + i))}
	}
	sl := NewSelectList(items, nil)
	sl.maxVisible = 5
	lines := sl.Render(80)
	lastLine := StripAnsi(lines[len(lines)-1])
	if !strings.Contains(lastLine, "/20") {
		t.Errorf("scroll indicator should show /20: %q", lastLine)
	}
}

var ctrlD = tea.KeyMsg{Type: tea.KeyCtrlD}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestSelectListDeleteConfirmation(t *testing.T) {
	deleted := ""
	sl := NewSelectList(threeItems(), nil)
	sl.onDelete = func(item SelectItem) { deleted = item.Value }
	sl.HandleInput(ctrlD)
	if sl.confirmDelete != 0 {
		t.Fatalf("confirmDelete should be 0 (first item), got %d", sl.confirmDelete)
	}
	if deleted != "" {
		t.Error("should not have deleted yet")
	}
	if got := StripAnsi(strings.Join(sl.Render(80), "\n")); !strings.Contains(got, "delete? y to confirm") {
		t.Errorf("missing confirm hint: %q", got)
	}
	sl.HandleInput(runeKey('y'))
	if deleted != "a" {
		t.Errorf("should have deleted 'a', got %q", deleted)
	}
}

func TestSelectListDeleteConfirmCtrlDTwice(t *testing.T) {
	deleted := ""
	sl := NewSelectList(threeItems(), nil)
	sl.onDelete = func(item SelectItem) { deleted = item.Value }
	sl.HandleInput(ctrlD)
	sl.HandleInput(ctrlD)
	if deleted != "a" {
		t.Errorf("should have deleted 'a', got %q", deleted)
	}
}

func TestSelectListDeleteCancelOnOtherKey(t *testing.T) {
	deleted := ""
	sl := NewSelectList(threeItems(), nil)
	sl.onDelete = func(item SelectItem) { deleted = item.Value }
	sl.HandleInput(ctrlD)
	sl.HandleInput(runeKey('d'))
	if deleted != "" {
		t.Error("delete should have been cancelled")
	}
	if sl.confirmDelete != -1 {
		t.Errorf("confirmDelete should be -1 after cancel, got %d", sl.confirmDelete)
	}
}

func TestSelectListDeleteDisabledWhenNoCallback(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(ctrlD)
	if sl.confirmDelete != -1 {
		t.Errorf("confirmDelete should remain -1 when onDelete is nil, got %d", sl.confirmDelete)
	}
}

func TestSelectListDFiltersWithEmptyQuery(t *testing.T) {
	sl := NewSelectList([]SelectItem{
		{Value: "a", Label: "Alpha"},
		{Value: "d", Label: "Delta"},
	}, nil)
	sl.onDelete = func(SelectItem) {}
	sl.HandleInput(runeKey('d'))
	if sl.confirmDelete != -1 {
		t.Fatalf("'d' must not start delete, confirmDelete=%d", sl.confirmDelete)
	}
	if sl.query != "d" {
		t.Fatalf("query = %q, want %q", sl.query, "d")
	}
	if len(sl.filtered) != 1 || sl.filtered[0].Value != "d" {
		t.Errorf("filtered = %+v, want only Delta", sl.filtered)
	}
}

func TestSelectListBackspaceRemovesRune(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.SetQuery("caf\u00e9")
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyBackspace})
	if sl.query != "caf" {
		t.Errorf("query = %q, want %q", sl.query, "caf")
	}
}

func TestSelectListSetQueryFilters(t *testing.T) {
	items := []SelectItem{
		{Value: "a", Label: "alpha"},
		{Value: "b", Label: "beta"},
		{Value: "c", Label: "gamma"},
	}
	sl := NewSelectList(items, nil)
	sl.SetQuery("bet")
	lines := sl.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "beta") {
		t.Error("filtered list should contain 'beta'")
	}
	if strings.Contains(joined, "alpha") {
		t.Error("filtered list should not contain 'alpha'")
	}
	if strings.Contains(joined, "gamma") {
		t.Error("filtered list should not contain 'gamma'")
	}
}

func TestSelectList_WrapsLongDescriptionAtNarrowWidth(t *testing.T) {
	sl := NewSelectList([]SelectItem{
		{Value: "env", Label: "Environment variable", Description: "read from an env var at runtime"},
	}, nil)
	lines := sl.Render(50)
	// Query line plus the item over two lines.
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), lines)
	}
	var descParts []string
	for _, l := range lines[1:] {
		plain := StripAnsi(l)
		if VisualWidth(plain) > 50 {
			t.Errorf("line wider than 50: %q", plain)
		}
		descParts = append(descParts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(plain), "->")))
	}
	joined := strings.Join(descParts, " ")
	if !strings.Contains(joined, "read from an env var at runtime") {
		t.Errorf("full description missing: %q", joined)
	}
	// Continuation line is indented under the description column.
	first := StripAnsi(lines[1])
	col := strings.Index(first, "read")
	cont := StripAnsi(lines[2])
	if col < 0 || !strings.HasPrefix(cont, strings.Repeat(" ", col)) || strings.TrimSpace(cont) == "" {
		t.Errorf("continuation not aligned to description column %d: %q", col, cont)
	}
}
