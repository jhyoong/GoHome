package tui

import (
	"testing"
)

// TestBarCells exercises the cell-count math at several key ratios.
func TestBarCells(t *testing.T) {
	const width = 10

	tests := []struct {
		name  string
		used  int
		total int
		want  int
	}{
		{"ratio 0 (0%)", 0, 100, 0},
		{"ratio 0.25 (25%)", 25, 100, 2}, // 0.25 * 10 = 2.5 -> 2
		{"ratio 0.5 (50%)", 50, 100, 5},
		{"ratio 0.8 (80%)", 80, 100, 8},
		{"ratio 1.0 (100%)", 100, 100, 10},
		{"over 100%", 200, 100, 10}, // clamped to width
		{"total zero", 50, 0, 0},
		{"negative total", 50, -1, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := barCells(tc.used, tc.total, width)
			if got != tc.want {
				t.Errorf("barCells(%d, %d, %d) = %d; want %d",
					tc.used, tc.total, width, got, tc.want)
			}
		})
	}
}

// TestProgressBarWidth verifies that the visible bar is always exactly width
// cells wide (ANSI stripped). We compare rune count of the raw characters.
func TestProgressBarWidth(t *testing.T) {
	const width = 10
	tests := []struct{ used, total int }{
		{0, 100},
		{50, 100},
		{100, 100},
		{200, 100},
		{0, 0},
	}
	for _, tc := range tests {
		bar := progressBar(tc.used, tc.total, width)
		// Strip lipgloss ANSI by counting only the block chars.
		count := 0
		for _, r := range bar {
			if r == '▓' || r == '░' {
				count++
			}
		}
		if count != width {
			t.Errorf("progressBar(%d, %d, %d): visible width=%d, want %d (bar=%q)",
				tc.used, tc.total, width, count, width, bar)
		}
	}
}

// TestProgressBarZeroWidth returns empty string when width is 0.
func TestProgressBarZeroWidth(t *testing.T) {
	got := progressBar(50, 100, 0)
	if got != "" {
		t.Errorf("progressBar with width=0 should return empty, got %q", got)
	}
}
