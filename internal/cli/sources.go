package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

type sourceRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	License   string `json:"license"`
	Installed bool   `json:"installed"`
	Status    string `json:"status"`
}

type sourcesJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Sources       []sourceRow `json:"sources"`
}

func newSourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "Lista las fuentes de documentación conocidas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var rows []sourceRow
			for _, m := range source.All() {
				installed, status := sourceState(m.ID)
				rows = append(rows, sourceRow{
					ID:        m.ID,
					Name:      m.Name,
					Provider:  m.Provider,
					License:   m.License,
					Installed: installed,
					Status:    status,
				})
			}

			if flagJSON {
				return writeJSON(cmd, sourcesJSON{SchemaVersion: 1, Sources: rows})
			}

			out := cmd.OutOrStdout()
			if outputMode() == formatMarkdown {
				fmt.Fprintln(out, "## Fuentes")
				fmt.Fprintln(out)
				for _, r := range rows {
					fmt.Fprintf(out, "- **%s** — %s (%s, %s) · %s\n", r.ID, r.Name, r.Provider, r.License, r.Status)
				}
				return nil
			}
			fmt.Fprintf(out, "%-6s %-12s %-8s %-14s %s\n", "ID", "NOMBRE", "FUENTE", "LICENCIA", "ESTADO")
			for _, r := range rows {
				fmt.Fprintf(out, "%-6s %-12s %-8s %-14s %s\n", r.ID, r.Name, r.Provider, r.License, r.Status)
			}
			return nil
		},
	}
}

// sourceState prefers the index: a source can be queryable even when the raw
// snapshot was deleted after indexing.
func sourceState(id string) (bool, string) {
	if ix, err := index.Open(config.IndexPath()); err == nil {
		_, ok, err := ix.Source(context.Background(), id)
		ix.Close()
		if err == nil && ok {
			return true, "indexada · " + config.IndexPath()
		}
	}
	dir := filepath.Join(config.SourcesDir(), id)
	if _, err := os.Stat(dir); err == nil {
		return true, "snapshot · " + dir
	}
	return false, "no instalada"
}
