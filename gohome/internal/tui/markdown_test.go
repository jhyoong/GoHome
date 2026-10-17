package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderMarkdownHeadings(t *testing.T) {
	md := "# Heading 1\n\n## Heading 2\n\n### Heading 3"
	lines := RenderMarkdown(md, 80)
	if len(lines) == 0 {
		t.Fatal("expected output lines")
	}
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

func TestRenderMarkdownTable(t *testing.T) {
	md := "| Name | Status |\n|---|---|\n| foo | ok |\n| bar | fail |"
	lines := RenderMarkdown(md, 80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "foo") {
		t.Errorf("table cell 'foo' missing: %q", joined)
	}
	if !strings.Contains(joined, "bar") {
		t.Errorf("table cell 'bar' missing: %q", joined)
	}
	if !strings.Contains(joined, "┌") {
		t.Errorf("table top border missing: %q", joined)
	}
	if !strings.Contains(joined, "└") {
		t.Errorf("table bottom border missing: %q", joined)
	}
}

func TestRenderMarkdownTableAlignment(t *testing.T) {
	md := "| Left | Center | Right |\n|:---|:---:|---:|\n| a | b | c |"
	lines := RenderMarkdown(md, 80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Left") || !strings.Contains(joined, "Center") || !strings.Contains(joined, "Right") {
		t.Errorf("table headers missing: %q", joined)
	}
}

func TestHighlightCodeCachesResult(t *testing.T) {
	resetHighlightCache()
	code := "func main() {}\n"
	first := highlightCode(code, "go")
	if len(highlightCache) != 1 {
		t.Fatalf("cache size = %d, want 1", len(highlightCache))
	}

	// Replace the cached value: a hit must return it instead of re-highlighting.
	key := "go\x00" + code
	highlightCache[key] = "sentinel"
	if got := highlightCode(code, "go"); got != "sentinel" {
		t.Errorf("expected cached value, got %q", got)
	}

	// Different language is a different key.
	highlightCode(code, "python")
	if len(highlightCache) != 2 {
		t.Errorf("cache size = %d, want 2", len(highlightCache))
	}
	if !strings.Contains(StripAnsi(first), "func main") {
		t.Errorf("highlighted output lost code: %q", first)
	}
}

func TestHighlightCodeCacheBounded(t *testing.T) {
	resetHighlightCache()
	for i := 0; i < highlightCacheMax+10; i++ {
		highlightCode(strings.Repeat("x", i+1), "")
	}
	if len(highlightCache) != highlightCacheMax || len(highlightKeys) != highlightCacheMax {
		t.Fatalf("cache size = %d (keys %d), want %d", len(highlightCache), len(highlightKeys), highlightCacheMax)
	}
}

func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = StripAnsi(l)
	}
	return out
}

func assertMaxWidth(t *testing.T, lines []string, width int) {
	t.Helper()
	for _, l := range lines {
		if w := VisualWidth(l); w > width {
			t.Errorf("line exceeds width %d (got %d): %q", width, w, StripAnsi(l))
		}
	}
}

func TestRenderMarkdownRawHTML(t *testing.T) {
	plain := StripAnsi(strings.Join(RenderMarkdown("Returns Vec<String> here.", 80), "\n"))
	if !strings.Contains(plain, "Vec<String> here.") {
		t.Errorf("inline raw HTML lost: %q", plain)
	}
}

func TestRenderMarkdownHTMLBlock(t *testing.T) {
	plain := plainLines(RenderMarkdown("<div>\nhello\n</div>\n\nafter", 80))
	joined := strings.Join(plain, "\n")
	for _, want := range []string{"<div>", "hello", "</div>", "after"} {
		if !strings.Contains(joined, want) {
			t.Errorf("HTML block output missing %q: %q", want, joined)
		}
	}
}

func TestRenderMarkdownCodeBlockWrapped(t *testing.T) {
	for _, md := range []string{
		"```\n    abcdefghijklmnopqrstuvwxyz\n```",
		"        x = abcdefghijklmnopqrstuvwxyz",
	} {
		lines := RenderMarkdown(md, 10)
		assertMaxWidth(t, lines, 10)
		plain := plainLines(lines)
		if len(plain) < 2 {
			t.Fatalf("expected wrapped code, got %q", plain)
		}
		if !strings.HasPrefix(plain[0], "    ") {
			t.Errorf("indentation lost: %q", plain[0])
		}
		if !strings.Contains(strings.Join(plain, ""), "abcdefghijklmnopqrstuvwxyz") {
			t.Errorf("code content lost: %q", plain)
		}
	}
}

func TestRenderMarkdownHeadingWrapped(t *testing.T) {
	lines := RenderMarkdown("# A heading that is much longer than the width", 20)
	assertMaxWidth(t, lines, 20)
	plain := strings.Join(plainLines(lines), " ")
	if !strings.Contains(plain, "much longer") {
		t.Errorf("heading text lost: %q", plain)
	}
}

// sgrStateAt returns the SGR state in effect at byte offset idx of s.
func sgrStateAt(s string, idx int) sgrState {
	var st sgrState
	for _, m := range sgrSequence.FindAllString(s[:idx], -1) {
		st.apply(m)
	}
	return st
}

func TestRenderMarkdownHeadingStyleAfterEmphasis(t *testing.T) {
	lines := RenderMarkdown("## Title *em* `code` rest", 80)
	line := lines[0]
	idx := strings.Index(line, "rest")
	if idx < 0 {
		t.Fatalf("heading text missing: %q", line)
	}
	st := sgrStateAt(line, idx)
	if !st.bold || st.fg != "36" {
		t.Errorf("heading style lost after emphasis: %+v in %q", st, line)
	}
}

func TestRenderMarkdownNestedListIndent(t *testing.T) {
	lines := plainLines(RenderMarkdown("- a\n  - b\n    - c", 80))
	want := []string{"- a", "  - b", "    - c"}
	var got []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			got = append(got, l)
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("nested list = %q, want %q", got, want)
	}
}

func TestRenderMarkdownTableTooWide(t *testing.T) {
	md := "| Name | Description |\n|---|---|\n| foo | a long description that does not fit in the width |\n| bar | short |"
	lines := RenderMarkdown(md, 30)
	assertMaxWidth(t, lines, 30)
	plain := plainLines(lines)
	joined := strings.Join(plain, " ")
	for _, word := range []string{"foo", "bar", "long", "description", "width", "short"} {
		if !strings.Contains(joined, word) {
			t.Errorf("table lost %q: %q", word, plain)
		}
	}
	// Every table line has the same width and keeps its borders.
	tableW := VisualWidth(plain[0])
	for _, l := range plain {
		if l == "" {
			continue
		}
		if VisualWidth(l) != tableW {
			t.Errorf("table line width %d, want %d: %q", VisualWidth(l), tableW, l)
		}
		first, _ := utf8.DecodeRuneInString(l)
		if !strings.ContainsRune("┌├│└", first) {
			t.Errorf("table line has no left border: %q", l)
		}
	}
}

func TestRenderMarkdownTableFitsUnchanged(t *testing.T) {
	md := "| Name | Status |\n|---|---|\n| foo | ok |"
	plain := plainLines(RenderMarkdown(md, 80))
	want := []string{
		"┌──────┬────────┐",
		"│ Name │ Status │",
		"├──────┼────────┤",
		"│ foo  │ ok     │",
		"└──────┴────────┘",
	}
	if strings.Join(plain, "\n") != strings.Join(want, "\n") {
		t.Errorf("table =\n%s\nwant\n%s", strings.Join(plain, "\n"), strings.Join(want, "\n"))
	}
}

func TestHighlightCodeCacheEvictsOne(t *testing.T) {
	resetHighlightCache()
	for i := 0; i < highlightCacheMax; i++ {
		highlightCode(strings.Repeat("x", i+1), "")
	}
	highlightCode("overflow", "")
	if len(highlightCache) != highlightCacheMax {
		t.Fatalf("cache size = %d, want %d", len(highlightCache), highlightCacheMax)
	}
	if _, ok := highlightCache["\x00x"]; ok {
		t.Error("oldest entry should be evicted")
	}
	if _, ok := highlightCache["\x00xx"]; !ok {
		t.Error("second entry should still be cached")
	}
	if _, ok := highlightCache["\x00overflow"]; !ok {
		t.Error("new entry should be cached")
	}
}

func TestRenderMarkdownStreamingCodeNotCached(t *testing.T) {
	resetHighlightCache()
	RenderMarkdown("Text\n\n```go\nfunc a() {}\n", 80)
	if len(highlightCache) != 0 {
		t.Errorf("unterminated code block cached: size %d", len(highlightCache))
	}
	RenderMarkdown("Text\n\n```go\nfunc a() {}\n```\n", 80)
	if len(highlightCache) != 1 {
		t.Errorf("terminated code block not cached: size %d", len(highlightCache))
	}
}
