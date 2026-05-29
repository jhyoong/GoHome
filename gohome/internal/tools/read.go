package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const defaultReadLimit = 2000

// ReadTool implements the "read" tool.
type ReadTool struct{}

func (ReadTool) Name() string { return "read" }

func (ReadTool) Description() string {
	return "Read a file and return its contents with 1-indexed line numbers. " +
		"Use offset (1-indexed starting line) and limit (max lines, default 2000) to page through large files."
}

var readSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":   {"type": "string", "description": "Path of the file to read"},
    "offset": {"type": "integer", "description": "1-indexed starting line (default 1)"},
    "limit":  {"type": "integer", "description": "Maximum number of lines to return (default 2000)"}
  },
  "required": ["path"]
}`)

func (ReadTool) InputSchema() json.RawMessage { return readSchema }

type readInput struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset"`
	Limit  *int   `json:"limit"`
}

func (rt ReadTool) Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error) {
	var inp readInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: "read: invalid input: " + err.Error()}, nil
	}

	offset := 1
	if inp.Offset != nil && *inp.Offset > 0 {
		offset = *inp.Offset
	}
	limit := defaultReadLimit
	if inp.Limit != nil && *inp.Limit > 0 {
		limit = *inp.Limit
	}

	f, err := os.Open(inp.Path)
	if err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("read: cannot open %q: %v", inp.Path, err)}, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var sb strings.Builder
	lineno := 0
	written := 0

	for scanner.Scan() {
		lineno++
		if lineno < offset {
			continue
		}
		if written >= limit {
			break
		}
		fmt.Fprintf(&sb, "%d\t%s\n", lineno, scanner.Text())
		written++
	}
	if err := scanner.Err(); err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("read: error reading %q: %v", inp.Path, err)}, nil
	}

	content := sb.String()

	// Mark the file as read in the session if one is present.
	if s := SessionFrom(ctx); s != nil {
		s.MarkRead(inp.Path)
	}

	return Result{Content: content}, nil
}
