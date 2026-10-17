package tui

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const (
	ansiBold      = "\x1b[1m"
	ansiItalic    = "\x1b[3m"
	ansiUnderline = "\x1b[4m"
	ansiDim       = "\x1b[2m"
	ansiReverse   = "\x1b[7m"
	ansiReset     = "\x1b[0m"
	ansiCyan      = "\x1b[36m"
)

var mdParser parser.Parser

func init() {
	md := goldmark.New(goldmark.WithExtensions(extension.Table))
	mdParser = md.Parser()
}

// RenderMarkdown parses markdown source and returns terminal-ready lines
// styled with ANSI escape sequences, wrapped to fit within width columns.
func RenderMarkdown(source string, width int) []string {
	if strings.TrimSpace(source) == "" {
		return nil
	}

	src := []byte(source)
	reader := text.NewReader(src)
	doc := mdParser.Parse(reader)

	var lines []string
	renderNode(doc, src, width, false, false, &lines)

	// Strip trailing blank lines to avoid double-spacing between timeline entries.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// renderNode walks an AST node and appends rendered lines to out.
func renderNode(node ast.Node, src []byte, width int, bold, italic bool, out *[]string) {
	switch n := node.(type) {

	case *ast.Document:
		renderChildren(n, src, width, bold, italic, out)

	case *ast.Heading:
		style := ansiCyan + ansiBold
		if n.Level == 1 {
			style += ansiUnderline
		}
		prefix := ""
		if n.Level >= 3 {
			prefix = strings.Repeat("#", n.Level) + " "
		}
		text := extractInline(n, src, bold, italic)
		// Inline styles end with a reset; turn the heading style back on.
		text = strings.ReplaceAll(text, ansiReset, ansiReset+style)
		*out = append(*out, WrapText(style+prefix+text+ansiReset, width)...)
		// blank line after heading
		*out = append(*out, "")

	case *ast.Paragraph:
		text := extractInline(n, src, bold, italic)
		wrapped := WrapText(text, width)
		*out = append(*out, wrapped...)
		// blank line after paragraph
		*out = append(*out, "")

	case *ast.FencedCodeBlock:
		lang := string(n.Language(src))
		var codeLines []string
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			codeLines = append(codeLines, string(line.Value(src)))
		}
		code := strings.Join(codeLines, "")
		// A block without its closing fence is still streaming; caching it
		// would fill the cache with one entry per token.
		var highlighted string
		if fenceClosed(n, src) {
			highlighted = highlightCode(code, lang)
		} else {
			highlighted = highlightUncached(code, lang)
		}
		*out = append(*out, HardWrapText(strings.TrimRight(highlighted, "\n"), width)...)
		*out = append(*out, "")

	case *ast.CodeBlock:
		var codeLines []string
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			codeLines = append(codeLines, string(line.Value(src)))
		}
		code := strings.Join(codeLines, "")
		highlighted := highlightCode(code, "")
		*out = append(*out, HardWrapText(strings.TrimRight(highlighted, "\n"), width)...)
		*out = append(*out, "")

	case *ast.HTMLBlock:
		// Shown as plain text.
		var html strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			html.Write(line.Value(src))
		}
		if n.HasClosure() {
			html.Write(n.ClosureLine.Value(src))
		}
		*out = append(*out, WrapText(strings.TrimRight(html.String(), "\n"), width)...)
		*out = append(*out, "")

	case *ast.Blockquote:
		var inner []string
		renderChildren(n, src, width-2, bold, italic, &inner)
		for _, l := range inner {
			*out = append(*out, ansiDim+"| "+ansiReset+l)
		}

	case *ast.List:
		renderList(n, src, width, bold, italic, out)

	case *ast.ListItem:
		// ListItem rendering is handled inside renderList
		renderChildren(n, src, width, bold, italic, out)

	case *ast.ThematicBreak:
		*out = append(*out, ansiDim+strings.Repeat("─", max(width, 1))+ansiReset)
		*out = append(*out, "")

	default:
		if node.Kind() == east.KindTable {
			renderTable(node, src, width, bold, italic, out)
			break
		}
		// For unrecognized block nodes, recurse into children
		renderChildren(node, src, width, bold, italic, out)
	}
}

// renderChildren iterates over child nodes and renders each.
func renderChildren(node ast.Node, src []byte, width int, bold, italic bool, out *[]string) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		renderNode(child, src, width, bold, italic, out)
	}
}

// renderList handles ordered and unordered lists with correct bullet/number prefixes.
// Nested content is rendered at the item's inner width and indented by the
// item's prefix width, so each nesting level adds one indent step.
func renderList(list *ast.List, src []byte, width int, bold, italic bool, out *[]string) {
	itemNum := list.Start
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		item, ok := child.(*ast.ListItem)
		if !ok {
			continue
		}

		var prefix string
		if list.IsOrdered() {
			prefix = fmt.Sprintf("%d. ", itemNum)
			itemNum++
		} else {
			prefix = "- "
		}
		innerWidth := width - len(prefix)
		if innerWidth < 10 {
			innerWidth = 10
		}

		// Collect lines for this item's content
		var itemLines []string
		for sub := item.FirstChild(); sub != nil; sub = sub.NextSibling() {
			switch sn := sub.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				text := extractInline(sn, src, bold, italic)
				wrapped := WrapText(text, innerWidth)
				itemLines = append(itemLines, wrapped...)
			case *ast.List:
				var nested []string
				renderList(sn, src, innerWidth, bold, italic, &nested)
				itemLines = append(itemLines, nested...)
			default:
				var sub2Lines []string
				renderNode(sub, src, innerWidth, bold, italic, &sub2Lines)
				itemLines = append(itemLines, sub2Lines...)
			}
		}

		// Emit: first line gets the prefix, continuation lines get indentation
		contIndent := strings.Repeat(" ", len(prefix))
		for i, l := range itemLines {
			if i == 0 {
				*out = append(*out, prefix+l)
			} else {
				*out = append(*out, contIndent+l)
			}
		}
	}
	// blank line after list
	*out = append(*out, "")
}

// renderTable handles GFM table nodes, rendering them with box-drawing borders.
func renderTable(node ast.Node, src []byte, width int, bold, italic bool, out *[]string) {
	var rows [][]string
	headerCount := 0

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		isHeader := child.Kind() == east.KindTableHeader
		var cells []string
		for cell := child.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, extractInline(cell, src, bold, italic))
		}
		rows = append(rows, cells)
		if isHeader {
			headerCount++
		}
	}

	if len(rows) == 0 {
		return
	}

	// Determine number of columns and measure widths.
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	colWidths := make([]int, numCols)
	for _, row := range rows {
		for c, cell := range row {
			w := VisualWidth(StripAnsi(cell))
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	fitColumns(colWidths, width)

	// Build separator line.
	var sepParts []string
	for _, w := range colWidths {
		sepParts = append(sepParts, strings.Repeat("─", w+2))
	}
	sep := ansiDim + "├" + strings.Join(sepParts, "┼") + "┤" + ansiReset
	topBorder := ansiDim + "┌" + strings.Join(sepParts, "┬") + "┐" + ansiReset
	bottomBorder := ansiDim + "└" + strings.Join(sepParts, "┴") + "┘" + ansiReset

	*out = append(*out, topBorder)
	for i, row := range rows {
		// Wrap each cell that is wider than its column.
		cellLines := make([][]string, numCols)
		height := 1
		for c := 0; c < numCols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			if VisualWidth(cell) > colWidths[c] {
				cellLines[c] = WrapText(cell, colWidths[c])
			} else {
				cellLines[c] = []string{cell}
			}
			height = max(height, len(cellLines[c]))
		}
		for li := 0; li < height; li++ {
			var parts []string
			for c := 0; c < numCols; c++ {
				cell := ""
				if li < len(cellLines[c]) {
					cell = cellLines[c][li]
				}
				padded := cell + strings.Repeat(" ", max(colWidths[c]-VisualWidth(cell), 0))
				if i < headerCount {
					padded = ansiBold + padded + ansiReset
				}
				parts = append(parts, " "+padded+" ")
			}
			*out = append(*out, ansiDim+"│"+ansiReset+strings.Join(parts, ansiDim+"│"+ansiReset)+ansiDim+"│"+ansiReset)
		}
		if i < headerCount {
			*out = append(*out, sep)
		}
	}
	*out = append(*out, bottomBorder)
	*out = append(*out, "")
}

// fitColumns shrinks the widest columns, one column at a time, until the
// table (cell padding and borders included) fits within width. Columns never
// shrink below one column.
func fitColumns(colWidths []int, width int) {
	// Each column has one space of padding on both sides plus one border,
	// and the table has one extra border on the right.
	avail := width - 3*len(colWidths) - 1
	total := 0
	for _, w := range colWidths {
		total += w
	}
	for total > avail {
		widest := 0
		for c, w := range colWidths {
			if w > colWidths[widest] {
				widest = c
			}
		}
		if colWidths[widest] <= 1 {
			return
		}
		colWidths[widest]--
		total--
	}
}

// fenceClosed reports whether a fenced code block has its closing fence in
// src. A block without one is still being streamed.
func fenceClosed(n *ast.FencedCodeBlock, src []byte) bool {
	lines := n.Lines()
	if lines.Len() == 0 {
		return false
	}
	rest := src[lines.At(lines.Len()-1).Stop:]
	line, _, _ := bytes.Cut(rest, []byte("\n"))
	// Strip list indentation and blockquote markers before the fence.
	fence := strings.TrimSpace(strings.TrimLeft(string(line), " \t>"))
	return len(fence) >= 3 && (strings.Trim(fence, "`") == "" || strings.Trim(fence, "~") == "")
}

// extractInline renders inline content of a node to a string with ANSI styling.
func extractInline(node ast.Node, src []byte, bold, italic bool) string {
	var buf strings.Builder
	extractInlineAppend(node, src, bold, italic, &buf)
	return buf.String()
}

// extractInlineAppend recursively walks inline nodes and writes styled text to buf.
func extractInlineAppend(node ast.Node, src []byte, bold, italic bool, buf *strings.Builder) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			seg := n.Segment
			raw := string(seg.Value(src))
			if n.SoftLineBreak() {
				raw += " "
			} else if n.HardLineBreak() {
				raw += "\n"
			}
			applyInlineStyle(raw, bold, italic, buf)

		case *ast.CodeSpan:
			// Extract raw bytes from code span children
			var code strings.Builder
			for cc := n.FirstChild(); cc != nil; cc = cc.NextSibling() {
				if t, ok := cc.(*ast.Text); ok {
					code.Write(t.Segment.Value(src))
				}
			}
			buf.WriteString(ansiReverse + code.String() + ansiReset)

		case *ast.Emphasis:
			if n.Level == 2 {
				// bold
				buf.WriteString(ansiBold)
				extractInlineAppend(n, src, true, italic, buf)
				buf.WriteString(ansiReset)
			} else {
				// italic
				buf.WriteString(ansiItalic)
				extractInlineAppend(n, src, bold, true, buf)
				buf.WriteString(ansiReset)
			}

		case *ast.Link:
			url := string(n.Destination)
			buf.WriteString("\x1b]8;;" + url + "\x1b\\")
			buf.WriteString(ansiCyan)
			extractInlineAppend(n, src, bold, italic, buf)
			buf.WriteString(ansiReset)
			buf.WriteString("\x1b]8;;\x1b\\")

		case *ast.AutoLink:
			url := string(n.URL(src))
			buf.WriteString("\x1b]8;;" + url + "\x1b\\")
			buf.WriteString(ansiCyan)
			applyInlineStyle(url, bold, italic, buf)
			buf.WriteString(ansiReset)
			buf.WriteString("\x1b]8;;\x1b\\")

		case *ast.RawHTML:
			// Shown as plain text, e.g. Vec<String>.
			for i := 0; i < n.Segments.Len(); i++ {
				seg := n.Segments.At(i)
				raw := strings.ReplaceAll(string(seg.Value(src)), "\n", " ")
				applyInlineStyle(raw, bold, italic, buf)
			}

		case *ast.String:
			applyInlineStyle(string(n.Value), bold, italic, buf)

		default:
			// Recurse for unknown inline nodes
			extractInlineAppend(child, src, bold, italic, buf)
		}
	}
}

// applyInlineStyle writes text to buf with active bold/italic ANSI codes.
func applyInlineStyle(text string, bold, italic bool, buf *strings.Builder) {
	if bold {
		buf.WriteString(ansiBold)
	}
	if italic {
		buf.WriteString(ansiItalic)
	}
	buf.WriteString(text)
	if bold || italic {
		buf.WriteString(ansiReset)
	}
}

// highlightCacheMax bounds the number of cached highlighted code blocks.
// When full, the oldest entry is evicted.
const highlightCacheMax = 256

var (
	highlightStyle     = chromastyles.Get("monokai")
	highlightFormatter = formatters.Get("terminal256")

	highlightMu    sync.Mutex
	highlightCache = make(map[string]string)
	highlightKeys  []string // insertion order, oldest first
)

func init() {
	if highlightStyle == nil {
		highlightStyle = chromastyles.Fallback
	}
	if highlightFormatter == nil {
		highlightFormatter = formatters.Fallback
	}
}

// highlightCode applies chroma syntax highlighting to code, returning ANSI output.
// Falls back to plain code if highlighting fails. Results are cached by
// (lang, code) so unchanged blocks are not re-highlighted on every render.
func highlightCode(code, lang string) string {
	key := lang + "\x00" + code
	highlightMu.Lock()
	cached, ok := highlightCache[key]
	highlightMu.Unlock()
	if ok {
		return cached
	}

	out := highlightUncached(code, lang)

	highlightMu.Lock()
	if _, ok := highlightCache[key]; !ok {
		if len(highlightKeys) >= highlightCacheMax {
			delete(highlightCache, highlightKeys[0])
			highlightKeys = highlightKeys[1:]
		}
		highlightKeys = append(highlightKeys, key)
	}
	highlightCache[key] = out
	highlightMu.Unlock()
	return out
}

// resetHighlightCache empties the highlight cache.
func resetHighlightCache() {
	highlightMu.Lock()
	clear(highlightCache)
	highlightKeys = nil
	highlightMu.Unlock()
}

func highlightUncached(code, lang string) string {
	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var buf bytes.Buffer
	if err := highlightFormatter.Format(&buf, highlightStyle, iterator); err != nil {
		return code
	}
	return buf.String()
}
