package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

func TestProgressBarColorThresholds(t *testing.T) {
	// Force ANSI color output so lipgloss emits escape sequences even in
	// a non-TTY test environment. With termenv.ANSI, terminal colors 1/2/3
	// render as \x1b[31m / \x1b[32m / \x1b[33m (basic 3x/4x codes).
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	tests := []struct {
		name    string
		used    int
		total   int
		wantSeq string // ANSI escape sequence fragment
	}{
		{"50% is green", 50, 100, "[32m"},
		{"79% is green", 79, 100, "[32m"},
		{"80% is yellow", 80, 100, "[33m"},
		{"90% is yellow", 90, 100, "[33m"},
		{"95% is yellow", 95, 100, "[33m"},
		{"96% is red", 96, 100, "[31m"},
		{"100% is red", 100, 100, "[31m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bar := progressBar(tc.used, tc.total, 10)
			if !strings.Contains(bar, tc.wantSeq) {
				t.Errorf("progressBar(%d, %d, 10) = %q, want seq %s",
					tc.used, tc.total, bar, tc.wantSeq)
			}
		})
	}
}
