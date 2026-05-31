package tui

import "testing"

func TestVisualWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"", 0},
		{"\x1b[31mred\x1b[0m", 3},
		{"\x1b[1;32mbold green\x1b[0m", 10},
		{"日本語", 6},
		{"\x1b[34m日本\x1b[0m", 4},
		{"a\x1b[38;2;255;0;0mb\x1b[0mc", 3},
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
