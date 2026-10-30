package cli

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

// runQuery is the default command: docty [lang] <consulta...>
func runQuery(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	lang := cfg.DefaultLang
	rest := args
	if _, ok := source.Get(args[0]); ok {
		lang = args[0]
		rest = args[1:]
	}
	if len(rest) == 0 {
		return listSource(cmd, lang)
	}
	return executeQuery(cmd, lang, strings.Join(rest, " "), "auto")
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [lang] <símbolo>",
		Short: "Busca un símbolo exacto",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lang, rest := splitLang(cfg.DefaultLang, args)
			if len(rest) == 0 {
				return exitWith(2, "falta el símbolo: docty get %s <símbolo>", lang)
			}
			return executeQuery(cmd, lang, strings.Join(rest, " "), "lookup")
		},
	}
}

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [lang] <términos>",
		Short: "Busca en el texto de la documentación",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lang, rest := splitLang(cfg.DefaultLang, args)
			if len(rest) == 0 {
				return exitWith(2, "faltan los términos: docty search %s <términos>", lang)
			}
			return executeQuery(cmd, lang, strings.Join(rest, " "), "search")
		},
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [lang]",
		Short: "Lista los símbolos indexados",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lang := cfg.DefaultLang
			if len(args) == 1 {
				lang = args[0]
			}
			return listSource(cmd, lang)
		},
	}
}

func splitLang(def string, args []string) (string, []string) {
	if len(args) > 0 {
		if _, ok := source.Get(args[0]); ok {
			return args[0], args[1:]
		}
	}
	return def, args
}

func listSource(cmd *cobra.Command, lang string) error {
	ctx := context.Background()
	ix, err := openIndex(lang)
	if err != nil {
		return err
	}
	defer ix.Close()

	docs, err := ix.List(ctx, lang, 2000)
	if err != nil {
		return exitWith(3, "%v", err)
	}
	if len(docs) == 0 {
		return exitWith(1, "no hay símbolos indexados para %s", lang)
	}

	if flagJSON {
		return writeJSON(cmd, listJSON{
			SchemaVersion: 1,
			Lang:          lang,
			Count:         len(docs),
			Results:       docs,
		})
	}

	out := cmd.OutOrStdout()
	if outputMode() == formatMarkdown {
		fmt.Fprintf(out, "## %s: %d símbolos\n\n", lang, len(docs))
		for _, d := range docs {
			fmt.Fprintf(out, "- **%s** — %s\n", d.Symbol, kindLabel(d.Kind))
		}
		return nil
	}
	fmt.Fprintf(out, "%s: %d símbolos\n\n", lang, len(docs))
	for _, d := range docs {
		fmt.Fprintf(out, "  %-42s %s\n", d.Symbol, kindLabel(d.Kind))
	}
	return nil
}

// executeQuery resolves a query. forced is "auto", "lookup" or "search".
func executeQuery(cmd *cobra.Command, lang, query, forced string) error {
	ctx := context.Background()
	ix, err := openIndex(lang)
	if err != nil {
		return err
	}
	defer ix.Close()

	if _, ok, err := ix.Source(ctx, lang); err != nil {
		return exitWith(3, "%v", err)
	} else if !ok {
		return exitWith(3, "el índice no contiene %s · ejecuta: docty index %s --from <ruta>", lang, lang)
	}

	start := time.Now()

	if forced != "search" {
		q := strings.TrimSpace(query)

		if !strings.Contains(q, " ") {
			cands, err := ix.Candidates(ctx, lang, q, 10)
			if err != nil {
				return exitWith(3, "%v", err)
			}
			switch {
			case len(cands) == 1:
				full, err := ix.Lookup(ctx, lang, cands[0].Symbol)
				if err != nil {
					return exitWith(3, "%v", err)
				}
				if full != nil {
					return outputDocument(cmd, query, lang, full, time.Since(start))
				}
			case len(cands) > 1:
				// An exact, case-sensitive match is not ambiguous: "Map" is
				// the constructor, while "map" is a common method name.
				for _, c := range cands {
					if c.Symbol == q {
						full, err := ix.Lookup(ctx, lang, c.Symbol)
						if err != nil {
							return exitWith(3, "%v", err)
						}
						if full != nil {
							return outputDocument(cmd, query, lang, full, time.Since(start))
						}
					}
				}
				if flagJSON {
					return outputJSON(cmd, query, lang, "lookup", docsToResults(cands), time.Since(start))
				}
				renderCandidates(cmd, query, lang, cands)
				return nil
			}
		} else {
			doc, err := ix.Lookup(ctx, lang, query)
			if err != nil {
				return exitWith(3, "%v", err)
			}
			if doc != nil {
				return outputDocument(cmd, query, lang, doc, time.Since(start))
			}
		}
	}

	matches, err := ix.Search(ctx, lang, query, 20)
	if err != nil {
		return exitWith(3, "%v", err)
	}
	if len(matches) == 0 {
		return noResultsForIndex(cmd, ix, lang, query)
	}

	// Free-form questions get a synthesized answer when a provider is
	// configured; otherwise the retrieval output below is the answer.
	var warnings []string
	if forced == "auto" && strings.Contains(query, " ") {
		if cfg, err := loadConfig(); err == nil {
			handled, w, err := synthesize(cmd, cfg, lang, query, matches, start)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
			warnings = w
		}
	}

	hints := symbolHints(ctx, ix, lang, query)

	if flagJSON {
		return outputJSONMode(cmd, query, lang, "search", "retrieval",
			retrievalResults(hints, matches), warnings, time.Since(start))
	}
	renderHints(cmd, hints)
	renderMatches(cmd, query, lang, matches)
	return nil
}

// symbolHints finds symbols literally mentioned in a multi-word question, so
// "diferencias entre let y const" surfaces the let and const pages instead of
// only lexical matches from examples.
func symbolHints(ctx context.Context, ix *index.Index, lang, query string) []index.Doc {
	tokens := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$'
	})
	if len(tokens) < 3 {
		return nil
	}

	seen := map[string]bool{}
	var out []index.Doc
	for _, tok := range tokens {
		key := strings.ToLower(tok)
		if len([]rune(tok)) < 2 || seen[key] {
			continue
		}
		seen[key] = true

		cands, err := ix.Candidates(ctx, lang, tok, 5)
		if err != nil {
			continue
		}
		for _, c := range cands {
			if strings.EqualFold(c.Symbol, tok) || strings.EqualFold(c.Symbol, tok+"()") {
				out = append(out, c)
				break
			}
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

func noResultsForIndex(cmd *cobra.Command, ix *index.Index, lang, query string) error {
	symbols, _ := ix.Symbols(context.Background(), lang)
	suggestions := ingest.SuggestStrings(symbols, query, 3)

	msg := fmt.Sprintf("no encontré %q en %s", query, lang)
	if len(suggestions) > 0 {
		msg += "\n       ¿quisiste decir?\n"
		for _, s := range suggestions {
			msg += "         · " + s + "\n"
		}
		msg = strings.TrimRight(msg, "\n")
	}
	return exitWith(1, "%s", msg)
}

func outputDocument(cmd *cobra.Command, query, lang string, doc *ingest.Document, elapsed time.Duration) error {
	if flagJSON {
		return outputJSON(cmd, query, lang, "lookup", []jsonResult{documentToResult(doc)}, elapsed)
	}
	return renderDocument(cmd, doc)
}

// --- JSON contract ---

type listJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Lang          string      `json:"lang"`
	Count         int         `json:"count"`
	Results       []index.Doc `json:"results"`
}

type jsonEnvelope struct {
	SchemaVersion int              `json:"schema_version"`
	Query         string           `json:"query"`
	Lang          string           `json:"lang"`
	Mode          string           `json:"mode"`
	Engine        string           `json:"engine"`
	Answer        string           `json:"answer,omitempty"`
	Grounded      *bool            `json:"grounded,omitempty"`
	Cached        *bool            `json:"cached,omitempty"`
	Citations     []jsonCitation   `json:"citations,omitempty"`
	Results       []jsonResult     `json:"results"`
	Warnings      []string         `json:"warnings,omitempty"`
	TimingsMS     map[string]int64 `json:"timings_ms"`
}

type jsonCitation struct {
	N      int    `json:"n"`
	Symbol string `json:"symbol"`
	URL    string `json:"url"`
}

type jsonResult struct {
	Symbol    string        `json:"symbol"`
	Kind      string        `json:"kind"`
	Title     string        `json:"title"`
	Signature string        `json:"signature,omitempty"`
	Summary   string        `json:"summary"`
	URL       string        `json:"url"`
	Score     float64       `json:"score"`
	Sections  []jsonSection `json:"sections,omitempty"`
}

type jsonSection struct {
	Heading     string `json:"heading"`
	HeadingPath string `json:"heading_path"`
	Body        string `json:"body"`
}

func documentToResult(doc *ingest.Document) jsonResult {
	r := jsonResult{
		Symbol:  doc.Symbol,
		Kind:    doc.Kind,
		Title:   doc.Title,
		Summary: doc.Summary,
		URL:     doc.URL,
		Score:   1,
	}
	for _, s := range doc.Sections {
		r.Sections = append(r.Sections, jsonSection{Heading: s.Heading, HeadingPath: s.HeadingPath, Body: s.Body})
	}
	return r
}

func docsToResults(docs []index.Doc) []jsonResult {
	out := make([]jsonResult, 0, len(docs))
	for _, d := range docs {
		out = append(out, jsonResult{
			Symbol:    d.Symbol,
			Kind:      d.Kind,
			Title:     d.Title,
			Signature: d.Signature,
			Summary:   d.Summary,
			URL:       d.URL,
			Score:     0,
		})
	}
	return out
}

func matchesToResults(matches []index.Match) []jsonResult {
	var out []jsonResult
	idx := map[int64]int{}
	for _, m := range matches {
		i, ok := idx[m.Doc.ID]
		if !ok {
			i = len(out)
			idx[m.Doc.ID] = i
			out = append(out, jsonResult{
				Symbol:    m.Doc.Symbol,
				Kind:      m.Doc.Kind,
				Title:     m.Doc.Title,
				Signature: m.Doc.Signature,
				Summary:   m.Doc.Summary,
				URL:       m.Doc.URL,
				Score:     -m.Rank,
			})
		}
		if len(out[i].Sections) < 2 {
			out[i].Sections = append(out[i].Sections, jsonSection{
				Heading:     m.Heading,
				HeadingPath: m.HeadingPath,
				Body:        m.Snippet,
			})
		}
	}
	return out
}

// retrievalResults merges literal symbol hints with lexical matches, keeping
// the first occurrence of each symbol.
func retrievalResults(hints []index.Doc, matches []index.Match) []jsonResult {
	results := docsToResults(hints)
	seen := map[string]bool{}
	for _, r := range results {
		seen[r.Symbol] = true
	}
	for _, r := range matchesToResults(matches) {
		if !seen[r.Symbol] {
			results = append(results, r)
		}
	}
	return results
}

func outputJSONMode(cmd *cobra.Command, query, lang, mode, engine string, results []jsonResult, warnings []string, elapsed time.Duration) error {
	env := jsonEnvelope{
		SchemaVersion: 1,
		Query:         query,
		Lang:          lang,
		Mode:          mode,
		Engine:        engine,
		Results:       results,
		Warnings:      warnings,
		TimingsMS:     map[string]int64{"retrieval": elapsed.Milliseconds(), "total": elapsed.Milliseconds()},
	}
	if env.Results == nil {
		env.Results = []jsonResult{}
	}
	return writeJSON(cmd, env)
}

func outputJSON(cmd *cobra.Command, query, lang, mode string, results []jsonResult, elapsed time.Duration) error {
	return outputJSONMode(cmd, query, lang, mode, "retrieval", results, nil, elapsed)
}
