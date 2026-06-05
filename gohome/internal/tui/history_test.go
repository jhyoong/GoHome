package tui

import "testing"

func TestHistoryAddAndBrowse(t *testing.T) {
	h := NewHistory(100)
	h.Add("first")
	h.Add("second")
	h.Add("third")

	h.StartBrowsing("draft")

	got := h.Prev()
	if got != "third" {
		t.Errorf("Prev() = %q, want %q", got, "third")
	}
	got = h.Prev()
	if got != "second" {
		t.Errorf("Prev() = %q, want %q", got, "second")
	}
	got = h.Prev()
	if got != "first" {
		t.Errorf("Prev() = %q, want %q", got, "first")
	}
	got = h.Prev()
	if got != "first" {
		t.Errorf("Prev() at start = %q, want %q", got, "first")
	}
	got = h.Next()
	if got != "second" {
		t.Errorf("Next() = %q, want %q", got, "second")
	}
}

func TestHistoryNextRestoresDraft(t *testing.T) {
	h := NewHistory(100)
	h.Add("one")
	h.StartBrowsing("my draft")
	h.Prev()
	got := h.Next()
	if got != "my draft" {
		t.Errorf("Next() past end = %q, want %q", got, "my draft")
	}
}

func TestHistoryMaxSize(t *testing.T) {
	h := NewHistory(3)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Add("d")

	h.StartBrowsing("")
	h.Prev()
	h.Prev()
	got := h.Prev()
	if got != "b" {
		t.Errorf("oldest after eviction = %q, want %q", got, "b")
	}
	got = h.Prev()
	if got != "b" {
		t.Errorf("past start = %q, want %q", got, "b")
	}
}

func TestHistoryEmpty(t *testing.T) {
	h := NewHistory(100)
	h.StartBrowsing("draft")
	got := h.Prev()
	if got != "draft" {
		t.Errorf("Prev() on empty = %q, want %q", got, "draft")
	}
}

func TestHistoryNoDuplicates(t *testing.T) {
	h := NewHistory(100)
	h.Add("same")
	h.Add("same")
	h.StartBrowsing("")
	h.Prev()
	got := h.Prev()
	if got != "same" {
		t.Errorf("second Prev() = %q, want %q", got, "same")
	}
}
