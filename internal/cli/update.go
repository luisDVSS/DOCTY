package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		ref   string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "update [lang]",
		Short: "Descarga el snapshot de documentación y reconstruye el índice",
		Long: `Hace un checkout disperso del repositorio upstream (solo las páginas del
lenguaje), reconstruye el índice y deja todo listo para consultar.

Por defecto usa el commit fijado en el manifest para que la ingesta sea
reproducible. Usa --ref main para traer la última versión.

Ejemplos:
  docty update js
  docty update js --ref main
  docty update js --force`,
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
			m, ok := source.Get(lang)
			if !ok {
				return exitWith(2, "el lenguaje %q no está soportado (disponibles: %s)", lang, sourceIDs())
			}

			targetRef := ref
			if targetRef == "" {
				targetRef = m.SnapshotCommit
			}
			dir := filepath.Join(config.SourcesDir(), lang)

			fmt.Fprintf(cmd.ErrOrStderr(), "actualizando %s desde %s…\n", m.Name, m.Upstream)
			if targetRef == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "  rama por defecto del upstream")
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "  ref %s\n", targetRef)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "  esto puede tardar unos segundos")

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			commit, err := source.Fetch(ctx, source.FetchOptions{
				Dir:      dir,
				Upstream: m.Upstream,
				Ref:      targetRef,
				Select:   m.Select,
				Force:    force,
				Log:      cmd.ErrOrStderr(),
			})
			if err != nil {
				return exitWith(3, "%v\n       si el commit fijado ya no está disponible, prueba: docty update %s --ref main", err, lang)
			}

			docs, err := loadFiltered(dir, m)
			if err != nil {
				return exitWith(3, "%v", err)
			}
			if len(docs) == 0 {
				return exitWith(3, "el snapshot descargado no contiene páginas de %s", m.Name)
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "  indexando…")
			start := time.Now()
			if err := index.Build(ctx, config.IndexPath(), m, shortCommit(commit), docs); err != nil {
				return exitWith(3, "construyendo el índice: %v", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d páginas · snapshot %s · índice %s (%d ms)\n",
				lang, len(docs), shortCommit(commit), config.IndexPath(), time.Since(start).Milliseconds())
			fmt.Fprintf(cmd.OutOrStdout(), "prueba: docty %s %s\n", lang, docs[0].Symbol)
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "commit, rama o tag (por defecto el fijado en el manifest)")
	cmd.Flags().BoolVar(&force, "force", false, "borra el snapshot local y descarga de nuevo")
	return cmd
}

func shortCommit(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
