package tui

import (
	"strings"
	"unicode/utf8"
)

// Untrusted text (LLM replies, tool output, file contents) can carry terminal
// control sequences that write the clipboard (OSC 52), clear the screen or
// move the cursor to fake output. The sanitizer below removes all of them,
// colors included, before the text is stored for display. The app's own
// styling is added later at render time, so it is not affected.
//
// Removed:
//   - ESC sequences: CSI (ESC [ ... final byte), OSC/DCS/SOS/PM/APC
//     (ESC ] / ESC P / ESC X / ESC ^ / ESC _ ... ended by BEL or ST),
//     and short ESC sequences (ESC, optional 0x20-0x2F bytes, final byte).
//   - 8-bit C1 controls U+0080-U+009F. C1 CSI (U+009B) and the C1 string
//     introducers (U+0090, U+0098, U+009D, U+009E, U+009F) are removed with
//     their bodies.
//   - All other C0 controls and DEL, except '\n' and '\t'. '\r' is removed,
//     so "\r\n" becomes "\n" and a lone '\r' disappears. Removing it (instead
//     of turning it into '\n') never adds lines, which keeps diff line numbers
//     aligned and progress-bar output on one line.
//   - Invalid UTF-8 bytes.
//
// An unterminated sequence at the end of the text is dropped. The existing
// StripAnsi helper is not used here because it misses several of these forms.

// sanitizeText returns s with all terminal control sequences and control
// characters removed except newline and tab.
func sanitizeText(s string) string {
	clean, _ := sanitize(s, true)
	return clean
}

// sanitizeStream sanitizes one chunk of streamed text. An incomplete
// sequence or UTF-8 character at the end is returned as rest instead of
// being dropped, so the caller can prepend it to the next chunk. This keeps
// a sequence split across two chunks from leaking its tail as plain text.
func sanitizeStream(s string) (clean, rest string) {
	return sanitize(s, false)
}

// sanitize does the work for sanitizeText and sanitizeStream. When final is
// true an incomplete trailing sequence is dropped; otherwise it is returned
// as rest.
func sanitize(s string, final bool) (clean, rest string) {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			if !final && !utf8.FullRuneInString(s[i:]) {
				return b.String(), s[i:]
			}
			i++ // invalid byte
		case r == '\n' || r == '\t':
			b.WriteRune(r)
			i += size
		case r == 0x1b || (r >= 0x80 && r <= 0x9f):
			n, ok := escapeLen(s[i:])
			if !ok {
				if final {
					return b.String(), ""
				}
				return b.String(), s[i:]
			}
			i += n
		case r < 0x20 || r == 0x7f:
			i += size
		default:
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String(), ""
}

// escapeLen returns the byte length of the escape sequence at the start of
// s, which begins with ESC or a C1 control. ok is false when the sequence is
// not finished before the end of s.
func escapeLen(s string) (n int, ok bool) {
	if s[0] == 0x1b {
		if len(s) < 2 {
			return 0, false
		}
		c := s[1]
		switch {
		case c == '[':
			return csiLen(s, 2)
		case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
			return stringSeqLen(s, 2)
		case c >= 0x20 && c <= 0x2f:
			j := 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
				j++
			}
			if j == len(s) {
				return 0, false
			}
			if s[j] >= 0x30 && s[j] <= 0x7e {
				return j + 1, true
			}
			return j, true // malformed: drop what was read
		case c >= 0x30 && c <= 0x7e:
			return 2, true
		default:
			return 1, true // ESC before a control or non-ASCII: drop the ESC
		}
	}
	r, size := utf8.DecodeRuneInString(s)
	switch r {
	case 0x9b:
		return csiLen(s, size)
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return stringSeqLen(s, size)
	}
	return size, true
}

// csiLen scans CSI parameter and intermediate bytes from start and returns
// the length up to and including the final byte. A byte outside the CSI
// ranges ends the sequence without being consumed.
func csiLen(s string, start int) (int, bool) {
	for j := start; j < len(s); j++ {
		c := s[j]
		switch {
		case c >= 0x40 && c <= 0x7e:
			return j + 1, true
		case c >= 0x20 && c <= 0x3f:
		default:
			return j, true
		}
	}
	return 0, false
}

// stringSeqLen scans an OSC/DCS/SOS/PM/APC body from start and returns the
// length up to and including the terminator: BEL, ESC \ or C1 ST (U+009C).
// An ESC followed by anything else ends the body without being consumed, so
// it is handled as the start of a new sequence.
func stringSeqLen(s string, start int) (int, bool) {
	for j := start; j < len(s); j++ {
		switch s[j] {
		case 0x07:
			return j + 1, true
		case 0x1b:
			if j+1 == len(s) {
				return 0, false
			}
			if s[j+1] == '\\' {
				return j + 2, true
			}
			return j, true
		case 0xc2: // first byte of U+009C in UTF-8
			if j+1 == len(s) {
				return 0, false
			}
			if s[j+1] == 0x9c {
				return j + 2, true
			}
		}
	}
	return 0, false
}
