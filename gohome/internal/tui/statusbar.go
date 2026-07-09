package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

var yoloStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

// formatTokens formats a token count as a human-friendly string.
// Values >= 1000 are rendered as "12.3k" (one decimal place).
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	k := float64(n) / 1000.0
	// Round to 1 decimal.
	k = math.Round(k*10) / 10
	return fmt.Sprintf("%.1fk", k)
}

// usedTokens returns the Input+Output token total for a session.
// Cache reads/writes are excluded from the bar's "used" so the number
// reflects new work sent to the model in this session.
func usedTokens(u common.Usage) int {
	return u.InputTokens + u.OutputTokens
}

// statusBar renders the one-line bottom status bar for the given model state.
func (m *Model) statusBar() string {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return ""
	}

	modelDisplay := m.configName
	if modelDisplay == "" {
		modelDisplay = m.modelName
	}
	if modelDisplay == "" {
		modelDisplay = "?"
	}
	if m.modelName != "" && m.configName != "" && m.modelName != m.configName {
		modelDisplay = m.configName + " (" + m.modelName + ")"
	}

	used := usedTokens(sv.Usage)
	total := m.contextWindow

	bar := progressBar(used, total, 10)

	pct := 0
	if total > 0 {
		pct = int(float64(used) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
	}

	project := m.projectDir
	if project == "" {
		project = m.focused
	}
	if m.gitBranch != "" {
		project += " (" + m.gitBranch + ")"
	}

	line := fmt.Sprintf("%s · %s · %s %s/%s (%d%%)",
		project,
		modelDisplay,
		bar,
		formatTokens(used),
		formatTokens(total),
		pct,
	)

	if m.yolo {
		line += " · " + yoloStyle.Render("[YOLO]")
	}

	var right string
	if !m.chat.IsAutoScroll() {
		currentLine, totalLines := m.chat.ScrollInfo(m.winW)
		scrollPct := 0
		if totalLines > 0 {
			scrollPct = int(float64(currentLine) / float64(totalLines) * 100)
		}
		right = fmt.Sprintf("Ln %d/%d (%d%%)", currentLine+1, totalLines, scrollPct)
	}

	if right != "" {
		leftW := VisualWidth(StripAnsi(line))
		rightW := len(right)
		gap := m.winW - leftW - rightW
		if gap < 1 {
			gap = 1
		}
		line += fmt.Sprintf("%*s%s", gap, "", right)
	}

	return m.theme.StatusBar.Render(line)
}
