// Package mdrender turns the Markdown stored in the index into readable
// terminal output. It deliberately implements a small, predictable subset of
// Markdown: headings, emphasis, inline code, fenced code, lists, links,
// blockquotes and horizontal rules. Unknown constructs are passed through.
package mdrender

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiBoldOff   = "\x1b[22m"
	ansiItalic    = "\x1b[3m"
	ansiItalicOff = "\x1b[23m"
	ansiCyan      = "\x1b[36m"
	ansiYellow    = "\x1b[33m"
	ansiBlue      = "\x1b[34m"
	ansiGray      = "\x1b[90m"
	ansiFgOff     = "\x1b[39m"
)

// Options controls terminal rendering.
type Options struct {
	// Color enables ANSI styles. When false the Markdown markers are still
	// removed and the structure is preserved, but no escape sequences are
	// emitted.
	Color bool
	// Width is reserved for future paragraph wrapping. Zero means no wrapping.
	Width int
	// BaseURL resolves relative links found in the Markdown. It is normally
	// the URL of the page being rendered.
	BaseURL string
}

// Render formats a complete Markdown document for a terminal.
func Render(md string, opt Options) string {
	var b strings.Builder
	w := NewWriter(&b, opt)
	_, _ = io.WriteString(w, md)
	_ = w.Close()
	return strings.TrimRight(b.String(), "\n")
}

// Writer is a streaming Markdown renderer. It buffers partial lines, so it can
// be used as the destination of an LLM stream without waiting for the whole
// answer.
type Writer struct {
	w   io.Writer
	opt Options

	buf     []byte
	inFence bool
	fence   byte
	fenceN  int

	started   bool
	lastBlank bool
}

// NewWriter returns a streaming renderer that writes to w.
func NewWriter(w io.Writer, opt Options) *Writer {
	return &Writer{w: w, opt: opt}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if err := w.writeLine(line); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Close flushes the pending line and closes an open code fence.
func (w *Writer) Close() error {
	if len(w.buf) > 0 {
		line := string(w.buf)
		w.buf = nil
		if err := w.writeLine(line); err != nil {
			return err
		}
	}
	if w.inFence {
		w.inFence = false
		_, err := fmt.Fprint(w.w, "  └─\n")
		return err
	}
	return nil
}

func (w *Writer) writeLine(line string) error {
	line = strings.TrimSuffix(line, "\r")

	if w.inFence {
		if isClosingFence(line, w.fence, w.fenceN) {
			w.inFence = false
			w.lastBlank = true
			_, err := fmt.Fprint(w.w, "  └─\n\n")
			return err
		}
		return w.raw("  │ " + w.code(line) + "\n")
	}

	if ch, n, info, ok := openingFence(line); ok {
		w.fence, w.fenceN, w.inFence = ch, n, true
		if w.started && !w.lastBlank {
			if _, err := io.WriteString(w.w, "\n"); err != nil {
				return err
			}
		}
		label := ""
		if info != "" {
			if fields := strings.Fields(info); len(fields) > 0 {
				label = " " + strings.TrimSuffix(fields[0], "-nolint")
			}
		}
		w.started, w.lastBlank = true, false
		_, err := fmt.Fprintf(w.w, "  ┌─%s\n", w.dim(label))
		return err
	}

	if strings.TrimSpace(line) == "" {
		if !w.started || w.lastBlank {
			return nil
		}
		w.lastBlank = true
		_, err := io.WriteString(w.w, "\n")
		return err
	}

	if _, text, ok := heading(line); ok {
		if w.started && !w.lastBlank {
			if _, err := io.WriteString(w.w, "\n"); err != nil {
				return err
			}
		}
		style := w.on(ansiBold) + w.on(ansiCyan)
		w.started, w.lastBlank = true, false
		if w.opt.Color {
			_, err := fmt.Fprintf(w.w, "%s%s%s\n\n", style, w.inline(text, style), ansiReset)
			w.lastBlank = true
			return err
		}
		_, err := fmt.Fprintf(w.w, "%s\n\n", w.inline(text, ""))
		w.lastBlank = true
		return err
	}

	if isHorizontalRule(line) {
		w.started, w.lastBlank = true, false
		return w.raw(w.dim(strings.Repeat("─", 40)) + "\n")
	}

	if text, ok := blockquote(line); ok {
		w.started, w.lastBlank = true, false
		style := w.on(ansiGray) + w.on(ansiItalic)
		if w.opt.Color {
			return w.raw(w.dim("│ ") + style + w.inline(text, style) + ansiReset + "\n")
		}
		return w.raw("│ " + w.inline(text, "") + "\n")
	}

	if indent, marker, text, ok := listItem(line); ok {
		w.started, w.lastBlank = true, false
		// MDN uses "- : description" for definition-style lists.
		if strings.HasPrefix(text, ":") {
			text = strings.TrimPrefix(strings.TrimPrefix(text, ":"), " ")
			return w.raw(indent + "  " + w.inline(text, "") + "\n")
		}
		bullet := marker
		if marker == "-" || marker == "*" || marker == "+" {
			bullet = "•"
		}
		if strings.HasPrefix(text, "[ ] ") {
			bullet, text = "☐", text[4:]
		} else if strings.HasPrefix(strings.ToLower(text), "[x] ") {
			bullet, text = "☑", text[4:]
		}
		return w.raw(indent + w.bullet(bullet) + " " + w.inline(text, "") + "\n")
	}

	w.started, w.lastBlank = true, false
	return w.raw(w.inline(line, "") + "\n")
}

func (w *Writer) raw(s string) error {
	_, err := io.WriteString(w.w, s)
	return err
}

func (w *Writer) on(code string) string {
	if !w.opt.Color {
		return ""
	}
	return code
}

func (w *Writer) dim(s string) string {
	s = sanitizeANSI(s)
	if !w.opt.Color {
		return s
	}
	return ansiGray + s + ansiFgOff
}

func (w *Writer) bullet(s string) string {
	if !w.opt.Color {
		return s
	}
	return ansiCyan + s + ansiFgOff
}

func (w *Writer) code(s string) string {
	s = sanitizeANSI(s)
	if !w.opt.Color {
		return s
	}
	return ansiYellow + s + ansiFgOff
}

// wrap styles text and restores the enclosing style afterwards, so nested
// inline elements do not cancel a heading's or quote's formatting.
func (w *Writer) wrap(text, on, off, outer string) string {
	text = sanitizeANSI(text)
	if !w.opt.Color {
		return text
	}
	return on + text + off + outer
}

func (w *Writer) inline(s, style string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			b.WriteByte(s[i+1])
			i += 2
		case s[i] == '`':
			n := runLength(s, i, '`')
			if end := findCodeClose(s, i+n, n); end >= 0 {
				b.WriteString(w.wrap(s[i+n:end], ansiYellow, ansiFgOff, style))
				i = end + n
				continue
			}
			b.WriteByte(s[i])
			i++
		case strings.HasPrefix(s[i:], "!["):
			if text, target, end, ok := parseLink(s, i+1); ok {
				b.WriteString(w.inline(text, style))
				b.WriteString(" ")
				b.WriteString(w.wrap("("+resolveURL(w.opt.BaseURL, target)+")", ansiGray, ansiFgOff, style))
				i = end
				continue
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '[':
			if text, target, end, ok := parseLink(s, i); ok {
				b.WriteString(w.inline(text, style))
				b.WriteString(" ")
				b.WriteString(w.wrap("("+resolveURL(w.opt.BaseURL, target)+")", ansiGray, ansiFgOff, style))
				i = end
				continue
			}
			b.WriteByte(s[i])
			i++
		case strings.HasPrefix(s[i:], "**"):
			if end := findDelimiter(s, i+2, "**"); end > i+2 {
				nested := style + w.on(ansiBold)
				b.WriteString(w.on(ansiBold))
				b.WriteString(w.inline(s[i+2:end], nested))
				b.WriteString(w.on(ansiBoldOff))
				b.WriteString(style)
				i = end + 2
				continue
			}
			b.WriteByte(s[i])
			i++
		case strings.HasPrefix(s[i:], "~~"):
			if end := findDelimiter(s, i+2, "~~"); end > i+2 {
				b.WriteString(w.wrap(s[i+2:end], ansiGray, ansiFgOff, style))
				i = end + 2
				continue
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '*' || s[i] == '_':
			if canOpenEmphasis(s, i) {
				if end := findEmphasisClose(s, i+1, s[i]); end > i+1 {
					nested := style + w.on(ansiItalic)
					b.WriteString(w.on(ansiItalic))
					b.WriteString(w.inline(s[i+1:end], nested))
					b.WriteString(w.on(ansiItalicOff))
					b.WriteString(style)
					i = end + 1
					continue
				}
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '<':
			if end := strings.IndexByte(s[i:], '>'); end > 1 {
				inner := s[i+1 : i+end]
				if strings.HasPrefix(inner, "http://") || strings.HasPrefix(inner, "https://") || strings.HasPrefix(inner, "mailto:") {
					b.WriteString(w.wrap(inner, ansiBlue, ansiFgOff, style))
					i += end + 1
					continue
				}
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '&':
			if end := strings.IndexByte(s[i:], ';'); end > 0 && end <= 10 {
				entity := s[i : i+end+1]
				if decoded := html.UnescapeString(entity); decoded != entity {
					b.WriteString(decoded)
					i += end + 1
					continue
				}
			}
			b.WriteByte(s[i])
			i++
		default:
			if s[i] == 0x1b {
				b.WriteString("^[")
			} else {
				b.WriteByte(s[i])
			}
			i++
		}
	}
	return b.String()
}

func runLength(s string, i int, ch byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == ch {
		n++
	}
	return n
}

func findDelimiter(s string, start int, delim string) int {
	for i := start; i+len(delim) <= len(s); {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '`' {
			n := runLength(s, i, '`')
			if end := findCodeClose(s, i+n, n); end >= 0 {
				i = end + n
				continue
			}
			i += n
			continue
		}
		if strings.HasPrefix(s[i:], delim) {
			return i
		}
		i++
	}
	return -1
}

// findCodeClose returns the start of the next run of exactly n backticks.
func findCodeClose(s string, start, n int) int {
	for i := start; i < len(s); {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '`' {
			m := runLength(s, i, '`')
			if m == n {
				return i
			}
			i += m
			continue
		}
		i++
	}
	return -1
}

func canOpenEmphasis(s string, i int) bool {
	ch := s[i]
	if i+1 >= len(s) || isSpace(s[i+1]) {
		return false
	}
	if i+1 < len(s) && s[i+1] == ch {
		return false
	}
	if ch == '_' && i > 0 && isWord(s[i-1]) {
		return false
	}
	return true
}

func findEmphasisClose(s string, start int, ch byte) int {
	for i := start; i < len(s); {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '`' {
			n := runLength(s, i, '`')
			if end := findCodeClose(s, i+n, n); end >= 0 {
				i = end + n
				continue
			}
			i += n
			continue
		}
		if s[i] == ch {
			if i+1 < len(s) && s[i+1] == ch {
				i += 2
				continue
			}
			if i > start && !isSpace(s[i-1]) && (i+1 == len(s) || !isWord(s[i+1])) {
				return i
			}
		}
		i++
	}
	return -1
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isWord(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func parseLink(s string, start int) (text, url string, end int, ok bool) {
	if start >= len(s) || s[start] != '[' {
		return "", "", 0, false
	}
	depth := 0
	i := start
	for ; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				goto textDone
			}
		}
	}
	return "", "", 0, false

textDone:
	text = s[start+1 : i]
	if i+1 >= len(s) || s[i+1] != '(' {
		return "", "", 0, false
	}
	j := i + 2
	depth = 1
	for ; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				goto urlDone
			}
		}
	}
	return "", "", 0, false

urlDone:
	url = strings.TrimSpace(s[i+2 : j])
	if k := strings.IndexAny(url, " \t"); k >= 0 {
		url = url[:k]
	}
	return text, url, j + 1, true
}

func sanitizeANSI(s string) string {
	return strings.ReplaceAll(s, "\x1b", "^[")
}

func resolveURL(base, ref string) string {
	if ref == "" || base == "" {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil || u.IsAbs() {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	return b.ResolveReference(u).String()
}

var listRE = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)

func listItem(line string) (indent, marker, text string, ok bool) {
	m := listRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

func heading(line string) (level int, text string, ok bool) {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return 0, "", false
	}
	if i < len(line) && line[i] != ' ' && line[i] != '\t' {
		return 0, "", false
	}
	text = strings.TrimSpace(line[i:])
	if strings.HasSuffix(text, "#") {
		trimmed := strings.TrimRight(text, "#")
		if trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t") {
			text = strings.TrimSpace(trimmed)
		}
	}
	return i, text, true
}

func isHorizontalRule(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	ch := t[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == ' ' || t[i] == '\t' {
			continue
		}
		if t[i] != ch {
			return false
		}
	}
	return true
}

func blockquote(line string) (string, bool) {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, ">") {
		return "", false
	}
	t = strings.TrimPrefix(t, ">")
	t = strings.TrimPrefix(t, " ")
	return t, true
}

func openingFence(line string) (ch byte, n int, info string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 {
		return 0, 0, "", false
	}
	ch = t[0]
	if ch != '`' && ch != '~' {
		return 0, 0, "", false
	}
	n = runLength(t, 0, ch)
	if n < 3 {
		return 0, 0, "", false
	}
	return ch, n, strings.TrimSpace(t[n:]), true
}

func isClosingFence(line string, ch byte, n int) bool {
	t := strings.TrimSpace(line)
	if len(t) < n || t[0] != ch {
		return false
	}
	m := runLength(t, 0, ch)
	if m < n {
		return false
	}
	return strings.TrimSpace(t[m:]) == ""
}
