package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	defaultReadLimit        = 2000
	maxReadLineRunes        = 2000
	maxReadOutputBytes      = 100 * 1024
	readLineTruncatedMarker = " [line truncated]"
)

// ReadTool implements the "read" tool.
type ReadTool struct{}

func (ReadTool) Name() string { return "read" }

func (ReadTool) Description() string {
	return "Read a file and return its contents with 1-indexed line numbers. " +
		"Use offset (1-indexed starting line) and limit (max lines, default 2000) to page through large files. " +
		"Lines longer than 2000 characters are cut, and output stops at 100 KiB with a note giving the offset to continue from."
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
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	var sb strings.Builder
	lineno := 0
	written := 0

	for written < limit {
		line, err := readLineCapped(r, lineno+1 >= offset)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{IsError: true, Content: fmt.Sprintf("read: error reading %q: %v", inp.Path, err)}, nil
		}
		lineno++
		if lineno < offset {
			continue
		}
		entry := fmt.Sprintf("%d\t%s\n", lineno, line)
		if written > 0 && sb.Len()+len(entry) > maxReadOutputBytes {
			last := lineno - 1
			fmt.Fprintf(&sb, "[output truncated at line %d; use offset=%d to continue]\n", last, last+1)
			break
		}
		sb.WriteString(entry)
		written++
	}

	content := sb.String()

	// Mark the file as read in the session if one is present.
	if s := SessionFrom(ctx); s != nil {
		s.MarkRead(inp.Path)
	}

	return Result{Content: content}, nil
}

// readLineCapped reads one line from r. The trailing "\n" and a trailing "\r"
// are stripped. Lines of any length are consumed fully, but only the first
// maxReadLineRunes runes are kept; longer lines get readLineTruncatedMarker.
// When keep is false the line is consumed and discarded. It returns io.EOF
// only when no more lines remain (a last line without "\n" is still returned).
func readLineCapped(r *bufio.Reader, keep bool) (string, error) {
	const maxKeepBytes = maxReadLineRunes * utf8.UTFMax
	var buf []byte
	overflow := false
	got := false
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			if err == io.EOF && got {
				break
			}
			return "", err
		}
		got = true
		if keep {
			room := maxKeepBytes + 1 - len(buf)
			if len(chunk) > room {
				chunk = chunk[:room]
				overflow = true
			}
			if room > 0 {
				buf = append(buf, chunk...)
			}
		}
		if !isPrefix {
			break
		}
	}
	if !keep {
		return "", nil
	}
	if !overflow && utf8.RuneCount(buf) <= maxReadLineRunes {
		return string(buf), nil
	}
	// Cut on a rune boundary after maxReadLineRunes runes.
	n, i := 0, 0
	for i < len(buf) && n < maxReadLineRunes {
		_, size := utf8.DecodeRune(buf[i:])
		i += size
		n++
	}
	return string(buf[:i]) + readLineTruncatedMarker, nil
}
