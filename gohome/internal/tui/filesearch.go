package tui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxFileResults  = 50
	maxVisibleFiles = 10
)

var fileSearchBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

// ScoredResult is a file path with a relevance score.
// Exported so that tests in tui_test (external package) can construct them.
type ScoredResult struct {
	Path  string
	Score int
}

// FileSearchResultMsg is sent when fd/find results arrive.
// Exported so tui_test (external package) can construct it for Task 7 tests.
type FileSearchResultMsg struct {
	Query   string
	Results []ScoredResult
}

// FileSearchPopup renders a floating list of file search results.
type FileSearchPopup struct {
	query    string
	results  []ScoredResult
	selected int
	offset   int // index of the first visible row
	visible  bool
}

func NewFileSearchPopup() *FileSearchPopup {
	return &FileSearchPopup{}
}

func (p *FileSearchPopup) MoveDown() {
	if len(p.results) == 0 {
		return
	}
	p.selected = (p.selected + 1) % len(p.results)
	p.clampOffset()
}

func (p *FileSearchPopup) MoveUp() {
	if len(p.results) == 0 {
		return
	}
	p.selected--
	if p.selected < 0 {
		p.selected = len(p.results) - 1
	}
	p.clampOffset()
}

// clampOffset scrolls the window so the selected row is visible.
func (p *FileSearchPopup) clampOffset() {
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+maxVisibleFiles {
		p.offset = p.selected - maxVisibleFiles + 1
	}
}

func (p *FileSearchPopup) SelectedPath() string {
	if len(p.results) == 0 || p.selected >= len(p.results) {
		return ""
	}
	return p.results[p.selected].Path
}

func (p *FileSearchPopup) SetResults(query string, results []ScoredResult) {
	if query != p.query {
		return // stale
	}
	p.results = results
	p.selected = 0
	p.offset = 0
	p.visible = len(results) > 0
}

func (p *FileSearchPopup) Hide() {
	p.visible = false
	p.results = nil
	p.selected = 0
	p.offset = 0
	p.query = ""
}

func (p *FileSearchPopup) Render(width int) []string {
	if !p.visible || len(p.results) == 0 {
		return nil
	}

	end := p.offset + maxVisibleFiles
	if end > len(p.results) {
		end = len(p.results)
	}
	visible := p.results[p.offset:end]

	topBorder := fileSearchBorder.Render(strings.Repeat("─", width))
	var lines []string
	lines = append(lines, topBorder)
	for j, r := range visible {
		i := p.offset + j
		prefix := "  "
		if i == p.selected {
			prefix = "> "
		}
		line := prefix + r.Path
		if VisualWidth(line) > width {
			line = TruncateText(line, width)
		}
		if i == p.selected {
			line = "\x1b[7m" + line + "\x1b[0m"
		}
		lines = append(lines, line)
	}
	if len(p.results) > maxVisibleFiles {
		lines = append(lines, fmt.Sprintf("  ... %d more", len(p.results)-maxVisibleFiles))
	}
	botBorder := fileSearchBorder.Render(strings.Repeat("─", width))
	lines = append(lines, botBorder)
	return lines
}

func searchFilesCmd(query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, lookErr := exec.LookPath("fd")
		name, args := fileSearchArgs(query, lookErr == nil)
		cmd := exec.CommandContext(ctx, name, args...)

		out, err := cmd.Output()
		if err != nil {
			return FileSearchResultMsg{Query: query, Results: nil}
		}

		rawLines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(rawLines) == 1 && rawLines[0] == "" {
			return FileSearchResultMsg{Query: query, Results: nil}
		}
		return FileSearchResultMsg{Query: query, Results: rankFileResults(query, rawLines)}
	}
}

// fileSearchArgs returns the command and arguments for a file search.
// "--" stops fd from reading a query that starts with "-" as a flag.
func fileSearchArgs(query string, haveFd bool) (string, []string) {
	if haveFd {
		return "fd", []string{"--type", "f", "--color", "never", "--", query}
	}
	return "find", []string{".", "-type", "f", "-not", "-path", "*/.git/*", "-name", "*" + query + "*"}
}

// rankFileResults scores all paths, then keeps the best maxFileResults.
func rankFileResults(query string, paths []string) []ScoredResult {
	results := scoreResults(query, paths)
	if len(results) > maxFileResults {
		results = results[:maxFileResults]
	}
	return results
}

func scoreResults(query string, paths []string) []ScoredResult {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var results []ScoredResult
	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		lp := strings.ToLower(p)
		var score int
		switch {
		case base == q:
			score = 0
		case strings.HasPrefix(base, q):
			score = 20
		case strings.Contains(base, q):
			score = 50
		case strings.Contains(lp, q):
			score = 70
		default:
			continue
		}
		results = append(results, ScoredResult{Path: p, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score < results[j].Score
		}
		return len(results[i].Path) < len(results[j].Path)
	})
	return results
}
