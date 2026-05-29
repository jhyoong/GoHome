package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	charFilled = "▓"
	charEmpty  = "░"
)

// barCells computes the number of filled cells for a progress bar of the given
// width. The ratio used/total is clamped to [0, 1]; total <= 0 gives 0.
// This helper is unexported and used by progressBar; it is also tested directly.
func barCells(used, total, width int) int {
	if total <= 0 || width <= 0 {
		return 0
	}
	ratio := float64(used) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return filled
}

// progressBar renders a width-cell bar using filled/empty Unicode block chars.
// The bar colour reflects how full it is:
//   - ratio <= 0.50  -> green
//   - 0.50 < ratio <= 0.80 -> yellow
//   - ratio > 0.80          -> red
func progressBar(used, total, width int) string {
	if width <= 0 {
		return ""
	}

	filled := barCells(used, total, width)
	empty := width - filled

	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
	}

	var color lipgloss.Color
	switch {
	case ratio <= 0.50:
		color = lipgloss.Color("2") // green
	case ratio <= 0.80:
		color = lipgloss.Color("3") // yellow
	default:
		color = lipgloss.Color("1") // red
	}

	bar := strings.Repeat(charFilled, filled) + strings.Repeat(charEmpty, empty)
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}
