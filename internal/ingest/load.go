package ingest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SplitSections cuts a document body at level-2 through level-4 headings,
// keeping fenced code blocks intact. The preamble becomes the "Summary"
// section. Deeper levels are folded into their parent's body.
func SplitSections(symbol, body string) []Section {
	var (
		sections []Section
		cur      = Section{Heading: "Summary", HeadingPath: symbol}
		inFence  bool
		fence    string
		parents  = map[int]string{} // level -> heading, for breadcrumbs
	)

	flush := func() {
		cur.Body = strings.TrimSpace(cur.Body)
		if cur.Body == "" {
			return
		}
		cur.Ord = len(sections)
		cur.Tokens = len(strings.Fields(cur.Body))
		sections = append(sections, cur)
	}

	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)

		if inFence {
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
				fence = ""
			}
			cur.Body += line + "\n"
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = true
			fence = trimmed[:3]
			cur.Body += line + "\n"
			continue
		}

		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "#### ") {
			flush()
			level := len(line) - len(strings.TrimLeft(line, "#"))
			heading := strings.TrimSpace(strings.TrimLeft(line, "#"))

			path := symbol
			for l := 2; l < level; l++ {
				if parent := parents[l]; parent != "" {
					path += " > " + parent
				}
			}
			path += " > " + heading

			for l := level; l <= 4; l++ {
				delete(parents, l)
			}
			parents[level] = heading

			cur = Section{
				Heading:     heading,
				HeadingPath: path,
			}
			continue
		}
		cur.Body += line + "\n"
	}
	flush()

	return sections
}

// LoadDir walks a directory (an mdn/content checkout, or a folder of pages)
// and parses every JavaScript page it finds.
func LoadDir(root string) ([]*Document, error) {
	var docs []*Document
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc, err := Parse(src)
		if err != nil {
			// Files without MDN frontmatter (CONTRIBUTING.md, etc.) are not
			// an error: just skip them.
			return nil
		}
		if !strings.Contains(strings.ToLower(doc.Slug), "/javascript/") {
			return nil
		}
		doc.SourcePath = path
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", root, err)
	}

	sort.Slice(docs, func(i, j int) bool {
		return strings.ToLower(docs[i].Symbol) < strings.ToLower(docs[j].Symbol)
	})
	return docs, nil
}

// LoadFile parses a single Markdown file.
func LoadFile(path string) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	doc.SourcePath = path
	return doc, nil
}
