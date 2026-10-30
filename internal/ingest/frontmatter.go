package ingest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the subset of MDN metadata Docty needs.
type Frontmatter struct {
	Title         string `yaml:"title"`
	ShortTitle    string `yaml:"short-title"`
	Slug          string `yaml:"slug"`
	PageType      string `yaml:"page-type"`
	BrowserCompat string `yaml:"browser-compat"`
	Sidebar       string `yaml:"sidebar"`
}

// splitFrontmatter separates the YAML header from the Markdown body.
func splitFrontmatter(src []byte) (Frontmatter, string, error) {
	var fm Frontmatter

	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return fm, "", fmt.Errorf("falta el frontmatter")
	}
	rest := text[len("---\n"):]

	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, "", fmt.Errorf("frontmatter sin cerrar")
	}
	header := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return fm, "", fmt.Errorf("frontmatter inválido: %w", err)
	}
	if fm.Slug == "" {
		return fm, "", fmt.Errorf("frontmatter sin slug")
	}
	return fm, body, nil
}
