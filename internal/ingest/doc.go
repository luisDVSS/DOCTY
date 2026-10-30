package ingest

import (
	"fmt"
	"strings"
)

// Document is one normalized MDN page.
type Document struct {
	Symbol        string    `json:"symbol"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	ShortTitle    string    `json:"short_title,omitempty"`
	Slug          string    `json:"slug"`
	PageType      string    `json:"page_type"`
	URL           string    `json:"url"`
	Summary       string    `json:"summary"`
	Sections      []Section `json:"sections"`
	UnknownMacros []string  `json:"unknown_macros,omitempty"`
	SourcePath    string    `json:"-"`
}

// Section is a chunk of a document, split at Markdown headings.
type Section struct {
	Ord         int    `json:"ord"`
	Heading     string `json:"heading"`
	HeadingPath string `json:"heading_path"`
	Body        string `json:"body"`
	Tokens      int    `json:"tokens"`
}

const mdnBaseURL = "https://developer.mozilla.org/en-US/docs/"

// Parse turns a raw MDN Markdown file into a Document.
func Parse(src []byte) (*Document, error) {
	fm, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	body, unknownMacros := ExpandMacrosReport(body)

	doc := &Document{
		Symbol:        DeriveSymbol(fm.Slug, fm.PageType, fm.Title),
		Kind:          KindOf(fm.PageType),
		Title:         fm.Title,
		ShortTitle:    fm.ShortTitle,
		Slug:          fm.Slug,
		PageType:      fm.PageType,
		URL:           mdnBaseURL + fm.Slug,
		Sections:      SplitSections(DeriveSymbol(fm.Slug, fm.PageType, fm.Title), body),
		UnknownMacros: unknownMacros,
	}
	if len(doc.Sections) > 0 {
		doc.Summary = doc.Sections[0].Body
	}
	return doc, nil
}

// KindOf maps an MDN page-type to the kind Docty exposes to users.
func KindOf(pageType string) string {
	switch pageType {
	case "javascript-instance-method":
		return "method"
	case "javascript-static-method":
		return "static-method"
	case "javascript-instance-data-property", "javascript-instance-accessor-property":
		return "property"
	case "javascript-static-data-property", "javascript-static-accessor-property":
		return "static-property"
	case "javascript-class":
		return "class"
	case "javascript-function":
		return "function"
	case "javascript-object":
		return "object"
	case "javascript-constructor":
		return "constructor"
	case "javascript-operator":
		return "operator"
	case "javascript-statement":
		return "statement"
	case "javascript-error":
		return "error"
	case "javascript-language-feature":
		return "feature"
	case "javascript-namespace":
		return "namespace"
	case "javascript-global-property":
		return "global-property"
	case "javascript-guide", "guide":
		return "guide"
	default:
		if pageType == "" {
			return "page"
		}
		return pageType
	}
}

// DeriveSymbol computes the canonical symbol of a page from its slug and
// page-type. Examples:
//
//	.../Global_Objects/Array/map + javascript-instance-method -> Array.prototype.map
//	.../Global_Objects/Object/assign + javascript-static-method -> Object.assign
//	.../Global_Objects/Array/Array + javascript-constructor -> Array
//	.../Operators/Spread_syntax + javascript-operator -> Spread syntax
func DeriveSymbol(slug, pageType, title string) string {
	parts := strings.Split(strings.Trim(slug, "/"), "/")
	start := 0
	for i, p := range parts {
		if strings.EqualFold(p, "Reference") {
			start = i + 1
			break
		}
	}
	segs := parts[start:]
	if len(segs) == 0 {
		return strings.TrimSpace(title)
	}

	last := segs[len(segs)-1]
	parent := ""
	if len(segs) >= 2 {
		parent = segs[len(segs)-2]
	}
	member := last
	if strings.HasPrefix(last, "Symbol.") {
		member = "[" + last + "]"
	}

	switch pageType {
	case "javascript-instance-method",
		"javascript-instance-data-property",
		"javascript-instance-accessor-property":
		return joinMember(parent+".prototype", member)
	case "javascript-static-method",
		"javascript-static-data-property",
		"javascript-static-accessor-property":
		return joinMember(parent, member)
	case "javascript-constructor":
		return parent + "()"
	case "javascript-object", "javascript-class", "javascript-namespace", "javascript-global-property":
		return last
	case "javascript-function":
		return last
	case "javascript-error", "javascript-language-feature", "javascript-guide", "guide":
		return strings.TrimSpace(title)
	case "javascript-operator", "javascript-statement":
		return strings.ReplaceAll(last, "_", " ")
	default:
		return last
	}
}

func normalizeSymbol(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func joinMember(parent, member string) string {
	if strings.HasPrefix(member, "[") {
		return parent + member
	}
	return parent + "." + member
}

// Find returns the document whose symbol, title or short title matches,
// case-insensitively.
func Find(docs []*Document, query string) (*Document, bool) {
	q := normalizeSymbol(query)
	match := func(s string) bool {
		s = normalizeSymbol(s)
		return s == q || strings.TrimSuffix(s, "()") == q
	}
	for _, d := range docs {
		if match(d.Symbol) || match(d.Title) {
			return d, true
		}
	}
	for _, d := range docs {
		if match(d.ShortTitle) {
			return d, true
		}
	}
	return nil, false
}

// String renders a short one-line description, useful for list output.
func (d *Document) String() string {
	if d.Summary == "" {
		return fmt.Sprintf("%s (%s)", d.Symbol, d.Kind)
	}
	return fmt.Sprintf("%s (%s)", d.Symbol, d.Kind)
}
