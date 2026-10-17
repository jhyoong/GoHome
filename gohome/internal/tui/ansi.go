package tui

import (
	"regexp"
	"strconv"
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
// columns. ANSI SGR state is tracked: the combined active SGR state is
// re-emitted at the start of each continuation line and reset at the end of a
// broken line.
// Newline characters are respected: the text is split on \n first, then each
// segment is wrapped individually, with SGR state carried across segments.
func WrapText(s string, maxWidth int) []string {
	if s == "" {
		return []string{""}
	}

	segments := strings.Split(s, "\n")
	var all []string
	var state sgrState
	for _, seg := range segments {
		var lines []string
		lines, state = wrapSingleLine(seg, maxWidth, state)
		all = append(all, lines...)
	}
	return all
}

// HardWrapText breaks s into lines of at most maxWidth display columns,
// cutting at any grapheme instead of at word boundaries. Unlike WrapText it
// keeps every space, so leading indentation survives. Used for code blocks.
// SGR state is carried across breaks the same way as in WrapText.
func HardWrapText(s string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	var lines []string
	var state sgrState
	for _, seg := range strings.Split(s, "\n") {
		prefix := state.seq()
		rest := seg
		for {
			if VisualWidth(rest) <= maxWidth {
				state.applyAll(rest)
				lines = append(lines, prefix+rest)
				break
			}
			chunk, next := takeWidth(rest, maxWidth)
			if StripAnsi(chunk) == "" {
				// A grapheme wider than the line: take it anyway, otherwise
				// the loop never progresses.
				chunk, next = takeGrapheme(rest)
			}
			if VisualWidth(next) == 0 {
				// Only escape codes are left: keep them on this line.
				chunk, next = chunk+next, ""
			}
			state.applyAll(chunk)
			line := prefix + chunk
			if state.active() {
				line += ansiResetSeq
			}
			lines = append(lines, line)
			if next == "" {
				break
			}
			prefix = state.seq()
			rest = next
		}
	}
	return lines
}

const ansiResetSeq = "\x1b[0m"

// wrapSingleLine wraps a single line (no newlines) at word boundaries so each
// output line fits within maxWidth display columns. state is the SGR state
// active at the start of the line. Returns the wrapped lines and the final
// SGR state (for carrying across newline-separated segments).
func wrapSingleLine(s string, maxWidth int, state sgrState) ([]string, sgrState) {
	if s == "" {
		return []string{""}, state
	}

	var lines []string
	if maxWidth < 1 {
		maxWidth = 1
	}
	var currentLine strings.Builder
	currentWidth := 0

	type token struct {
		text  string // raw text including ANSI
		width int    // visual width of text
	}

	// Tokenise into words and spaces. ANSI escapes are kept with the token
	// they appear in.
	var tokens []token
	i := 0
	var cur strings.Builder
	curWidth := 0
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

	// flushLine ends the current line (resetting any active SGR) and starts a
	// new one with the active SGR state re-emitted.
	flushLine := func() {
		if state.active() {
			currentLine.WriteString(ansiResetSeq)
		}
		lines = append(lines, currentLine.String())
		currentLine.Reset()
		currentWidth = 0
		currentLine.WriteString(state.seq())
	}

	// If we have an initial SGR, emit it at the start of the first line.
	currentLine.WriteString(state.seq())

	// Spaces are held back until the next word: they are written only when
	// that word stays on the same line, so lines never end in a space at a
	// break and never start with one.
	var pending strings.Builder
	pendingWidth := 0
	// writePending writes the held spaces if keep is set and they fit on the
	// current line; otherwise only their escape sequences are kept.
	writePending := func(keep bool) {
		if keep && currentWidth > 0 && currentWidth+pendingWidth <= maxWidth {
			currentLine.WriteString(pending.String())
			currentWidth += pendingWidth
		} else {
			for _, seq := range ansiEscape.FindAllString(pending.String(), -1) {
				currentLine.WriteString(seq)
			}
		}
		state.applyAll(pending.String())
		pending.Reset()
		pendingWidth = 0
	}

	for _, tok := range tokens {
		isSpace := strings.TrimFunc(StripAnsi(tok.text), func(r rune) bool { return r == ' ' }) == "" &&
			strings.Contains(tok.text, " ")
		if isSpace {
			pending.WriteString(tok.text)
			pendingWidth += tok.width
			continue
		}

		wordText := tok.text
		wordWidth := tok.width

		if wordWidth == 0 {
			// Zero-width token (only escape codes or zero-width characters):
			// keep it on the current line.
			writePending(true)
			currentLine.WriteString(wordText)
			state.applyAll(wordText)
			continue
		}

		if currentWidth > 0 && currentWidth+pendingWidth+wordWidth > maxWidth {
			// The word does not fit after the spaces: drop the spaces (but
			// keep their escape codes) and move the word to a new line.
			writePending(false)
			flushLine()
		} else {
			writePending(true)
		}

		// The word may itself be wider than maxWidth, in which case we
		// force-break it.
		for wordWidth > 0 {
			remaining := maxWidth - currentWidth
			if remaining <= 0 {
				flushLine()
				remaining = maxWidth
			}

			// Does the whole word fit on the remaining space?
			if wordWidth <= remaining {
				currentLine.WriteString(wordText)
				currentWidth += wordWidth
				state.applyAll(wordText)
				wordWidth = 0
			} else {
				// Force-break: take as many graphemes as fit.
				chunk, rest2 := takeWidth(wordText, remaining)
				if chunk == "" && currentWidth == 0 {
					// A grapheme wider than the line on an empty line: take
					// it anyway, otherwise the loop never progresses.
					chunk, rest2 = takeGrapheme(wordText)
				}
				currentLine.WriteString(chunk)
				state.applyAll(chunk)
				flushLine()

				wordText = rest2
				wordWidth = VisualWidth(rest2)
			}
		}
	}
	writePending(true)

	// Flush the last line. Only reset if there's actual content (not just
	// the SGR prefix).
	if state.active() && len(StripAnsi(currentLine.String())) > 0 {
		currentLine.WriteString(ansiResetSeq)
	}
	lines = append(lines, currentLine.String())

	return lines, state
}

// sgrState is the combined SGR state built up by a run of SGR sequences.
// fg and bg hold the raw colour parameters (e.g. "31" or "38;5;12").
type sgrState struct {
	bold, dim, italic, underline, reverse bool
	fg, bg                                string
}

// active reports whether any attribute is set.
func (s sgrState) active() bool {
	return s != sgrState{}
}

// seq returns a single SGR sequence that recreates the state, or "" when
// nothing is set.
func (s sgrState) seq() string {
	var codes []string
	for _, a := range []struct {
		on   bool
		code string
	}{{s.bold, "1"}, {s.dim, "2"}, {s.italic, "3"}, {s.underline, "4"}, {s.reverse, "7"}} {
		if a.on {
			codes = append(codes, a.code)
		}
	}
	if s.fg != "" {
		codes = append(codes, s.fg)
	}
	if s.bg != "" {
		codes = append(codes, s.bg)
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// applyAll applies every SGR sequence found in text to the state.
func (s *sgrState) applyAll(text string) {
	for _, m := range sgrSequence.FindAllString(text, -1) {
		s.apply(m)
	}
}

// apply updates the state with one SGR sequence (e.g. "\x1b[1;36m").
// Unknown codes are ignored.
func (s *sgrState) apply(seq string) {
	params := strings.Split(seq[2:len(seq)-1], ";")
	for i := 0; i < len(params); i++ {
		n := 0
		if params[i] != "" {
			var err error
			if n, err = strconv.Atoi(params[i]); err != nil {
				continue
			}
		}
		switch {
		case n == 0:
			*s = sgrState{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.dim = true
		case n == 3:
			s.italic = true
		case n == 4:
			s.underline = true
		case n == 7:
			s.reverse = true
		case n == 22:
			s.bold, s.dim = false, false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = false
		case n == 27:
			s.reverse = false
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.fg = params[i]
		case n == 39:
			s.fg = ""
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			s.bg = params[i]
		case n == 49:
			s.bg = ""
		case n == 38 || n == 48:
			// Extended colour: 38;5;N or 38;2;R;G;B (48 for background).
			end := i + 1
			if end < len(params) && params[end] == "5" {
				end += 2
			} else if end < len(params) && params[end] == "2" {
				end += 4
			}
			if end > len(params) {
				end = len(params)
			}
			val := strings.Join(params[i:end], ";")
			if n == 38 {
				s.fg = val
			} else {
				s.bg = val
			}
			i = end - 1
		}
	}
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

// takeGrapheme returns the first grapheme cluster of s (with any leading ANSI
// sequences) and the remainder. It always consumes at least one cluster when s
// holds visible text.
func takeGrapheme(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] == '\x1b' {
		loc := ansiEscape.FindStringIndex(s[i:])
		if loc == nil || loc[0] != 0 {
			break
		}
		i += loc[1]
	}
	if i >= len(s) {
		return s, ""
	}
	_, rest, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
	end := len(s) - len(rest)
	return s[:end], s[end:]
}
