# TUI P0 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement all five P0 gaps -- component interface refactor, ANSI utilities, markdown rendering, multi-line editor with history, animated spinner, and slash commands -- as incremental commits on the `rewrite-to-tui` branch.

**Architecture:** Thin component layer on Bubbletea. Decompose the monolithic `Model` in `tui.go` into discrete components (`ChatComponent`, `EditorComponent`, `SpinnerComponent`, etc.) each implementing a `Component` or `Interactive` interface. Bubbletea remains the event loop and terminal driver.

**Tech Stack:** Go 1.24, Bubbletea (event loop), Lipgloss (styling), Goldmark (markdown parsing), Chroma v2 (syntax highlighting), rivo/uniseg (already indirect dep -- promote to direct for grapheme/width).

---

## Task 1: Define Component Interface and Split Files

Extract the component interfaces into their own file, and rename `tui.go` to `model.go`. This is a pure reorganization -- no behavior changes, all existing tests must still pass.

**Files:**
- Create: `gohome/internal/tui/component.go`
- Rename: `gohome/internal/tui/tui.go` -> `gohome/internal/tui/model.go`

**Step 1: Create component.go with interface definitions**

```go
// gohome/internal/tui/component.go
package tui

import tea "github.com/charmbracelet/bubbletea"

// Component is the rendering contract for all TUI elements.
// Render returns terminal lines for the given available width.
// A component that has nothing to show returns an empty slice (zero height).
type Component interface {
	Render(width int) []string
}

// Interactive is a Component that can also receive keyboard input.
type Interactive interface {
	Component
	HandleInput(msg tea.KeyMsg) tea.Cmd
}
```

**Step 2: Rename tui.go to model.go**

```bash
cd /Users/macminijh/projects/GoHome && git mv gohome/internal/tui/tui.go gohome/internal/tui/model.go
```

**Step 3: Run tests to verify nothing broke**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All tests PASS (the package name and imports are unchanged).

**Step 4: Commit**

```bash
git add gohome/internal/tui/component.go gohome/internal/tui/model.go
git commit -m "refactor(tui): define Component/Interactive interfaces, rename tui.go to model.go"
```

---

## Task 2: ANSI Text Utilities

Pure functions for ANSI-aware text manipulation. These are independent of any component and fully testable in isolation.

**Files:**
- Create: `gohome/internal/tui/ansi.go`
- Create: `gohome/internal/tui/ansi_test.go`

**Step 1: Write failing tests for VisualWidth**

```go
// gohome/internal/tui/ansi_test.go
package tui

import "testing"

func TestVisualWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"", 0},
		{"\x1b[31mred\x1b[0m", 3},                   // ANSI codes are zero-width
		{"\x1b[1;32mbold green\x1b[0m", 10},          // nested SGR
		{"日本語", 6},                                   // East Asian wide chars = 2 each
		{"\x1b[34m日本\x1b[0m", 4},                    // ANSI + wide
		{"a\x1b[38;2;255;0;0mb\x1b[0mc", 3},          // truecolor SGR
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := VisualWidth(tt.input)
			if got != tt.want {
				t.Errorf("VisualWidth(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestVisualWidth -v`
Expected: FAIL -- `VisualWidth` undefined.

**Step 3: Implement VisualWidth**

```go
// gohome/internal/tui/ansi.go
package tui

import (
	"regexp"

	"github.com/rivo/uniseg"
)

// ansiEscape matches all ANSI escape sequences (CSI, OSC, etc.).
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\|\x1b\[[0-9;]*m|\x1b\[38;2;[0-9;]*m|\x1b\[48;2;[0-9;]*m`)

// StripAnsi removes all ANSI escape sequences from s.
func StripAnsi(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// VisualWidth returns the display width of s, ignoring ANSI escape sequences.
// East Asian wide characters count as 2.
func VisualWidth(s string) int {
	clean := StripAnsi(s)
	return uniseg.StringWidth(clean)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestVisualWidth -v`
Expected: PASS

**Step 5: Write failing tests for TruncateText**

```go
// Append to ansi_test.go
func TestTruncateText(t *testing.T) {
	tests := []struct {
		input string
		width int
		want  string
	}{
		{"hello world", 5, "hello"},
		{"hello", 10, "hello"},
		{"\x1b[31mhello world\x1b[0m", 5, "\x1b[31mhello\x1b[0m"},
		{"日本語テスト", 6, "日本語"},
		{"", 5, ""},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := TruncateText(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("TruncateText(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
			}
		})
	}
}
```

**Step 6: Implement TruncateText**

```go
// Append to ansi.go

// TruncateText truncates s to fit within width display columns.
// ANSI escape sequences are preserved (they don't consume width).
// If the string contains active SGR at the truncation point, a reset is appended.
func TruncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisualWidth(s) <= width {
		return s
	}

	var result []byte
	used := 0
	i := 0
	inEscape := false
	hasOpenSGR := false

	for i < len(s) {
		// Check for ANSI escape start
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			start := i
			i += 2
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			if i < len(s) {
				i++ // consume final letter
			}
			seq := s[start:i]
			result = append(result, seq...)
			if seq == "\x1b[0m" {
				hasOpenSGR = false
			} else {
				hasOpenSGR = true
			}
			continue
		}
		inEscape = false

		// Measure grapheme width
		cluster, _, clusterWidth, _ := uniseg.FirstGraphemeCluster([]byte(s[i:]), -1)
		if used+clusterWidth > width {
			break
		}
		result = append(result, cluster...)
		used += clusterWidth
		i += len(cluster)
	}

	if hasOpenSGR {
		result = append(result, "\x1b[0m"...)
	}
	_ = inEscape
	return string(result)
}
```

**Step 7: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestTruncateText -v`
Expected: PASS

**Step 8: Write failing tests for WrapText**

```go
// Append to ansi_test.go
func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  []string
	}{
		{"short", "hello", 80, []string{"hello"}},
		{"exact", "hello", 5, []string{"hello"}},
		{"wrap at word", "hello world foo", 11, []string{"hello world", "foo"}},
		{"force break", "abcdefghij", 5, []string{"abcde", "fghij"}},
		{"empty", "", 80, []string{""}},
		{"preserves ansi", "\x1b[31mhello world\x1b[0m", 5, []string{"\x1b[31mhello\x1b[0m", "\x1b[31mworld\x1b[0m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapText(tt.input, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("WrapText(%q, %d) returned %d lines, want %d:\n  got:  %q\n  want: %q",
					tt.input, tt.width, len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
```

**Step 9: Implement WrapText**

```go
// Append to ansi.go

// WrapText wraps text at word boundaries to fit within width columns.
// ANSI escape sequences are preserved across line breaks -- if a style is active
// at a break point, the new line starts with that style re-emitted.
func WrapText(text string, width int) []string {
	if width <= 0 {
		width = 1
	}
	if text == "" {
		return []string{""}
	}

	var lines []string
	var currentLine []byte
	var currentWidth int
	var lastSpaceIdx int  // byte index of last space in currentLine
	var lastSpaceWidth int // width at last space
	var activeSGR string  // most recent non-reset SGR sequence

	i := 0
	for i < len(text) {
		// ANSI escape -- copy through without consuming width
		if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '[' {
			start := i
			i += 2
			for i < len(text) && !((text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= 'a' && text[i] <= 'z')) {
				i++
			}
			if i < len(text) {
				i++
			}
			seq := text[start:i]
			currentLine = append(currentLine, seq...)
			if seq == "\x1b[0m" {
				activeSGR = ""
			} else if len(seq) > 2 && seq[len(seq)-1] == 'm' {
				activeSGR = seq
			}
			continue
		}

		// Regular grapheme cluster
		cluster, _, clusterWidth, _ := uniseg.FirstGraphemeCluster([]byte(text[i:]), -1)
		isSpace := len(cluster) == 1 && cluster[0] == ' '

		// Would this grapheme overflow?
		if currentWidth+clusterWidth > width {
			// Try to break at last space
			if lastSpaceIdx > 0 {
				line := string(currentLine[:lastSpaceIdx])
				if activeSGR != "" {
					line += "\x1b[0m"
				}
				lines = append(lines, line)
				// Remainder after space becomes new line
				remainder := currentLine[lastSpaceIdx+1:] // skip the space
				currentLine = nil
				if activeSGR != "" {
					currentLine = append(currentLine, activeSGR...)
				}
				currentLine = append(currentLine, remainder...)
				currentWidth = currentWidth - lastSpaceWidth - 1 // subtract space + everything before
				// Recalculate currentWidth from remainder
				currentWidth = VisualWidth(StripAnsi(string(currentLine)))
				lastSpaceIdx = 0
				lastSpaceWidth = 0
			} else {
				// Force break
				line := string(currentLine)
				if activeSGR != "" {
					line += "\x1b[0m"
				}
				lines = append(lines, line)
				currentLine = nil
				if activeSGR != "" {
					currentLine = append(currentLine, activeSGR...)
				}
				currentWidth = 0
				lastSpaceIdx = 0
				lastSpaceWidth = 0
			}
		}

		if isSpace {
			lastSpaceIdx = len(currentLine)
			lastSpaceWidth = currentWidth
		}
		currentLine = append(currentLine, cluster...)
		currentWidth += clusterWidth
		i += len(cluster)
	}

	// Flush remaining
	line := string(currentLine)
	if activeSGR != "" {
		line += "\x1b[0m"
	}
	lines = append(lines, line)

	return lines
}
```

**Step 10: Run all ansi tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestVisualWidth|TestTruncateText|TestWrapText" -v`
Expected: All PASS

**Step 11: Update shortSummary to use TruncateText**

In `model.go`, replace the naive byte-slicing in `shortSummary`:

Change:
```go
if len(s) > 60 {
    return s[:57] + "..."
}
```
To:
```go
if VisualWidth(s) > 60 {
    return TruncateText(s, 57) + "..."
}
```

**Step 12: Run all tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All PASS

**Step 13: Promote uniseg to direct dependency**

Run: `cd /Users/macminijh/projects/GoHome && go mod tidy`

**Step 14: Commit**

```bash
git add gohome/internal/tui/ansi.go gohome/internal/tui/ansi_test.go gohome/internal/tui/model.go go.mod go.sum
git commit -m "feat(tui): ANSI-aware text utilities -- VisualWidth, TruncateText, WrapText"
```

---

## Task 3: Markdown Renderer

A goldmark-based ANSI renderer that takes markdown source and returns terminal-ready lines.

**Files:**
- Create: `gohome/internal/tui/markdown.go`
- Create: `gohome/internal/tui/markdown_test.go`

**Step 1: Add goldmark and chroma dependencies**

```bash
cd /Users/macminijh/projects/GoHome && go get github.com/yuin/goldmark && go get github.com/alecthomas/chroma/v2
```

**Step 2: Write failing tests for basic markdown rendering**

```go
// gohome/internal/tui/markdown_test.go
package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdownHeadings(t *testing.T) {
	md := "# Heading 1\n\n## Heading 2\n\n### Heading 3"
	lines := RenderMarkdown(md, 80)
	if len(lines) == 0 {
		t.Fatal("expected output lines")
	}
	// h1 should contain bold+underline escape and the text
	h1 := StripAnsi(lines[0])
	if !strings.Contains(h1, "Heading 1") {
		t.Errorf("h1 line = %q, want 'Heading 1'", h1)
	}
}

func TestRenderMarkdownParagraphWrap(t *testing.T) {
	long := "This is a long paragraph that should be word-wrapped when rendered at a narrow width to verify wrapping works."
	lines := RenderMarkdown(long, 30)
	if len(lines) < 2 {
		t.Errorf("expected multiple lines for narrow width, got %d", len(lines))
	}
	for _, line := range lines {
		if VisualWidth(StripAnsi(line)) > 30 {
			t.Errorf("line exceeds width 30: %q (width %d)", line, VisualWidth(StripAnsi(line)))
		}
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	lines := RenderMarkdown(md, 80)
	joined := strings.Join(lines, "\n")
	plain := StripAnsi(joined)
	if !strings.Contains(plain, "fmt.Println") {
		t.Errorf("code block content missing: %q", plain)
	}
}

func TestRenderMarkdownList(t *testing.T) {
	md := "- item one\n- item two\n- item three"
	lines := RenderMarkdown(md, 80)
	plain := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "item one") {
		t.Errorf("list items missing: %q", plain)
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	lines := RenderMarkdown("", 80)
	if len(lines) != 0 {
		t.Errorf("empty input should return empty slice, got %d lines", len(lines))
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestRenderMarkdown -v`
Expected: FAIL -- `RenderMarkdown` undefined.

**Step 4: Implement RenderMarkdown**

```go
// gohome/internal/tui/markdown.go
package tui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	ansiBold      = "\x1b[1m"
	ansiItalic    = "\x1b[3m"
	ansiUnderline = "\x1b[4m"
	ansiDim       = "\x1b[2m"
	ansiReverse   = "\x1b[7m"
	ansiReset     = "\x1b[0m"
)

// RenderMarkdown parses markdown source and returns ANSI-styled terminal lines
// wrapped to fit within width columns.
func RenderMarkdown(source string, width int) []string {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if width <= 0 {
		width = 80
	}

	src := []byte(source)
	reader := text.NewReader(src)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)

	var lines []string
	renderNode(&lines, doc, src, width, 0)
	return lines
}

func renderNode(lines *[]string, node ast.Node, src []byte, width, indent int) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Heading:
			text := extractText(n, src)
			var styled string
			switch n.Level {
			case 1:
				styled = ansiBold + ansiUnderline + text + ansiReset
			case 2:
				styled = ansiBold + text + ansiReset
			default:
				prefix := strings.Repeat("#", n.Level) + " "
				styled = prefix + text
			}
			wrapped := WrapText(styled, width)
			*lines = append(*lines, wrapped...)
			*lines = append(*lines, "")

		case *ast.Paragraph:
			text := extractInlineText(n, src)
			indentStr := strings.Repeat(" ", indent)
			wrapped := WrapText(text, width-indent)
			for _, wl := range wrapped {
				*lines = append(*lines, indentStr+wl)
			}
			*lines = append(*lines, "")

		case *ast.FencedCodeBlock:
			lang := ""
			if n.Info != nil {
				lang = strings.TrimSpace(string(n.Info.Text(src)))
			}
			code := extractCodeBlockText(n, src)
			highlighted := highlightCode(code, lang)
			codeLines := strings.Split(strings.TrimRight(highlighted, "\n"), "\n")
			*lines = append(*lines, ansiDim+"---"+ansiReset)
			for _, cl := range codeLines {
				*lines = append(*lines, "  "+cl)
			}
			*lines = append(*lines, ansiDim+"---"+ansiReset)
			*lines = append(*lines, "")

		case *ast.CodeBlock:
			code := extractCodeBlockText(n, src)
			codeLines := strings.Split(strings.TrimRight(code, "\n"), "\n")
			*lines = append(*lines, ansiDim+"---"+ansiReset)
			for _, cl := range codeLines {
				*lines = append(*lines, "  "+cl)
			}
			*lines = append(*lines, ansiDim+"---"+ansiReset)
			*lines = append(*lines, "")

		case *ast.List:
			renderList(lines, n, src, width, indent)
			*lines = append(*lines, "")

		case *ast.Blockquote:
			var inner []string
			renderNode(&inner, n, src, width-4, 0)
			for _, il := range inner {
				*lines = append(*lines, ansiDim+"| "+ansiReset+il)
			}

		case *ast.ThematicBreak:
			rule := strings.Repeat("─", width)
			*lines = append(*lines, ansiDim+rule+ansiReset)
			*lines = append(*lines, "")

		default:
			renderNode(lines, child, src, width, indent)
		}
	}
}

func renderList(lines *[]string, list *ast.List, src []byte, width, indent int) {
	idx := 1
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}
		var prefix string
		if list.IsOrdered() {
			prefix = fmt.Sprintf("%d. ", idx)
			idx++
		} else {
			prefix = "- "
		}
		text := extractText(li, src)
		indentStr := strings.Repeat(" ", indent)
		wrapped := WrapText(text, width-indent-len(prefix))
		for i, wl := range wrapped {
			if i == 0 {
				*lines = append(*lines, indentStr+prefix+wl)
			} else {
				*lines = append(*lines, indentStr+strings.Repeat(" ", len(prefix))+wl)
			}
		}
	}
}

func extractText(node ast.Node, src []byte) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		extractInlineAppend(&sb, child, src)
	}
	return sb.String()
}

func extractInlineText(node ast.Node, src []byte) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		extractInlineAppend(&sb, child, src)
	}
	return sb.String()
}

func extractInlineAppend(sb *strings.Builder, node ast.Node, src []byte) {
	switch n := node.(type) {
	case *ast.Text:
		sb.Write(n.Text(src))
		if n.HardLineBreak() || n.SoftLineBreak() {
			sb.WriteByte(' ')
		}
	case *ast.String:
		sb.Write(n.Value)
	case *ast.CodeSpan:
		sb.WriteString(ansiReverse)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*ast.Text); ok {
				sb.Write(t.Text(src))
			}
		}
		sb.WriteString(ansiReset)
	case *ast.Emphasis:
		if n.Level == 2 {
			sb.WriteString(ansiBold)
		} else {
			sb.WriteString(ansiItalic)
		}
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			extractInlineAppend(sb, child, src)
		}
		sb.WriteString(ansiReset)
	case *ast.Link:
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			extractInlineAppend(sb, child, src)
		}
		dest := string(n.Destination)
		linkText := ""
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*ast.Text); ok {
				linkText += string(t.Text(src))
			}
		}
		if dest != linkText {
			sb.WriteString(ansiDim + " (" + dest + ")" + ansiReset)
		}
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			extractInlineAppend(sb, child, src)
		}
	}
}

func extractCodeBlockText(node ast.Node, src []byte) string {
	var sb strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(src))
	}
	return sb.String()
}

func highlightCode(code, lang string) string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := chromastyles.Get("monokai")
	if style == nil {
		style = chromastyles.Fallback
	}

	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iter)
	if err != nil {
		return code
	}
	return buf.String()
}
```

**Step 5: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestRenderMarkdown -v`
Expected: All PASS

**Step 6: Run full test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All PASS

**Step 7: Commit**

```bash
git add gohome/internal/tui/markdown.go gohome/internal/tui/markdown_test.go go.mod go.sum
git commit -m "feat(tui): goldmark-based ANSI markdown renderer with syntax highlighting"
```

---

## Task 4: Input History Ring Buffer

Standalone data structure, fully testable without any TUI dependency.

**Files:**
- Create: `gohome/internal/tui/history.go`
- Create: `gohome/internal/tui/history_test.go`

**Step 1: Write failing tests**

```go
// gohome/internal/tui/history_test.go
package tui

import "testing"

func TestHistoryAddAndBrowse(t *testing.T) {
	h := NewHistory(100)
	h.Add("first")
	h.Add("second")
	h.Add("third")

	// Start browsing from "current draft"
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
	// At beginning, Prev returns same
	got = h.Prev()
	if got != "first" {
		t.Errorf("Prev() at start = %q, want %q", got, "first")
	}
	// Go back forward
	got = h.Next()
	if got != "second" {
		t.Errorf("Next() = %q, want %q", got, "second")
	}
}

func TestHistoryNextRestoresDraft(t *testing.T) {
	h := NewHistory(100)
	h.Add("one")
	h.StartBrowsing("my draft")
	h.Prev() // "one"
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
	h.Add("d") // "a" should be evicted

	h.StartBrowsing("")
	h.Prev() // d
	h.Prev() // c
	got := h.Prev() // b
	if got != "b" {
		t.Errorf("oldest after eviction = %q, want %q", got, "b")
	}
	got = h.Prev() // still b (at start)
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
	h.Prev() // "same"
	got := h.Prev()
	if got != "same" {
		t.Errorf("second Prev() = %q, want %q", got, "same")
	}
}
```

**Step 2: Run test to verify failure**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHistory -v`
Expected: FAIL -- `NewHistory` undefined.

**Step 3: Implement History**

```go
// gohome/internal/tui/history.go
package tui

// History is a ring buffer of past user inputs with browsing support.
type History struct {
	entries []string
	maxSize int
	pos     int    // browsing position; -1 = not browsing
	draft   string // saved editor content when browsing starts
}

// NewHistory creates a History with the given maximum capacity.
func NewHistory(maxSize int) *History {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &History{
		maxSize: maxSize,
		pos:     -1,
	}
}

// Add appends text to history. Skips empty strings and consecutive duplicates.
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

// StartBrowsing saves the current editor draft and resets the browse position.
func (h *History) StartBrowsing(draft string) {
	h.draft = draft
	h.pos = len(h.entries)
}

// Browsing returns true if the user is currently navigating history.
func (h *History) Browsing() bool {
	return h.pos >= 0
}

// Prev returns the previous (older) history entry.
// If already at the oldest, returns that entry again.
// If history is empty, returns the draft.
func (h *History) Prev() string {
	if len(h.entries) == 0 {
		return h.draft
	}
	if h.pos > 0 {
		h.pos--
	}
	return h.entries[h.pos]
}

// Next returns the next (newer) history entry.
// If past the newest, restores and returns the draft.
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

// StopBrowsing resets browse state.
func (h *History) StopBrowsing() {
	h.pos = -1
}
```

**Step 4: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHistory -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/history.go gohome/internal/tui/history_test.go
git commit -m "feat(tui): input history ring buffer with browsing support"
```

---

## Task 5: EditorComponent

Replace the Bubbletea `textarea.Model` with a custom editor implementing `Interactive`.

**Files:**
- Create: `gohome/internal/tui/editor.go`
- Create: `gohome/internal/tui/editor_test.go`

**Step 1: Write failing tests for basic rendering**

```go
// gohome/internal/tui/editor_test.go
package tui

import (
	"strings"
	"testing"
)

func TestEditorRenderEmpty(t *testing.T) {
	e := NewEditor(80, 24)
	lines := e.Render(80)
	// Should have borders + at least 3 content lines (min height)
	if len(lines) < 5 { // top border + 3 lines + bottom border
		t.Errorf("expected at least 5 lines, got %d", len(lines))
	}
	// First and last should be border lines
	if !strings.Contains(lines[0], "─") {
		t.Errorf("first line should be border, got %q", lines[0])
	}
}

func TestEditorRenderWithContent(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('h')
	e.InsertRune('i')
	lines := e.Render(80)
	joined := strings.Join(lines, "\n")
	plain := StripAnsi(joined)
	if !strings.Contains(plain, "hi") {
		t.Errorf("content 'hi' not found in render: %q", plain)
	}
}

func TestEditorSubmit(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('t')
	e.InsertRune('e')
	e.InsertRune('s')
	e.InsertRune('t')
	text, ok := e.Submit()
	if !ok {
		t.Fatal("Submit() returned ok=false")
	}
	if text != "test" {
		t.Errorf("Submit() = %q, want %q", text, "test")
	}
	// After submit, editor should be empty
	if e.Value() != "" {
		t.Errorf("after submit, Value() = %q, want empty", e.Value())
	}
}

func TestEditorNewline(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('a')
	e.InsertNewline()
	e.InsertRune('b')
	val := e.Value()
	if val != "a\nb" {
		t.Errorf("Value() = %q, want %q", val, "a\nb")
	}
}

func TestEditorDynamicHeight(t *testing.T) {
	e := NewEditor(80, 30) // termHeight=30, max=floor(30*0.3)=9
	// Type 10 lines
	for i := 0; i < 10; i++ {
		if i > 0 {
			e.InsertNewline()
		}
		e.InsertRune('x')
	}
	lines := e.Render(80)
	// Should not exceed maxHeight + 2 borders
	maxH := 9
	if len(lines) > maxH+2 {
		t.Errorf("editor rendered %d lines, expected <= %d (maxH=%d + 2 borders)", len(lines), maxH+2, maxH)
	}
}

func TestEditorEmptySubmitReturnsNotOk(t *testing.T) {
	e := NewEditor(80, 24)
	_, ok := e.Submit()
	if ok {
		t.Error("Submit() on empty editor should return ok=false")
	}
}
```

**Step 2: Run tests to verify failure**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestEditor -v`
Expected: FAIL -- `NewEditor` undefined.

**Step 3: Implement EditorComponent**

```go
// gohome/internal/tui/editor.go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	editorMinHeight = 3
	editorMaxRatio  = 0.3
)

var (
	editorBorder     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	editorBashBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// EditorComponent is a multi-line text editor implementing Interactive.
type EditorComponent struct {
	lines      []string
	cursorLine int
	cursorCol  int
	scrollTop  int
	width      int
	termHeight int
	history    *History
	onSubmit   func(string)
}

// NewEditor creates an EditorComponent. termHeight is used to compute max visible height.
func NewEditor(width, termHeight int) *EditorComponent {
	return &EditorComponent{
		lines:      []string{""},
		cursorLine: 0,
		cursorCol:  0,
		width:      width,
		termHeight: termHeight,
		history:    NewHistory(100),
	}
}

// SetSubmitHandler sets the callback invoked when the user submits.
func (e *EditorComponent) SetSubmitHandler(fn func(string)) {
	e.onSubmit = fn
}

// SetTermHeight updates the terminal height (called on resize).
func (e *EditorComponent) SetTermHeight(h int) {
	e.termHeight = h
}

// maxHeight returns the maximum visible editor lines (excluding borders).
func (e *EditorComponent) maxHeight() int {
	max := int(float64(e.termHeight) * editorMaxRatio)
	if max < editorMinHeight {
		max = editorMinHeight
	}
	return max
}

// Value returns the current editor content as a single string.
func (e *EditorComponent) Value() string {
	return strings.Join(e.lines, "\n")
}

// SetValue replaces the editor content entirely.
func (e *EditorComponent) SetValue(s string) {
	if s == "" {
		e.lines = []string{""}
	} else {
		e.lines = strings.Split(s, "\n")
	}
	e.cursorLine = len(e.lines) - 1
	e.cursorCol = len(e.lines[e.cursorLine])
}

// InsertRune inserts a character at the cursor position.
func (e *EditorComponent) InsertRune(r rune) {
	line := e.lines[e.cursorLine]
	e.lines[e.cursorLine] = line[:e.cursorCol] + string(r) + line[e.cursorCol:]
	e.cursorCol++
	if e.history.Browsing() {
		e.history.StopBrowsing()
	}
}

// InsertNewline splits the current line at the cursor.
func (e *EditorComponent) InsertNewline() {
	line := e.lines[e.cursorLine]
	before := line[:e.cursorCol]
	after := line[e.cursorCol:]
	e.lines[e.cursorLine] = before
	// Insert after line
	newLines := make([]string, 0, len(e.lines)+1)
	newLines = append(newLines, e.lines[:e.cursorLine+1]...)
	newLines = append(newLines, after)
	newLines = append(newLines, e.lines[e.cursorLine+1:]...)
	e.lines = newLines
	e.cursorLine++
	e.cursorCol = 0
}

// Submit returns the editor content and clears it. Returns ok=false if empty.
func (e *EditorComponent) Submit() (string, bool) {
	text := strings.TrimSpace(e.Value())
	if text == "" {
		return "", false
	}
	e.history.Add(text)
	e.lines = []string{""}
	e.cursorLine = 0
	e.cursorCol = 0
	e.scrollTop = 0
	return text, true
}

// Render implements Component.
func (e *EditorComponent) Render(width int) []string {
	e.width = width
	maxH := e.maxHeight()

	// Determine visible height
	contentH := len(e.lines)
	visibleH := contentH
	if visibleH > maxH {
		visibleH = maxH
	}
	if visibleH < editorMinHeight {
		visibleH = editorMinHeight
	}

	// Ensure cursor is visible
	if e.cursorLine < e.scrollTop {
		e.scrollTop = e.cursorLine
	}
	if e.cursorLine >= e.scrollTop+visibleH {
		e.scrollTop = e.cursorLine - visibleH + 1
	}

	// Build visible lines
	var output []string

	// Top border
	borderStyle := editorBorder
	if strings.HasPrefix(strings.TrimSpace(e.Value()), "!") {
		borderStyle = editorBashBorder
	}
	topBorder := borderStyle.Render(strings.Repeat("─", width))

	aboveCount := e.scrollTop
	if aboveCount > 0 {
		topBorder = borderStyle.Render(strings.Repeat("─", 3) + " " + strings.Repeat("─", width-4))
	}
	output = append(output, topBorder)

	// Content lines
	for i := 0; i < visibleH; i++ {
		lineIdx := e.scrollTop + i
		if lineIdx < len(e.lines) {
			line := e.lines[lineIdx]
			// Render cursor
			if lineIdx == e.cursorLine {
				line = e.renderWithCursor(line)
			}
			output = append(output, line)
		} else {
			output = append(output, "")
		}
	}

	// Bottom border
	belowCount := len(e.lines) - (e.scrollTop + visibleH)
	bottomBorder := borderStyle.Render(strings.Repeat("─", width))
	if belowCount > 0 {
		bottomBorder = borderStyle.Render(strings.Repeat("─", 3) + " " + strings.Repeat("─", width-4))
	}
	output = append(output, bottomBorder)

	return output
}

// renderWithCursor inserts a reverse-video cursor character at cursorCol.
func (e *EditorComponent) renderWithCursor(line string) string {
	col := e.cursorCol
	if col >= len(line) {
		return line + "\x1b[7m \x1b[0m"
	}
	return line[:col] + "\x1b[7m" + string(line[col]) + "\x1b[0m" + line[col+1:]
}

// HandleInput implements Interactive.
func (e *EditorComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		if msg.Alt {
			e.InsertNewline()
			return nil
		}
		text, ok := e.Submit()
		if ok && e.onSubmit != nil {
			e.onSubmit(text)
		}
		return nil

	case tea.KeyUp:
		if e.cursorLine == 0 {
			// History navigation
			if !e.history.Browsing() {
				e.history.StartBrowsing(e.Value())
			}
			entry := e.history.Prev()
			e.SetValue(entry)
		} else {
			e.cursorLine--
			if e.cursorCol > len(e.lines[e.cursorLine]) {
				e.cursorCol = len(e.lines[e.cursorLine])
			}
		}

	case tea.KeyDown:
		if e.cursorLine >= len(e.lines)-1 {
			// History forward
			if e.history.Browsing() {
				entry := e.history.Next()
				e.SetValue(entry)
			}
		} else {
			e.cursorLine++
			if e.cursorCol > len(e.lines[e.cursorLine]) {
				e.cursorCol = len(e.lines[e.cursorLine])
			}
		}

	case tea.KeyLeft:
		if e.cursorCol > 0 {
			e.cursorCol--
		} else if e.cursorLine > 0 {
			e.cursorLine--
			e.cursorCol = len(e.lines[e.cursorLine])
		}

	case tea.KeyRight:
		if e.cursorCol < len(e.lines[e.cursorLine]) {
			e.cursorCol++
		} else if e.cursorLine < len(e.lines)-1 {
			e.cursorLine++
			e.cursorCol = 0
		}

	case tea.KeyHome, tea.KeyCtrlA:
		e.cursorCol = 0

	case tea.KeyEnd, tea.KeyCtrlE:
		e.cursorCol = len(e.lines[e.cursorLine])

	case tea.KeyCtrlK:
		// Kill to end of line
		e.lines[e.cursorLine] = e.lines[e.cursorLine][:e.cursorCol]

	case tea.KeyCtrlU:
		// Kill entire line
		e.lines[e.cursorLine] = ""
		e.cursorCol = 0

	case tea.KeyBackspace:
		if e.cursorCol > 0 {
			line := e.lines[e.cursorLine]
			e.lines[e.cursorLine] = line[:e.cursorCol-1] + line[e.cursorCol:]
			e.cursorCol--
		} else if e.cursorLine > 0 {
			// Join with previous line
			prev := e.lines[e.cursorLine-1]
			e.cursorCol = len(prev)
			e.lines[e.cursorLine-1] = prev + e.lines[e.cursorLine]
			e.lines = append(e.lines[:e.cursorLine], e.lines[e.cursorLine+1:]...)
			e.cursorLine--
		}

	case tea.KeyDelete:
		line := e.lines[e.cursorLine]
		if e.cursorCol < len(line) {
			e.lines[e.cursorLine] = line[:e.cursorCol] + line[e.cursorCol+1:]
		} else if e.cursorLine < len(e.lines)-1 {
			// Join with next line
			e.lines[e.cursorLine] = line + e.lines[e.cursorLine+1]
			e.lines = append(e.lines[:e.cursorLine+1], e.lines[e.cursorLine+2:]...)
		}

	case tea.KeyRunes:
		for _, r := range msg.Runes {
			e.InsertRune(r)
		}

	case tea.KeyShiftDown:
		// Shift+Enter -> newline (some terminals send this)
		e.InsertNewline()
	}

	return nil
}
```

**Step 4: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestEditor -v`
Expected: All PASS

**Step 5: Run full test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All PASS (editor is not yet wired into Model)

**Step 6: Commit**

```bash
git add gohome/internal/tui/editor.go gohome/internal/tui/editor_test.go
git commit -m "feat(tui): EditorComponent with dynamic height, word wrap, cursor, history"
```

---

## Task 6: SpinnerComponent

Animated braille spinner that shows when the agent is processing.

**Files:**
- Create: `gohome/internal/tui/spinner.go`
- Create: `gohome/internal/tui/spinner_test.go`

**Step 1: Write failing tests**

```go
// gohome/internal/tui/spinner_test.go
package tui

import "testing"

func TestSpinnerRenderInactive(t *testing.T) {
	s := NewSpinner()
	lines := s.Render(80)
	if len(lines) != 0 {
		t.Errorf("inactive spinner should render 0 lines, got %d", len(lines))
	}
}

func TestSpinnerRenderActive(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	lines := s.Render(80)
	if len(lines) != 1 {
		t.Fatalf("active spinner should render 1 line, got %d", len(lines))
	}
	plain := StripAnsi(lines[0])
	if plain == "" {
		t.Error("spinner line should not be empty")
	}
}

func TestSpinnerTick(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	first := s.Render(80)[0]
	s.Tick()
	second := s.Render(80)[0]
	if first == second {
		t.Error("spinner should change after tick")
	}
}

func TestSpinnerStop(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	s.Stop()
	lines := s.Render(80)
	if len(lines) != 0 {
		t.Errorf("stopped spinner should render 0 lines, got %d", len(lines))
	}
}

func TestSpinnerMessageChange(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	s.SetMessage("Running bash...")
	lines := s.Render(80)
	plain := StripAnsi(lines[0])
	if plain == "" {
		t.Error("expected non-empty")
	}
	// Should contain new message
	if !containsStr(plain, "Running bash...") {
		t.Errorf("expected message 'Running bash...' in %q", plain)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > len(sub) && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run tests to verify failure**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestSpinner -v`
Expected: FAIL -- `NewSpinner` undefined.

**Step 3: Implement SpinnerComponent**

```go
// gohome/internal/tui/spinner.go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 80 * time.Millisecond

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

// spinnerTickMsg is sent by the spinner's ticker to advance the frame.
type spinnerTickMsg struct{}

// SpinnerComponent displays an animated braille spinner when active.
type SpinnerComponent struct {
	frame   int
	active  bool
	message string
}

// NewSpinner creates an inactive SpinnerComponent.
func NewSpinner() *SpinnerComponent {
	return &SpinnerComponent{}
}

// Start activates the spinner with the given message.
func (s *SpinnerComponent) Start(message string) {
	s.active = true
	s.message = message
	s.frame = 0
}

// Stop deactivates the spinner.
func (s *SpinnerComponent) Stop() {
	s.active = false
}

// SetMessage updates the spinner text without restarting.
func (s *SpinnerComponent) SetMessage(msg string) {
	s.message = msg
}

// Tick advances the spinner frame. Called by the root model on spinnerTickMsg.
func (s *SpinnerComponent) Tick() {
	s.frame = (s.frame + 1) % len(spinnerFrames)
}

// Active returns whether the spinner is currently running.
func (s *SpinnerComponent) Active() bool {
	return s.active
}

// Render implements Component. Returns empty slice when inactive.
func (s *SpinnerComponent) Render(width int) []string {
	if !s.active {
		return nil
	}
	frame := spinnerFrames[s.frame%len(spinnerFrames)]
	line := spinnerStyle.Render(frame) + " " + s.message
	return []string{line}
}

// TickCmd returns a tea.Cmd that sends a spinnerTickMsg after the interval.
func SpinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}
```

**Step 4: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestSpinner -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/spinner.go gohome/internal/tui/spinner_test.go
git commit -m "feat(tui): SpinnerComponent with braille animation"
```

---

## Task 7: ChatComponent

Replace `renderTimeline()` with a proper component that uses markdown rendering for assistant messages and manages its own scrolling.

**Files:**
- Create: `gohome/internal/tui/chat.go`
- Create: `gohome/internal/tui/chat_test.go`

**Step 1: Write failing tests**

```go
// gohome/internal/tui/chat_test.go
package tui

import (
	"strings"
	"testing"
)

func TestChatRenderUserMessage(t *testing.T) {
	entries := []TimelineEntry{{Kind: "user", Text: "hello world"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "hello world") {
		t.Errorf("user message not found in render: %q", joined)
	}
}

func TestChatRenderAssistantMarkdown(t *testing.T) {
	entries := []TimelineEntry{{Kind: "assistant", Text: "# Hello\n\nThis is **bold**."}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	// Should contain ANSI bold for the heading
	if !strings.Contains(joined, ansiBold) {
		t.Error("expected bold ANSI in heading")
	}
	plain := StripAnsi(joined)
	if !strings.Contains(plain, "Hello") {
		t.Errorf("heading text missing: %q", plain)
	}
}

func TestChatRenderToolCollapsed(t *testing.T) {
	entries := []TimelineEntry{{Kind: "tool", ToolName: "bash", Text: `{"command":"ls"}`, ToolResult: "file.txt"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name missing: %q", joined)
	}
}

func TestChatRenderEmpty(t *testing.T) {
	entries := []TimelineEntry{}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty timeline should render 0 lines, got %d", len(lines))
	}
}

func TestChatScrolling(t *testing.T) {
	// Create many entries that exceed maxHeight
	var entries []TimelineEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, TimelineEntry{Kind: "user", Text: "message"})
	}
	c := NewChat(&entries, 10)
	lines := c.Render(80)
	// Should be clamped to maxHeight
	if len(lines) > 10 {
		t.Errorf("expected max 10 lines, got %d", len(lines))
	}
}
```

**Step 2: Run tests to verify failure**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestChat -v`
Expected: FAIL -- `NewChat` undefined.

**Step 3: Implement ChatComponent**

```go
// gohome/internal/tui/chat.go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	userPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	toolStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// ChatComponent renders the conversation timeline with markdown support.
type ChatComponent struct {
	timeline   *[]TimelineEntry
	scrollTop  int
	maxHeight  int
	autoScroll bool
	cursor     int // highlighted entry (-1 = none)
}

// NewChat creates a ChatComponent.
func NewChat(timeline *[]TimelineEntry, maxHeight int) *ChatComponent {
	return &ChatComponent{
		timeline:   timeline,
		maxHeight:  maxHeight,
		autoScroll: true,
		cursor:     -1,
	}
}

// SetMaxHeight updates the visible height constraint.
func (c *ChatComponent) SetMaxHeight(h int) {
	c.maxHeight = h
}

// SetCursor sets the highlighted entry index (-1 for none).
func (c *ChatComponent) SetCursor(idx int) {
	c.cursor = idx
}

// ScrollUp scrolls the chat up by n lines.
func (c *ChatComponent) ScrollUp(n int) {
	c.scrollTop -= n
	if c.scrollTop < 0 {
		c.scrollTop = 0
	}
	c.autoScroll = false
}

// ScrollDown scrolls the chat down by n lines.
func (c *ChatComponent) ScrollDown(n int) {
	c.scrollTop += n
	c.autoScroll = false
}

// ScrollToBottom resets auto-scroll.
func (c *ChatComponent) ScrollToBottom() {
	c.autoScroll = true
}

// Render implements Component.
func (c *ChatComponent) Render(width int) []string {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return nil
	}

	// Render all entries into lines
	var allLines []string
	for i, e := range *c.timeline {
		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		switch e.Kind {
		case "user":
			text := userPrefix.Render("you:") + " " + e.Text
			wrapped := WrapText(text, width-2)
			for _, wl := range wrapped {
				allLines = append(allLines, marker+wl)
			}

		case "assistant":
			mdLines := RenderMarkdown(e.Text, width-2)
			if len(mdLines) == 0 {
				mdLines = WrapText(e.Text, width-2)
			}
			for _, ml := range mdLines {
				allLines = append(allLines, marker+ml)
			}

		case "tool":
			collapsed := renderToolLine(e, width-2)
			allLines = append(allLines, marker+collapsed)
			if e.Expanded {
				if e.Text != "" {
					allLines = append(allLines, "       args: "+e.Text)
				}
				if e.ToolResult != "" {
					allLines = append(allLines, "       result:")
					for _, rl := range strings.Split(e.ToolResult, "\n") {
						allLines = append(allLines, "         "+rl)
					}
				}
			}

		case "notice":
			line := noticeStyle.Render("[notice] " + e.Text)
			allLines = append(allLines, marker+line)
		}
	}

	// Apply scroll and height constraints
	if c.maxHeight <= 0 || len(allLines) <= c.maxHeight {
		return allLines
	}

	// Auto-scroll: show the last maxHeight lines
	if c.autoScroll {
		c.scrollTop = len(allLines) - c.maxHeight
	}
	if c.scrollTop < 0 {
		c.scrollTop = 0
	}
	if c.scrollTop > len(allLines)-c.maxHeight {
		c.scrollTop = len(allLines) - c.maxHeight
	}

	return allLines[c.scrollTop : c.scrollTop+c.maxHeight]
}

func renderToolLine(e TimelineEntry, maxWidth int) string {
	arg := shortArg(e.Text)
	result := shortSummary(e.ToolResult)
	line := toolStyle.Render(fmt.Sprintf("[tool] %s", e.ToolName))
	if arg != "" {
		line += " " + arg
	}
	if result != "" {
		line += "  ->  " + result
	}
	if VisualWidth(StripAnsi(line)) > maxWidth {
		line = TruncateText(line, maxWidth)
	}
	return line
}
```

**Step 4: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestChat -v`
Expected: All PASS

**Step 5: Run full suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): ChatComponent with markdown rendering and scroll"
```

---

## Task 8: Slash Command Callbacks

Wire `/new`, `/resume`, `/model`, `/cancel` with the callback pattern.

**Files:**
- Create: `gohome/internal/tui/slash.go`
- Modify: `gohome/internal/tui/model.go` (move slash logic, add SlashCallbacks to Model)
- Modify: `gohome/internal/tui/slash_test.go` (add tests for new commands)

**Step 1: Write failing tests for new slash commands**

```go
// Append to existing slash_test.go or create new test file
// gohome/internal/tui/slash_new_test.go
package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestSlashCancel(t *testing.T) {
	cancelled := false
	callbacks := tui.SlashCallbacks{
		CancelSession: func(id string) { cancelled = true },
	}
	m := tui.New(nil, "main")
	m.SetSlashCallbacks(callbacks)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Type("/cancel")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Cancelled"))
	}, teatest.WithDuration(2*time.Second))

	if !cancelled {
		t.Error("CancelSession callback was not invoked")
	}
}

func TestSlashNew(t *testing.T) {
	called := false
	callbacks := tui.SlashCallbacks{
		NewSession: func() (string, error) {
			called = true
			return "new-sess", nil
		},
	}
	m := tui.New(nil, "main")
	m.SetSlashCallbacks(callbacks)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Type("/new")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("new-sess"))
	}, teatest.WithDuration(2*time.Second))

	if !called {
		t.Error("NewSession callback was not invoked")
	}
}

func TestSlashModel(t *testing.T) {
	m := tui.New(nil, "main")
	m.SetModelName("gpt-4")
	callbacks := tui.SlashCallbacks{
		SetModel: func(name string) error { return nil },
	}
	m.SetSlashCallbacks(callbacks)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// /model without arg shows current model
	tm.Type("/model")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("gpt-4"))
	}, teatest.WithDuration(2*time.Second))
}
```

**Step 2: Run tests to verify failure**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestSlashCancel|TestSlashNew|TestSlashModel" -v`
Expected: FAIL -- `SlashCallbacks` and `SetSlashCallbacks` undefined.

**Step 3: Create slash.go with SlashCallbacks struct and move handleSlashCommand**

```go
// gohome/internal/tui/slash.go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SlashCallbacks are functions called by slash commands to interact with the
// agent/session layer. The TUI remains decoupled from those packages.
type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) error
	CancelSession func(id string)
	SetModel      func(name string) error
}
```

**Step 4: Add SetSlashCallbacks to Model and update handleSlashCommand in model.go**

Add to Model struct: `slashCB SlashCallbacks`

Add method:
```go
func (m *Model) SetSlashCallbacks(cb SlashCallbacks) {
    m.slashCB = cb
}
```

Update `handleSlashCommand`:
```go
func (m *Model) handleSlashCommand(raw string) tea.Cmd {
    fields := strings.Fields(raw)
    if len(fields) == 0 {
        return nil
    }
    cmd := fields[0]
    switch cmd {
    case "/quit":
        return tea.Quit
    case "/yolo":
        m.yolo = !m.yolo
        if m.yolo {
            m.statusMsg = "YOLO mode ON"
        } else {
            m.statusMsg = "YOLO mode OFF"
        }
        if m.onYoloChange != nil {
            m.onYoloChange(m.yolo)
        }
    case "/tokens":
        m.showTokens = true
        m.statusMsg = ""
    case "/cancel":
        if m.slashCB.CancelSession != nil {
            m.slashCB.CancelSession(m.focused)
        }
        sv := m.getOrCreateSession(m.focused, 0)
        sv.InFlight = false
        sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
        m.statusMsg = "Cancelled"
    case "/new":
        if m.slashCB.NewSession != nil {
            id, err := m.slashCB.NewSession()
            if err != nil {
                m.statusMsg = fmt.Sprintf("/new: %v", err)
            } else {
                m.getOrCreateSession(id, 0)
                m.focused = id
                m.cursor = 0
                m.statusMsg = "New session: " + id
            }
        } else {
            m.statusMsg = "/new: not configured"
        }
    case "/resume":
        if len(fields) < 2 {
            m.statusMsg = "/resume: provide a session ID"
            break
        }
        sid := fields[1]
        if m.slashCB.ResumeSession != nil {
            err := m.slashCB.ResumeSession(sid)
            if err != nil {
                m.statusMsg = fmt.Sprintf("/resume: %v", err)
            } else {
                m.getOrCreateSession(sid, 0)
                m.focused = sid
                m.cursor = 0
                m.statusMsg = "Resumed: " + sid
            }
        } else {
            m.statusMsg = "/resume: not configured"
        }
    case "/model":
        if len(fields) < 2 {
            m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
            break
        }
        name := fields[1]
        if m.slashCB.SetModel != nil {
            err := m.slashCB.SetModel(name)
            if err != nil {
                m.statusMsg = fmt.Sprintf("/model: %v", err)
            } else {
                m.modelName = name
                m.statusMsg = "Model set to " + name
            }
        } else {
            m.statusMsg = "/model: not configured"
        }
    default:
        m.statusMsg = cmd + ": unknown command"
    }
    return nil
}
```

**Step 5: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -v`
Expected: All PASS (existing + new slash tests)

**Step 6: Commit**

```bash
git add gohome/internal/tui/slash.go gohome/internal/tui/model.go gohome/internal/tui/slash_new_test.go
git commit -m "feat(tui): implement /cancel, /new, /resume, /model with SlashCallbacks"
```

---

## Task 9: Wire Components into Root Model

Replace the monolithic `View()` with component-based rendering. Wire the spinner tick, replace the viewport with ChatComponent scroll, replace the textarea with EditorComponent.

**Files:**
- Modify: `gohome/internal/tui/model.go`
- Modify: existing tests as needed for new rendering

**Step 1: Add component fields to Model struct**

Replace:
```go
input   textarea.Model
vp      viewport.Model
vpReady bool
```

With:
```go
editor  *EditorComponent
chat    *ChatComponent
spinner *SpinnerComponent
```

Update `New()`:
```go
func New(fe *Frontend, sessionID string) *Model {
    // ... existing session setup ...
    
    var inputCh chan string
    if fe != nil {
        inputCh = fe.input
    } else {
        inputCh = make(chan string, 1)
    }

    m := &Model{
        theme:            style.Default(),
        sessions:         map[string]*SessionView{sessionID: main},
        order:            []string{sessionID},
        focused:          sessionID,
        inputCh:          inputCh,
        contextWindow:    128000,
        pendingApprovals: make(map[string]*approvalPrompt),
        editor:           NewEditor(80, 24),
        spinner:          NewSpinner(),
    }
    m.chat = NewChat(&main.Timeline, 20)
    m.editor.SetSubmitHandler(func(text string) {
        // Handle submit (moved from the Enter key handler)
    })
    return m
}
```

**Step 2: Rewrite View() to use components**

```go
func (m *Model) View() string {
    var lines []string
    lines = append(lines, m.sessionStrip())
    
    // Notification
    if nl := m.notificationLine(); nl != "" {
        lines = append(lines, m.theme.Notification.Render(nl))
    }
    
    // Chat area
    chatH := m.winH - editorMinHeight - 2 - stripHeight - statusHeight - 2
    if chatH < 1 {
        chatH = 1
    }
    m.chat.SetMaxHeight(chatH)
    chatLines := m.chat.Render(m.winW)
    lines = append(lines, chatLines...)
    
    // Spinner
    spinnerLines := m.spinner.Render(m.winW)
    lines = append(lines, spinnerLines...)
    
    // Status message
    if m.statusMsg != "" {
        lines = append(lines, m.statusMsg)
    }
    
    // Tokens overlay OR editor
    if m.showTokens {
        lines = append(lines, m.renderTokensOverlay())
    } else if m.activeApproval != nil {
        lines = append(lines, renderApprovalOverlay(m.activeApproval, m.winW))
    } else {
        m.editor.SetTermHeight(m.winH)
        editorLines := m.editor.Render(m.winW)
        lines = append(lines, editorLines...)
    }
    
    // Status bar
    lines = append(lines, m.statusBar())
    
    return strings.Join(lines, "\n")
}
```

**Step 3: Update Update() to route to components**

Wire `spinnerTickMsg` to spinner, route `tea.KeyMsg` to editor when focused, start spinner on `EventTokenDelta`, stop on `EventTurnDone`/`EventError`.

**Step 4: Remove textarea and viewport imports**

Remove `"github.com/charmbracelet/bubbles/textarea"` and `"github.com/charmbracelet/bubbles/viewport"` imports from model.go.

**Step 5: Update existing tests**

Some tests use `tm.Type(...)` which sends key messages through Bubbletea. These should still work since the EditorComponent handles `tea.KeyRunes`. Update any tests that check for textarea-specific output (like placeholder text) -- the editor doesn't show "Type a message..." but instead shows an empty line with a cursor.

**Step 6: Run full test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/...`
Expected: All PASS (some golden file snapshots may need updating)

**Step 7: Update golden files if needed**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -update`
(Only if snapshot tests exist and need re-baselining after the render change.)

**Step 8: Commit**

```bash
git add gohome/internal/tui/
git commit -m "refactor(tui): wire ChatComponent, EditorComponent, SpinnerComponent into root Model"
```

---

## Task 10: Remove Dead Code and Clean Up

Remove the old `renderTimeline` function (replaced by ChatComponent), unused viewport/textarea fields, and run `go mod tidy` to clean dependencies.

**Files:**
- Modify: `gohome/internal/tui/model.go`
- Modify: `go.mod`, `go.sum`

**Step 1: Remove renderTimeline and rebuildViewport from model.go**

These are fully replaced by `ChatComponent.Render()`.

**Step 2: Remove unused imports**

Remove `"github.com/charmbracelet/bubbles/textarea"` and `"github.com/charmbracelet/bubbles/viewport"` if not already done.

**Step 3: Run go mod tidy**

```bash
cd /Users/macminijh/projects/GoHome && go mod tidy
```

**Step 4: Run full suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor(tui): remove dead renderTimeline, viewport/textarea dependencies"
```

---

## Summary

| Task | What | Key Files |
|---|---|---|
| 1 | Interface + file split | `component.go`, rename `tui.go` -> `model.go` |
| 2 | ANSI utilities | `ansi.go`, `ansi_test.go` |
| 3 | Markdown renderer | `markdown.go`, `markdown_test.go` |
| 4 | History ring buffer | `history.go`, `history_test.go` |
| 5 | EditorComponent | `editor.go`, `editor_test.go` |
| 6 | SpinnerComponent | `spinner.go`, `spinner_test.go` |
| 7 | ChatComponent | `chat.go`, `chat_test.go` |
| 8 | Slash commands | `slash.go`, `slash_new_test.go`, modify `model.go` |
| 9 | Wire into root Model | modify `model.go`, update tests |
| 10 | Clean up dead code | modify `model.go`, `go.mod` |

Tasks 1-8 are independent modules (can be committed in any order). Task 9 integrates everything. Task 10 cleans up.
