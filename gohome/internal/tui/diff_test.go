package tui

import "testing"

func TestGenerateEditDiff(t *testing.T) {
	fileContent := "line1\nline2\nline3\nfunc Old() {\nline5\nline6\nline7\n"

	tests := []struct {
		name      string
		content   string
		oldStr    string
		newStr    string
		wantEmpty bool
	}{
		{
			name:    "simple single-line replacement",
			content: fileContent,
			oldStr:  "func Old() {",
			newStr:  "func New() {",
		},
		{
			name:    "multi-line replacement",
			content: fileContent,
			oldStr:  "func Old() {\nline5",
			newStr:  "func New() {\n    return nil",
		},
		{
			name:      "no match returns empty",
			content:   fileContent,
			oldStr:    "nonexistent",
			newStr:    "replacement",
			wantEmpty: true,
		},
		{
			name:    "match at start of file",
			content: fileContent,
			oldStr:  "line1",
			newStr:  "lineA",
		},
		{
			name:    "match at end of file",
			content: "line1\nline2\nline3\n",
			oldStr:  "line3",
			newStr:  "lineZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := generateEditDiff(tt.content, tt.oldStr, tt.newStr)
			if tt.wantEmpty {
				if diff != "" {
					t.Errorf("expected empty diff, got:\n%s", diff)
				}
				return
			}
			if diff == "" {
				t.Error("expected non-empty diff")
			}
			t.Logf("diff output:\n%s", diff)
		})
	}
}
