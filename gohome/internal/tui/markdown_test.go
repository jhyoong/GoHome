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
