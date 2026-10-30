package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "index [lang]",
		Short: "Construye el índice SQLite a partir de un snapshot local",
		Long: `Parsea las páginas y escribe el índice que usan las consultas.

Ejemplos:
  docty index js --from ./mdn
  docty index js --from ~/.local/share/docty/sources/js`,
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
					"no encuentro el snapshot en %s\n       indica una ruta con --from o descárgalo con: docty update %s", root, lang)
			}

			docs, err := loadFiltered(root, manifest)
			if err != nil {
				return exitWith(3, "%v", err)
			}
			if len(docs) == 0 {
				return exitWith(1, "no se encontraron páginas de %s en %s", manifest.Name, root)
			}

			start := time.Now()
			ctx := context.Background()
			if err := index.Build(ctx, config.IndexPath(), manifest, "local", docs); err != nil {
				return exitWith(3, "construyendo el índice: %v", err)
			}
			elapsed := time.Since(start)

			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d páginas indexadas en %s (%d ms)\n",
				lang, len(docs), config.IndexPath(), elapsed.Milliseconds())
			fmt.Fprintf(cmd.OutOrStdout(), "prueba: docty %s %s\n", lang, docs[0].Symbol)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "directorio de mdn/content o de páginas Markdown")
	return cmd
}

// loadFiltered parses a snapshot and keeps only the page types of a source.
func loadFiltered(root string, m source.Manifest) ([]*ingest.Document, error) {
	docs, err := ingest.LoadDir(root)
	if err != nil {
		return nil, err
	}
	filtered := docs[:0]
	for _, d := range docs {
		if m.Accepts(d.PageType) {
			filtered = append(filtered, d)
		}
	}
	return filtered, nil
}

// buildFromSnapshot indexes a local snapshot, used as a lightweight bootstrap
// when the index is missing but the docs are already downloaded.
func buildFromSnapshot(lang string) error {
	m, ok := source.Get(lang)
	if !ok {
		return fmt.Errorf("el lenguaje %q no está soportado", lang)
	}
	dir := filepath.Join(config.SourcesDir(), lang)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("no hay snapshot en %s", dir)
	}
	docs, err := loadFiltered(dir, m)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("el snapshot de %s está vacío", dir)
	}
	fmt.Fprintf(os.Stderr, "aviso: índice no encontrado; indexando el snapshot local de %s…\n", lang)
	if err := index.Build(context.Background(), config.IndexPath(), m, "local", docs); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "aviso: %d páginas indexadas en %s\n", len(docs), config.IndexPath())
	return nil
}

// openIndex opens the index or fails with an actionable message.
func openIndex(lang string) (*index.Index, error) {
	path := config.IndexPath()
	if _, err := os.Stat(path); err != nil {
		if cfgBootstrap {
			if berr := buildFromSnapshot(lang); berr != nil {
				return nil, exitWith(3,
					"no hay índice instalado en %s\n"+
						"       descarga el snapshot y constrúyelo con: docty update %s\n"+
						"       (o usa: docty index %s --from <ruta mdn/content>)", path, lang, lang)
			}
		} else {
			return nil, exitWith(3,
				"no hay índice instalado en %s\n"+
					"       constrúyelo con: docty update %s\n"+
					"       (o usa: docty index %s --from <ruta mdn/content>)", path, lang, lang)
		}
	}
	ix, err := index.Open(path)
	if err != nil {
		return nil, exitWith(3, "%v", err)
	}
	return ix, nil
}
