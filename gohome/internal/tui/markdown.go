package tui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	ansiBold      = "\x1b[1m"
	ansiItalic    = "\x1b[3m"
	ansiUnderline = "\x1b[4m"
	ansiDim       = "\x1b[2m"
	ansiReverse   = "\x1b[7m"
	ansiReset     = "\x1b[0m"
)

// RenderMarkdown parses markdown source and returns terminal-ready lines
// styled with ANSI escape sequences, wrapped to fit within width columns.
func RenderMarkdown(source string, width int) []string {
	if strings.TrimSpace(source) == "" {
		return nil
	}

	src := []byte(source)
	reader := text.NewReader(src)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)

	var lines []string
	renderNode(doc, src, width, 0, false, false, &lines)
	return lines
}

// renderNode walks an AST node and appends rendered lines to out.
// listDepth tracks nesting level for lists; ordered/unordered are handled per item.
func renderNode(node ast.Node, src []byte, width, listDepth int, bold, italic bool, out *[]string) {
	switch n := node.(type) {

	case *ast.Document:
		renderChildren(n, src, width, listDepth, bold, italic, out)

	case *ast.Heading:
		text := extractInline(n, src, bold, italic)
		switch n.Level {
		case 1:
			*out = append(*out, ansiBold+ansiUnderline+text+ansiReset)
		case 2:
			*out = append(*out, ansiBold+text+ansiReset)
		default:
			prefix := strings.Repeat("#", n.Level) + " "
			*out = append(*out, ansiBold+prefix+text+ansiReset)
		}
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
		highlighted := highlightCode(code, lang)
		*out = append(*out, strings.Split(strings.TrimRight(highlighted, "\n"), "\n")...)
		*out = append(*out, "")

	case *ast.CodeBlock:
		var codeLines []string
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			codeLines = append(codeLines, string(line.Value(src)))
		}
		code := strings.Join(codeLines, "")
		highlighted := highlightCode(code, "")
		*out = append(*out, strings.Split(strings.TrimRight(highlighted, "\n"), "\n")...)
		*out = append(*out, "")

	case *ast.Blockquote:
		var inner []string
		renderChildren(n, src, width-2, listDepth, bold, italic, &inner)
		for _, l := range inner {
			*out = append(*out, ansiDim+"| "+ansiReset+l)
		}

	case *ast.List:
		renderList(n, src, width, listDepth, bold, italic, out)

	case *ast.ListItem:
		// ListItem rendering is handled inside renderList
		renderChildren(n, src, width, listDepth, bold, italic, out)

	case *ast.ThematicBreak:
		*out = append(*out, ansiDim+strings.Repeat("─", width)+ansiReset)
		*out = append(*out, "")

	default:
		// For unrecognized block nodes, recurse into children
		renderChildren(node, src, width, listDepth, bold, italic, out)
	}
}

// renderChildren iterates over child nodes and renders each.
func renderChildren(node ast.Node, src []byte, width, listDepth int, bold, italic bool, out *[]string) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		renderNode(child, src, width, listDepth, bold, italic, out)
	}
}

// renderList handles ordered and unordered lists with correct bullet/number prefixes.
func renderList(list *ast.List, src []byte, width, listDepth int, bold, italic bool, out *[]string) {
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
		indent := strings.Repeat("  ", listDepth)
		fullPrefix := indent + prefix
		innerWidth := width - len(fullPrefix)
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
				renderList(sn, src, width, listDepth+1, bold, italic, &nested)
				itemLines = append(itemLines, nested...)
			default:
				var sub2Lines []string
				renderNode(sub, src, innerWidth, listDepth+1, bold, italic, &sub2Lines)
				itemLines = append(itemLines, sub2Lines...)
			}
		}

		// Emit: first line gets the prefix, continuation lines get indentation
		contIndent := indent + strings.Repeat(" ", len(prefix))
		for i, l := range itemLines {
			if i == 0 {
				*out = append(*out, fullPrefix+l)
			} else {
				*out = append(*out, contIndent+l)
			}
		}
	}
	// blank line after list
	*out = append(*out, "")
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
			// Render link text, ignore URL for terminal output
			extractInlineAppend(n, src, bold, italic, buf)

		case *ast.AutoLink:
			url := string(n.URL(src))
			applyInlineStyle(url, bold, italic, buf)

		case *ast.RawHTML:
			// Skip raw HTML

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

// highlightCode applies chroma syntax highlighting to code, returning ANSI output.
// Falls back to plain code if highlighting fails.
func highlightCode(code, lang string) string {
	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := chromastyles.Get("monokai")
	if style == nil {
		style = chromastyles.Fallback
	}

	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return code
	}
	return buf.String()
}
