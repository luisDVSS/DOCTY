package index

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/search"
)

// Doc is a lightweight document row, used for candidate lists.
type Doc struct {
	ID        int64  `json:"id"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	Summary   string `json:"summary"`
	Signature string `json:"signature,omitempty"`
}

// Match is one FTS hit: a section plus the document it belongs to.
type Match struct {
	Doc         Doc
	Heading     string
	HeadingPath string
	Snippet     string
	Body        string
	Rank        float64
}

// SourceInfo is the metadata of an installed source.
type SourceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Version   string `json:"version"`
	FetchedAt string `json:"fetched_at"`
	PageCount int    `json:"page_count"`
}

// Source returns metadata for an installed source.
func (ix *Index) Source(ctx context.Context, lang string) (SourceInfo, bool, error) {
	var s SourceInfo
	err := ix.db.QueryRowContext(ctx,
		`SELECT id, name, provider, version, fetched_at, page_count FROM sources WHERE id = ?`, lang).
		Scan(&s.ID, &s.Name, &s.Provider, &s.Version, &s.FetchedAt, &s.PageCount)
	if err == sql.ErrNoRows {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	return s, true, nil
}

// Lookup returns the full document for an exact symbol match.
func (ix *Index) Lookup(ctx context.Context, lang, symbol string) (*ingest.Document, error) {
	var (
		doc       ingest.Document
		id        int64
		signature string
	)
	err := ix.db.QueryRowContext(ctx,
		`SELECT id, symbol, kind, title, slug, url, summary_md, COALESCE(signature, '')
		 FROM documents WHERE source_id = ? AND symbol_norm = ?`,
		lang, strings.ToLower(strings.TrimSpace(symbol))).
		Scan(&id, &doc.Symbol, &doc.Kind, &doc.Title, &doc.Slug, &doc.URL, &doc.Summary, &signature)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := ix.db.QueryContext(ctx,
		`SELECT ord, heading, heading_path, body_md, token_count
		 FROM sections WHERE document_id = ? ORDER BY ord`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s ingest.Section
		if err := rows.Scan(&s.Ord, &s.Heading, &s.HeadingPath, &s.Body, &s.Tokens); err != nil {
			return nil, err
		}
		doc.Sections = append(doc.Sections, s)
	}
	return &doc, rows.Err()
}

// Candidates returns documents whose symbol exactly matches or ends with the
// query (e.g. "map" -> "Array.prototype.map").
func (ix *Index) Candidates(ctx context.Context, lang, query string, limit int) ([]Doc, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	base := strings.TrimSuffix(q, "()")
	variants := []string{q, base}
	if base != q {
		variants = append(variants, base+"()")
	} else {
		variants = append(variants, q+"()")
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		// Array.map -> Array.prototype.map; harmless when it does not exist.
		variants = append(variants, base[:i]+".prototype"+base[i:])
	}

	seen := map[int64]bool{}
	seenVariant := map[string]bool{}
	var docs []Doc
	for _, v := range variants {
		if seenVariant[v] {
			continue
		}
		seenVariant[v] = true
		rows, err := ix.db.QueryContext(ctx,
			`SELECT id, symbol, kind, title, slug, url, summary_md, COALESCE(signature, '')
			 FROM documents
			 WHERE source_id = ? AND (symbol_norm = ? OR symbol_norm LIKE '%.' || ?)
			 ORDER BY length(symbol), symbol
			 LIMIT ?`,
			lang, v, v, limit)
		if err != nil {
			return nil, err
		}
		found, err := scanDocs(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, d := range found {
			if !seen[d.ID] {
				seen[d.ID] = true
				docs = append(docs, d)
			}
		}
	}

	sort.SliceStable(docs, func(i, j int) bool {
		if len(docs[i].Symbol) != len(docs[j].Symbol) {
			return len(docs[i].Symbol) < len(docs[j].Symbol)
		}
		return docs[i].Symbol < docs[j].Symbol
	})
	if len(docs) > limit {
		docs = docs[:limit]
	}
	return docs, nil
}

func scanDocs(rows *sql.Rows) ([]Doc, error) {
	var out []Doc
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.ID, &d.Symbol, &d.Kind, &d.Title, &d.Slug, &d.URL, &d.Summary, &d.Signature); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Search runs an FTS5 query and returns section-level matches. Spanish queries
// are expanded through the ES->EN glossary; if the strict expansion finds
// nothing, a relaxed OR variant is tried before giving up.
func (ix *Index) Search(ctx context.Context, lang, query string, limit int) ([]Match, error) {
	match, expanded := search.ExpandQuery(query)
	if !expanded {
		match = BuildMatchQuery(query)
	}
	if match == "" {
		return nil, nil
	}

	matches, err := ix.searchMatch(ctx, lang, match, limit)
	if err != nil {
		return nil, err
	}

	// Free-form questions in Spanish often contain one restrictive word that
	// hides the right page ("copiar un objeto sin mutarlo"). For those, merge
	// the strict AND results with the relaxed OR results to improve recall.
	if expanded && len(strings.Fields(query)) > 1 {
		if loose, _ := search.ExpandQueryLoose(query); loose != "" && loose != match {
			relaxed, err := ix.searchMatch(ctx, lang, loose, limit)
			if err != nil {
				return nil, err
			}
			matches = mergeMatches(matches, relaxed, limit)
		}
		return matches, nil
	}

	if len(matches) == 0 {
		loose := BuildMatchQueryLoose(query)
		if loose != "" && loose != match {
			return ix.searchMatch(ctx, lang, loose, limit)
		}
	}
	return matches, nil
}

func mergeMatches(a, b []Match, limit int) []Match {
	docOrder := make([]int64, 0, 8)
	seenDoc := map[int64]bool{}

	addDocs := func(list []Match, max int) {
		n := 0
		for _, m := range list {
			if seenDoc[m.Doc.ID] {
				continue
			}
			seenDoc[m.Doc.ID] = true
			docOrder = append(docOrder, m.Doc.ID)
			n++
			if n == max {
				return
			}
		}
	}
	// A few strict hits first, then the relaxed ones: enough precision to be
	// useful, enough diversity to include the page the question is about.
	addDocs(a, 3)
	addDocs(b, 5)

	byDoc := map[int64][]Match{}
	seenSection := map[string]bool{}
	for _, list := range [][]Match{a, b} {
		for _, m := range list {
			if len(byDoc[m.Doc.ID]) >= 2 {
				continue
			}
			key := fmt.Sprintf("%d|%s", m.Doc.ID, m.Heading)
			if seenSection[key] {
				continue
			}
			seenSection[key] = true
			byDoc[m.Doc.ID] = append(byDoc[m.Doc.ID], m)
		}
	}

	out := make([]Match, 0, limit)
	for _, id := range docOrder {
		out = append(out, byDoc[id]...)
		if len(out) >= limit {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (ix *Index) searchMatch(ctx context.Context, lang, match string, limit int) ([]Match, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT d.id, d.symbol, d.kind, d.title, d.slug, d.url, d.summary_md,
		        COALESCE(d.signature, ''), s.heading, s.heading_path, s.body_md,
		        bm25(sections_fts, 6.0, 3.0, 1.0, 10.0) AS rank
		 FROM sections_fts
		 JOIN sections s ON s.id = sections_fts.rowid
		 JOIN documents d ON d.id = s.document_id
		 WHERE sections_fts MATCH ? AND d.source_id = ?
		 ORDER BY rank
		 LIMIT ?`,
		match, lang, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		var body string
		if err := rows.Scan(
			&m.Doc.ID, &m.Doc.Symbol, &m.Doc.Kind, &m.Doc.Title, &m.Doc.Slug,
			&m.Doc.URL, &m.Doc.Summary, &m.Doc.Signature,
			&m.Heading, &m.HeadingPath, &body, &m.Rank,
		); err != nil {
			return nil, err
		}
		m.Snippet = snippet(body, 220)
		m.Body = body
		out = append(out, m)
	}
	return out, rows.Err()
}

// List returns documents of a source ordered by symbol.
func (ix *Index) List(ctx context.Context, lang string, limit int) ([]Doc, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id, symbol, kind, title, slug, url, summary_md, COALESCE(signature, '')
		 FROM documents WHERE source_id = ?
		 ORDER BY symbol LIMIT ?`, lang, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocs(rows)
}

// Symbols returns every symbol of a source, for suggestions.
func (ix *Index) Symbols(ctx context.Context, lang string) ([]string, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT symbol FROM documents WHERE source_id = ? ORDER BY symbol`, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Counts returns the number of documents and sections in a source.
func (ix *Index) Counts(ctx context.Context, lang string) (docs, sections int, err error) {
	err = ix.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE source_id = ?`, lang).Scan(&docs)
	if err != nil {
		return
	}
	err = ix.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sections s JOIN documents d ON d.id = s.document_id
		 WHERE d.source_id = ?`, lang).Scan(&sections)
	return
}

// BuildMatchQueryLoose is the OR variant used as a fallback when the strict
// AND query finds nothing (e.g. "diferencias entre let y const").
func BuildMatchQueryLoose(query string) string {
	fields := matchFields(query)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " OR ")
}

func matchFields(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$'
	})
}

// BuildMatchQuery turns free text into a safe FTS5 MATCH expression. Every
// token is quoted; the last one becomes a prefix query so "struct" finds
// "structuredClone".
func BuildMatchQuery(query string) string {
	fields := matchFields(query)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for i, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		if i == len(fields)-1 {
			parts = append(parts, `"`+f+`"*`)
		} else {
			parts = append(parts, `"`+f+`"`)
		}
	}
	return strings.Join(parts, " ")
}

func snippet(body string, maxRunes int) string {
	body = search.PlainSnippet(body)
	r := []rune(body)
	if len(r) <= maxRunes {
		return body
	}
	return strings.TrimSpace(string(r[:maxRunes])) + "…"
}
