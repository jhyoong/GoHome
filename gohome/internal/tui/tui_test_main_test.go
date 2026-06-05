package tui_test

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain sets up the test environment for the tui package tests.
// It disables colour output (termenv.Ascii) so that snapshot golden files
// are stable across machines and colour profiles.
func TestMain(m *testing.M) {
	// Strip all ANSI colour codes from lipgloss output so that golden-file
	// snapshots are deterministic regardless of the host terminal.
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
