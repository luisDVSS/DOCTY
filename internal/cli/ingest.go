package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

type ingestRow struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

type ingestListJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Lang          string      `json:"lang"`
	Count         int         `json:"count"`
	Documents     []ingestRow `json:"documents"`
}

type ingestDocJSON struct {
	SchemaVersion int              `json:"schema_version"`
	Document      *ingest.Document `json:"document"`
}

func newIngestCmd() *cobra.Command {
	var (
		from string
		dump string
	)
	cmd := &cobra.Command{
		Use:   "ingest [lang]",
		Short: "Parsea documentación local y muestra el resultado",
		Long: `Parsea un checkout de mdn/content (o una carpeta de páginas Markdown),
normaliza macros y divide cada página en secciones.

Todavía no escribe el índice SQLite: sirve para validar el pipeline.

Ejemplos:
  docty ingest js --from ./mdn
  docty ingest js --from ./mdn --dump Array.prototype.map
  docty ingest js --from ./mdn --dump "Spread syntax" --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lang := cfg.DefaultLang
			if len(args) == 1 {
				lang = args[0]
			}
			manifest, ok := source.Get(lang)
			if !ok {
				return exitWith(2, "el lenguaje %q no está soportado (disponibles: %s)", lang, sourceIDs())
			}

			root := from
			if root == "" {
				root = filepath.Join(config.SourcesDir(), lang)
			}
			if st, err := os.Stat(root); err != nil || !st.IsDir() {
				return exitWith(3,
					"no encuentro el snapshot en %s\n       descarga mdn/content o indica una ruta con --from", root)
			}

			docs, err := ingest.LoadDir(root)
			if err != nil {
				return exitWith(3, "%v", err)
			}

			filtered := docs[:0]
			for _, d := range docs {
				if manifest.Accepts(d.PageType) {
					filtered = append(filtered, d)
				}
			}
			docs = filtered

			if dump != "" {
				doc, ok := ingest.Find(docs, dump)
				if !ok {
					return noResults(docs, dump, lang)
				}
				return printDocument(cmd, doc)
			}

			if len(docs) == 0 {
				return exitWith(1, "no se encontraron páginas de %s en %s", manifest.Name, root)
			}
			reportUnknownMacros(cmd, docs)
			return listDocuments(cmd, docs, lang)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "directorio de mdn/content o de páginas Markdown")
	cmd.Flags().StringVar(&dump, "dump", "", "muestra una página completa por símbolo")
	return cmd
}

// reportUnknownMacros warns about dropped macros without flooding stderr with
// one line per page.
func reportUnknownMacros(cmd *cobra.Command, docs []*ingest.Document) {
	var (
		pages int
		names []string
		seen  = map[string]bool{}
	)
	for _, d := range docs {
		if len(d.UnknownMacros) == 0 {
			continue
		}
		pages++
		for _, name := range d.UnknownMacros {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if pages == 0 {
		return
	}
	if len(names) > 6 {
		names = append(names[:6], "…")
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %d páginas con macros sin expandir (%s)\n",
		pages, strings.Join(names, ", "))
}

func printDocument(cmd *cobra.Command, doc *ingest.Document) error {
	if len(doc.UnknownMacros) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "aviso: macros sin expandir en %s: %s\n",
			doc.Symbol, strings.Join(doc.UnknownMacros, ", "))
	}
	if flagJSON {
		return writeJSON(cmd, ingestDocJSON{SchemaVersion: 1, Document: doc})
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s   ·   %s\n", doc.Symbol, doc.Kind)
	fmt.Fprintf(out, "%s\n", strings.Repeat("─", max(20, len(doc.Symbol)+len(doc.Kind)+7)))
	fmt.Fprintf(out, "título:   %s\n", doc.Title)
	fmt.Fprintf(out, "slug:     %s\n", doc.Slug)
	fmt.Fprintf(out, "fuente:   %s\n", doc.URL)
	fmt.Fprintf(out, "secciones: %d\n", len(doc.Sections))
	fmt.Fprintln(out)

	for _, s := range doc.Sections {
		fmt.Fprintf(out, "── %s ──\n", s.Heading)
		fmt.Fprintln(out, s.Body)
		fmt.Fprintln(out)
	}
	return nil
}

func listDocuments(cmd *cobra.Command, docs []*ingest.Document, lang string) error {
	if flagJSON {
		rows := make([]ingestRow, 0, len(docs))
		for _, d := range docs {
			rows = append(rows, ingestRow{Symbol: d.Symbol, Kind: d.Kind, Title: d.Title, URL: d.URL})
		}
		return writeJSON(cmd, ingestListJSON{
			SchemaVersion: 1,
			Lang:          lang,
			Count:         len(rows),
			Documents:     rows,
		})
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s: %d páginas\n\n", lang, len(docs))
	for _, d := range docs {
		fmt.Fprintf(out, "  %-42s %s\n", d.Symbol, d.Kind)
	}
	return nil
}

func noResults(docs []*ingest.Document, query, lang string) error {
	suggestions := ingest.Suggestions(docs, query, 3)
	msg := fmt.Sprintf("no encontré %q en %s", query, lang)
	if len(suggestions) > 0 {
		msg += "\n       ¿quisiste decir?\n"
		for _, s := range suggestions {
			msg += "         · " + s + "\n"
		}
	}
	return exitWith(1, "%s", strings.TrimRight(msg, "\n"))
}

func sourceIDs() string {
	ids := make([]string, 0)
	for _, m := range source.All() {
		ids = append(ids, m.ID)
	}
	return strings.Join(ids, ", ")
}
