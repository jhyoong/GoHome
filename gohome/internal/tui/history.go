package tui

type History struct {
	entries []string
	maxSize int
	pos     int
	draft   string
}

func NewHistory(maxSize int) *History {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &History{
		maxSize: maxSize,
		pos:     -1,
	}
}

func (h *History) Add(text string) {
	if text == "" {
		return
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == text {
		return
	}
	h.entries = append(h.entries, text)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[1:]
	}
	h.pos = -1
}

func (h *History) StartBrowsing(draft string) {
	h.draft = draft
	h.pos = len(h.entries)
}

func (h *History) Browsing() bool {
	return h.pos >= 0
}

func (h *History) Prev() string {
	if len(h.entries) == 0 {
		return h.draft
	}
	if h.pos > 0 {
		h.pos--
	}
	return h.entries[h.pos]
}

func (h *History) Next() string {
	if len(h.entries) == 0 {
		return h.draft
	}
	h.pos++
	if h.pos >= len(h.entries) {
		h.pos = len(h.entries)
		return h.draft
	}
	return h.entries[h.pos]
}

func (h *History) StopBrowsing() {
	h.pos = -1
}
