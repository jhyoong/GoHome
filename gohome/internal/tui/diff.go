package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const diffContextLines = 2

type editDiffInput struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// buildDiffPreview parses edit tool InputJSON, reads the target file, and
// returns a pre-formatted unified diff string. Returns "" on any failure.
func buildDiffPreview(inputJSON string) string {
	var inp editDiffInput
	if err := json.Unmarshal([]byte(inputJSON), &inp); err != nil {
		return ""
	}
	if inp.Path == "" || inp.OldString == "" {
		return ""
	}
	data, err := os.ReadFile(inp.Path)
	if err != nil {
		return generateEditDiff(inp.OldString, inp.OldString, inp.NewString)
	}
	return generateEditDiff(string(data), inp.OldString, inp.NewString)
}

// generateEditDiff builds a unified diff with line numbers showing old_string
// replaced by new_string within fileContent. Returns "" if old_string is not found.
func generateEditDiff(fileContent, oldStr, newStr string) string {
	idx := strings.Index(fileContent, oldStr)
	if idx < 0 {
		return ""
	}

	fileLines := strings.Split(fileContent, "\n")
	startLine := strings.Count(fileContent[:idx], "\n")

	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	ctxStart := startLine - diffContextLines
	if ctxStart < 0 {
		ctxStart = 0
	}
	ctxEnd := startLine + len(oldLines) + diffContextLines
	if ctxEnd > len(fileLines) {
		ctxEnd = len(fileLines)
	}

	maxLineNum := ctxEnd
	numWidth := len(fmt.Sprintf("%d", maxLineNum))
	fmtNum := fmt.Sprintf("%%%dd", numWidth)

	var sb strings.Builder

	// Context lines above.
	for i := ctxStart; i < startLine; i++ {
		fmt.Fprintf(&sb, "%s    %s\n", fmt.Sprintf(fmtNum, i+1), fileLines[i])
	}

	// Removed lines (old_string).
	for j, l := range oldLines {
		fmt.Fprintf(&sb, "%s  - %s\n", fmt.Sprintf(fmtNum, startLine+j+1), l)
	}

	// Added lines (new_string).
	for j, l := range newLines {
		fmt.Fprintf(&sb, "%s  + %s\n", fmt.Sprintf(fmtNum, startLine+j+1), l)
	}

	// Context lines below.
	afterLine := startLine + len(oldLines)
	for i := afterLine; i < ctxEnd; i++ {
		fmt.Fprintf(&sb, "%s    %s\n", fmt.Sprintf(fmtNum, i+1), fileLines[i])
	}

	return strings.TrimRight(sb.String(), "\n")
}
