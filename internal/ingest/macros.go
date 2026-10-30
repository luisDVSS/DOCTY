package ingest

import (
	"strconv"
	"strings"
)

// ExpandMacros rewrites MDN macros into readable Markdown and drops the ones
// that only make sense on the website (interactive examples, compatibility
// tables, sidebars...).
func ExpandMacros(s string) string {
	out, _ := ExpandMacrosReport(s)
	return out
}

// ExpandMacrosReport is like ExpandMacros but also returns the names of
// macros that were dropped because they are unknown, in first-seen order.
func ExpandMacrosReport(s string) (string, []string) {
	var (
		b       strings.Builder
		unknown []string
		seen    = map[string]bool{}
	)

	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])

		j := strings.Index(s[i:], "}}")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}

		text, name := expandMacro(s[i+2 : i+j])
		b.WriteString(text)
		if name != "" && !seen[name] {
			seen[name] = true
			unknown = append(unknown, name)
		}
		s = s[i+j+2:]
	}

	out := strings.ReplaceAll(b.String(), "\r\n", "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out), unknown
}

func expandMacro(macro string) (text, unknown string) {
	name, args := macro, ""
	if i := strings.Index(macro, "("); i >= 0 {
		name = strings.TrimSpace(macro[:i])
		args = macro[i+1:]
		args = strings.TrimSuffix(strings.TrimSpace(args), ")")
	}

	// MDN macro names are not consistent about capitalization (DOMxRef vs
	// domxref), so match them case-insensitively.
	switch strings.ToLower(name) {
	case "interactiveexample", "embedinteractiveexample", "embedlivesample",
		"embedghlivesample", "specifications", "compat", "seecompattable",
		"defaultapissidebar", "mdnsidebar", "jsref", "availableinworkers",
		"deprecated_inline", "non-standard_inline", "experimental_inline",
		"optional_inline", "readonly_inline", "writeonly_inline":
		return "", ""
	case "jsxref", "domxref", "cssxref", "glossary", "httpheader":
		return displayName(splitArgs(args)), ""
	default:
		// Unknown macros are dropped: showing raw {{...}} in the terminal is
		// worse than losing a decoration. The caller can report the name.
		return "", name
	}
}

// displayName picks the human-readable argument of an MDN xref-like macro.
func displayName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) >= 2 && args[1] != "" {
		return unquote(args[1])
	}
	ref := unquote(args[0])
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}

func splitArgs(s string) []string {
	var (
		out     []string
		cur     strings.Builder
		inQuote bool
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if v := strings.TrimSpace(cur.String()); v != "" {
		out = append(out, v)
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if v, err := strconv.Unquote(s); err == nil {
			return v
		}
		return s[1 : len(s)-1]
	}
	return s
}
