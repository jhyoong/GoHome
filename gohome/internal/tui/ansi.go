package tui

import (
	"regexp"
	"strings"

	"github.com/rivo/uniseg"
)

// ansiEscape matches ANSI CSI sequences (including SGR, cursor movement, etc.)
// and OSC sequences. Order matters: more specific patterns first.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\`)

// sgrSequence matches only SGR (Select Graphic Rendition) sequences, i.e. colour/style codes.
var sgrSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripAnsi removes all ANSI escape sequences from s.
func StripAnsi(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// VisualWidth returns the number of display columns occupied by s,
// ignoring any ANSI escape sequences and accounting for wide (CJK) characters.
func VisualWidth(s string) int {
	clean := StripAnsi(s)
	return uniseg.StringWidth(clean)
}

// TruncateText truncates s so that its visual width does not exceed maxWidth
// display columns. ANSI escape sequences are preserved; if an SGR sequence was
// active at the truncation point an ANSI reset (\x1b[0m) is appended.
func TruncateText(s string, maxWidth int) string {
	if maxWidth <= 0 || s == "" {
		return ""
	}
	if VisualWidth(s) <= maxWidth {
		return s
	}

	var out strings.Builder
	col := 0
	sgrActive := false
	i := 0

	for i < len(s) {
		// Check for an escape sequence at position i.
		if s[i] == '\x1b' {
			// Try to find how long this escape sequence is.
			loc := ansiEscape.FindStringIndex(s[i:])
			if loc != nil && loc[0] == 0 {
				seq := s[i : i+loc[1]]
				out.WriteString(seq)
				// Track whether an SGR sequence is active.
				if sgrSequence.MatchString(seq) {
					// A reset sequence turns off SGR.
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						sgrActive = false
					} else {
						sgrActive = true
					}
				}
				i += loc[1]
				continue
			}
		}

		// Get the next grapheme cluster.
		cluster, rest, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		w := uniseg.StringWidth(cluster)

		if col+w > maxWidth {
			break
		}
		col += w
		out.WriteString(cluster)
		i += len(s[i:]) - len(rest)
	}

	if sgrActive {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

// WrapText wraps s at word boundaries so each line fits within maxWidth display
// columns. ANSI SGR state is tracked: active SGR is re-emitted at the start of
// each continuation line and reset at the end of a broken line.
func WrapText(s string, maxWidth int) []string {
	if s == "" {
		return []string{""}
	}

	// Split into tokens (words and spaces). We'll process each word.
	// We use a simple approach: split on spaces, then reassemble with wrapping.
	// To handle ANSI correctly we need to track SGR state as we go.

	var lines []string
	var currentLine strings.Builder
	currentWidth := 0
	activeSGR := "" // the last SGR sequence before truncation point

	// We'll walk through the string rune by rune, tracking words and ANSI.
	// Collect the current "word" (non-space run, possibly containing ANSI).
	type token struct {
		text  string // raw text including ANSI
		width int    // visual width of text
	}

	// Tokenise into words and spaces.
	var tokens []token
	i := 0
	var cur strings.Builder
	curWidth := 0

	flushCur := func(isSpace bool) {
		if cur.Len() > 0 {
			tokens = append(tokens, token{cur.String(), curWidth})
			cur.Reset()
			curWidth = 0
		}
	}
	_ = flushCur

	// Simple tokeniser: run through the string collecting words (separated by spaces).
	// ANSI escapes inside a word are kept with the word.
	inSpace := false
	for i < len(s) {
		if s[i] == '\x1b' {
			loc := ansiEscape.FindStringIndex(s[i:])
			if loc != nil && loc[0] == 0 {
				cur.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}

		cluster, rest, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		cWidth := uniseg.StringWidth(cluster)
		isCurrentSpace := cluster == " "

		if isCurrentSpace != inSpace {
			if cur.Len() > 0 {
				tokens = append(tokens, token{cur.String(), curWidth})
				cur.Reset()
				curWidth = 0
			}
			inSpace = isCurrentSpace
		}
		cur.WriteString(cluster)
		curWidth += cWidth
		i += len(s[i:]) - len(rest)
	}
	if cur.Len() > 0 {
		tokens = append(tokens, token{cur.String(), curWidth})
	}

	// Now wrap tokens into lines.
	// activeSGR tracks the current SGR prefix to emit on new lines.
	resetSeq := "\x1b[0m"

	for _, tok := range tokens {
		// If this token is spaces, handle differently: spaces only go on the
		// current line if there's room and it's not at the start of a new line.
		isSpace := strings.TrimFunc(StripAnsi(tok.text), func(r rune) bool { return r == ' ' }) == "" &&
			strings.Contains(tok.text, " ")

		if isSpace {
			// Only emit spaces if we're in the middle of a line.
			if currentWidth > 0 && currentWidth+tok.width <= maxWidth {
				currentLine.WriteString(tok.text)
				currentWidth += tok.width
				// Update activeSGR for any SGR in spaces (unlikely but possible).
				activeSGR = updateActiveSGR(activeSGR, tok.text)
			}
			// If spaces don't fit, just skip them (don't wrap on a space).
			continue
		}

		// Non-space token (a word). It may itself be wider than maxWidth, in
		// which case we force-break it.
		wordText := tok.text
		wordWidth := tok.width

		for wordWidth > 0 {
			remaining := maxWidth - currentWidth
			if remaining <= 0 {
				// Current line is full, flush it.
				if activeSGR != "" {
					currentLine.WriteString(resetSeq)
				}
				lines = append(lines, currentLine.String())
				currentLine.Reset()
				currentWidth = 0
				if activeSGR != "" {
					currentLine.WriteString(activeSGR)
				}
				remaining = maxWidth
			}

			// Does the whole word fit on the remaining space?
			if wordWidth <= remaining {
				currentLine.WriteString(wordText)
				currentWidth += wordWidth
				activeSGR = updateActiveSGR(activeSGR, wordText)
				wordWidth = 0
			} else {
				// Force-break: take as many graphemes as fit.
				chunk, rest2 := takeWidth(wordText, remaining)
				currentLine.WriteString(chunk)
				activeSGR = updateActiveSGR(activeSGR, chunk)

				// Flush this line.
				if activeSGR != "" {
					currentLine.WriteString(resetSeq)
				}
				lines = append(lines, currentLine.String())
				currentLine.Reset()
				currentWidth = 0
				if activeSGR != "" {
					currentLine.WriteString(activeSGR)
				}

				wordText = rest2
				wordWidth = VisualWidth(StripAnsi(rest2))
			}
		}
	}

	// Flush the last line.
	if activeSGR != "" && currentLine.Len() > 0 {
		// Only reset if there's actual content (not just the SGR prefix).
		// Check if we wrote anything beyond the SGR prefix.
		lineStr := currentLine.String()
		stripped := StripAnsi(lineStr)
		if len(stripped) > 0 {
			currentLine.WriteString(resetSeq)
		}
	}
	lines = append(lines, currentLine.String())

	return lines
}

// updateActiveSGR scans text for SGR sequences and returns the last active one
// (or "" if the last SGR was a reset).
func updateActiveSGR(current, text string) string {
	matches := sgrSequence.FindAllString(text, -1)
	for _, m := range matches {
		if m == "\x1b[0m" || m == "\x1b[m" {
			current = ""
		} else {
			current = m
		}
	}
	return current
}

// takeWidth returns (prefix, suffix) where prefix contains as many grapheme
// clusters from s (including ANSI escapes) as fit within maxWidth display cols.
func takeWidth(s string, maxWidth int) (string, string) {
	var prefix strings.Builder
	col := 0
	i := 0

	for i < len(s) {
		if s[i] == '\x1b' {
			loc := ansiEscape.FindStringIndex(s[i:])
			if loc != nil && loc[0] == 0 {
				seq := s[i : i+loc[1]]
				prefix.WriteString(seq)
				i += loc[1]
				continue
			}
		}

		cluster, rest, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		w := uniseg.StringWidth(cluster)
		if col+w > maxWidth {
			return prefix.String(), s[i:]
		}
		col += w
		prefix.WriteString(cluster)
		i += len(s[i:]) - len(rest)
	}
	return prefix.String(), ""
}
