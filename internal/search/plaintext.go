package search

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	mdLinkRe      = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]*)\)`)

	mdBoldRe             = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdItalicStarRe       = regexp.MustCompile(`(^|[^\w*])\*([^*\n]+)\*([^\w*]|$)`)
	mdItalicUnderscoreRe = regexp.MustCompile(`(^|[^\w])_([^_\n]+)_([^\w]|$)`)

	mdLinkNormalizeRe = regexp.MustCompile(`(!?\[[^\]]*\]\()([^)]+)(\))`)
)

const mdnDocsBase = "https://developer.mozilla.org"

// PlainSnippet turns a Markdown section into a compact single-line snippet for
// the terminal. Links become their text, fenced code keeps its content but
// loses the fence, and emphasis/inline-code markers are removed.
func PlainSnippet(s string) string {
	s = htmlCommentRe.ReplaceAllString(s, "")
	s = transformFences(s, true)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// CleanForLLM prepares a section for the model: fenced code is preserved and
// MDN-relative links are rewritten to absolute URLs so citations stay useful.
func CleanForLLM(s string) string {
	s = htmlCommentRe.ReplaceAllString(s, "")
	s = transformFences(s, false)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// transformFences walks the document line by line so Markdown inside fenced
// code is never rewritten. In snippet mode the fence lines disappear; in LLM
// mode they are kept.
func transformFences(s string, snippet bool) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))

	var (
		inFence bool
		fence   string
	)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inFence && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			inFence = true
			fence = trimmed[:3]
			if !snippet {
				out = append(out, line)
			}
			continue
		}
		if inFence {
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
				if !snippet {
					out = append(out, line)
				}
				continue
			}
			out = append(out, line)
			continue
		}

		if snippet {
			out = append(out, stripInlineMarkdown(line))
		} else {
			out = append(out, normalizeMDNLinks(line))
		}
	}
	return strings.Join(out, "\n")
}

func stripInlineMarkdown(s string) string {
	s, codes := protectInlineCode(s)

	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdBoldRe.ReplaceAllString(s, "$1")
	s = mdItalicStarRe.ReplaceAllString(s, "$1$2$3")
	s = mdItalicUnderscoreRe.ReplaceAllString(s, "$1$2$3")

	for i, code := range codes {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00%d\x00", i), code)
	}
	return s
}

// protectInlineCode replaces `code spans` with placeholders so emphasis and
// link rewriting cannot corrupt identifiers or expressions inside them.
func protectInlineCode(s string) (string, []string) {
	var (
		b     strings.Builder
		codes []string
	)
	for i := 0; i < len(s); {
		if s[i] == '`' {
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				codes = append(codes, s[i+1:i+1+j])
				fmt.Fprintf(&b, "\x00%d\x00", len(codes)-1)
				i += j + 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), codes
}

func normalizeMDNLinks(s string) string {
	return mdLinkNormalizeRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdLinkNormalizeRe.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		raw := strings.TrimSpace(sub[2])
		url, suffix := raw, ""
		if i := strings.IndexAny(raw, " \t"); i >= 0 {
			url, suffix = raw[:i], raw[i:]
		}
		switch {
		case strings.HasPrefix(url, "//"):
			url = "https:" + url
		case strings.HasPrefix(url, "/"):
			url = mdnDocsBase + url
		}
		return sub[1] + url + suffix + sub[3]
	})
}
