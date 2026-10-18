package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestScoreResults_ExactFilename(t *testing.T) {
	results := scoreResults("main.go", []string{"cmd/main.go", "internal/tui/model.go", "main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Path != "main.go" {
		t.Errorf("best match: got %q, want %q", results[0].Path, "main.go")
	}
}

func TestScoreResults_StartsWithFilename(t *testing.T) {
	results := scoreResults("mod", []string{"internal/tui/model.go", "go.mod", "modfile/x.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	// "go.mod" starts with "mod" in filename -- should rank above substring-in-path
	// But "modfile/x.go" has "mod" at start of a directory name, not filename.
	// The file whose basename starts with "mod" should come first.
}

func TestScoreResults_SubstringInFilename(t *testing.T) {
	results := scoreResults("odel", []string{"internal/tui/model.go", "cmd/main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Path != "internal/tui/model.go" {
		t.Errorf("expected model.go first, got %q", results[0].Path)
	}
}

func TestScoreResults_ShorterPathBreaksTie(t *testing.T) {
	results := scoreResults("main.go", []string{"a/b/c/main.go", "a/main.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	if results[0].Path != "a/main.go" {
		t.Errorf("shorter path should win tie: got %q", results[0].Path)
	}
}

func TestScoreResults_EmptyQuery(t *testing.T) {
	results := scoreResults("", []string{"a.go", "b.go"})
	if len(results) != 0 {
		t.Errorf("empty query should return no results, got %d", len(results))
	}
}

func TestFileSearchPopup_Render_Empty(t *testing.T) {
	p := NewFileSearchPopup()
	lines := p.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty popup should render 0 lines, got %d", len(lines))
	}
}

func TestFileSearchPopup_Render_WithResults(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []ScoredResult{
		{Path: "src/main.go", Score: 0},
		{Path: "src/util.go", Score: 20},
		{Path: "test/main_test.go", Score: 50},
	}
	p.visible = true
	lines := p.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "src/main.go") {
		t.Errorf("first result missing: %q", joined)
	}
}

func TestFileSearchPopup_SelectionWrap(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []ScoredResult{
		{Path: "a.go", Score: 0},
		{Path: "b.go", Score: 20},
	}
	p.visible = true
	p.MoveDown()
	if p.selected != 1 {
		t.Errorf("after MoveDown: selected=%d, want 1", p.selected)
	}
	p.MoveDown()
	if p.selected != 0 {
		t.Errorf("after second MoveDown (wrap): selected=%d, want 0", p.selected)
	}
}

func TestFileSearchPopup_SelectedPath(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []ScoredResult{
		{Path: "a.go", Score: 0},
		{Path: "b.go", Score: 20},
	}
	p.visible = true
	p.selected = 1
	got := p.SelectedPath()
	if got != "b.go" {
		t.Errorf("SelectedPath: got %q, want %q", got, "b.go")
	}
}

func TestReplaceAtQuery_RetainsAtPrefix(t *testing.T) {
	m := New(nil, "")
	m.editor.SetValue("please read @mod")
	m.replaceAtQuery("src/model.go")
	got := m.editor.Value()
	want := "@src/model.go "
	if !strings.HasSuffix(got, want) {
		t.Errorf("replaceAtQuery: got %q, want suffix %q", got, want)
	}
	wantFull := "please read @src/model.go "
	if got != wantFull {
		t.Errorf("replaceAtQuery: got %q, want %q", got, wantFull)
	}
}

func TestReplaceAtQuery_TrailingSpacePreventsRetrigger(t *testing.T) {
	m := New(nil, "")
	m.editor.SetValue("@mod")
	m.replaceAtQuery("model.go")
	// After replacement, extractAtQuery should return false because of trailing space.
	_, ok := m.extractAtQuery()
	if ok {
		t.Error("extractAtQuery should return false after replaceAtQuery (trailing space)")
	}
}

func TestConfirmFileSearch_Tab(t *testing.T) {
	m := New(nil, "")
	m.editor.SetValue("@main")
	m.fileSearching = true
	m.fileSearch.query = "main"
	m.fileSearch.results = []ScoredResult{
		{Path: "cmd/main.go", Score: 0},
		{Path: "main_test.go", Score: 20},
	}
	m.fileSearch.visible = true
	m.fileSearch.selected = 0

	ok := m.confirmFileSearch()
	if !ok {
		t.Fatal("confirmFileSearch should return true")
	}
	got := m.editor.Value()
	want := "@cmd/main.go "
	if got != want {
		t.Errorf("after confirmFileSearch: got %q, want %q", got, want)
	}
	if m.fileSearching {
		t.Error("fileSearching should be false after confirm")
	}
	if m.fileSearch.visible {
		t.Error("popup should be hidden after confirm")
	}
}

func manyResults(n int) []ScoredResult {
	var rs []ScoredResult
	for i := 0; i < n; i++ {
		rs = append(rs, ScoredResult{Path: fmt.Sprintf("file%02d.go", i), Score: i})
	}
	return rs
}

func TestFileSearchPopup_SelectedRowStaysVisible(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = manyResults(15)
	p.visible = true
	for i := 0; i < 12; i++ {
		p.MoveDown()
	}
	joined := StripAnsi(strings.Join(p.Render(80), "\n"))
	if !strings.Contains(joined, "> file12.go") {
		t.Fatalf("selected row not visible:\n%s", joined)
	}
	if strings.Contains(joined, "file00.go") {
		t.Errorf("window should have scrolled past file00.go:\n%s", joined)
	}
	if !strings.Contains(joined, "... 5 more") {
		t.Errorf("missing more line:\n%s", joined)
	}

	// Wrapping up from the top goes to the last row, which must be visible.
	p.selected = 0
	p.offset = 0
	p.MoveUp()
	joined = StripAnsi(strings.Join(p.Render(80), "\n"))
	if !strings.Contains(joined, "> file14.go") {
		t.Fatalf("last row not visible after wrap:\n%s", joined)
	}

	// Wrapping down from the bottom returns to the top.
	p.MoveDown()
	joined = StripAnsi(strings.Join(p.Render(80), "\n"))
	if !strings.Contains(joined, "> file00.go") {
		t.Fatalf("first row not visible after wrap:\n%s", joined)
	}
}

func TestFileSearchPopup_SetResultsResetsOffset(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = manyResults(15)
	p.visible = true
	p.selected = 14
	p.offset = 5
	p.SetResults("", manyResults(15))
	if p.offset != 0 || p.selected != 0 {
		t.Errorf("offset=%d selected=%d, want 0 0", p.offset, p.selected)
	}
}

func TestRankFileResults_ScoresBeforeCap(t *testing.T) {
	var lines []string
	for i := 0; i < maxFileResults+20; i++ {
		lines = append(lines, fmt.Sprintf("deep/dir/x%02d/notes_target.txt", i))
	}
	// The exact match comes last in the raw output; it must still win.
	lines = append(lines, "target")
	got := rankFileResults("target", lines)
	if len(got) != maxFileResults {
		t.Fatalf("len=%d, want %d", len(got), maxFileResults)
	}
	if got[0].Path != "target" {
		t.Errorf("best match = %q, want %q", got[0].Path, "target")
	}
}

func TestFileSearchArgs(t *testing.T) {
	name, args := fileSearchArgs("-x", true)
	if name != "fd" {
		t.Fatalf("name = %q", name)
	}
	if len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != "-x" {
		t.Errorf("fd args must end with -- query: %v", args)
	}

	name, args = fileSearchArgs("foo", false)
	if name != "find" {
		t.Fatalf("name = %q", name)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-not -path */.git/*") {
		t.Errorf("find args must exclude .git: %v", args)
	}
}

// A file name can hold an escape sequence (for example in a cloned repo).
// The list shows it without the sequence; the selected value stays the real
// path.
func TestFileSearchPopup_Render_SanitizesPath(t *testing.T) {
	path := "src/evil\x1b]52;c;aGk=\x07\x1b[2J.go"
	p := NewFileSearchPopup()
	p.results = []ScoredResult{{Path: "a.go"}, {Path: path}}
	p.visible = true
	joined := strings.Join(p.Render(80), "\n")
	if strings.Contains(joined, "]52") || strings.Contains(joined, "[2J") || strings.ContainsRune(joined, '\x07') {
		t.Errorf("render has a raw escape sequence: %q", joined)
	}
	if !strings.Contains(joined, "src/evil.go") {
		t.Errorf("render missing cleaned path: %q", joined)
	}
	p.selected = 1
	if got := p.SelectedPath(); got != path {
		t.Errorf("SelectedPath = %q, want %q", got, path)
	}
}
