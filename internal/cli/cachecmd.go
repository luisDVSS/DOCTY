package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/llm"
	"github.com/spf13/cobra"
)

type cacheStatsJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Path          string `json:"path"`
	Answers       int    `json:"answers"`
	Bytes         int64  `json:"bytes"`
	Oldest        string `json:"oldest,omitempty"`
	Newest        string `json:"newest,omitempty"`
	HintShown     bool   `json:"hint_shown"`
}

type cacheClearJSON struct {
	SchemaVersion  int    `json:"schema_version"`
	Path           string `json:"path"`
	AnswersRemoved int    `json:"answers_removed"`
	BytesFreed     int64  `json:"bytes_freed"`
	HintRemoved    bool   `json:"hint_removed"`
}

type cachePathJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
}

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Consulta y limpia la caché de respuestas",
		Long: `Muestra el estado de la caché de respuestas del LLM.

La caché vive en el directorio de caché de Docty. "cache clear" elimina solo
el directorio de respuestas y el aviso único del LLM; no toca otros archivos
en la raíz del directorio de caché.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheStats(cmd)
		},
	}
	cmd.AddCommand(newCacheStatsCmd())
	cmd.AddCommand(newCacheClearCmd())
	cmd.AddCommand(newCachePathCmd())
	return cmd
}

func newCacheStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Muestra el tamaño y la antigüedad de la caché",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheStats(cmd)
		},
	}
}

func newCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Elimina las respuestas cacheadas y el aviso del LLM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear(cmd)
		},
	}
}

func newCachePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Muestra la ruta del directorio de caché",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCachePath(cmd)
		},
	}
}

func runCacheStats(cmd *cobra.Command) error {
	path := config.CacheDir()
	st := llm.NewCache(path).Stats()
	if flagJSON {
		out := cacheStatsJSON{
			SchemaVersion: 1,
			Path:          path,
			Answers:       st.Answers,
			Bytes:         st.Bytes,
			HintShown:     st.HintShown,
		}
		if !st.Oldest.IsZero() {
			out.Oldest = st.Oldest.Format(time.RFC3339)
		}
		if !st.Newest.IsZero() {
			out.Newest = st.Newest.Format(time.RFC3339)
		}
		return writeJSON(cmd, out)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Caché de respuestas")
	fmt.Fprintf(out, "  ruta        %s\n", path)
	fmt.Fprintf(out, "  respuestas  %d\n", st.Answers)
	fmt.Fprintf(out, "  tamaño      %s\n", humanBytes(st.Bytes))
	if st.Oldest.IsZero() {
		fmt.Fprintln(out, "  rango       —")
	} else {
		fmt.Fprintf(out, "  rango       %s → %s\n",
			st.Oldest.Local().Format("2006-01-02 15:04"),
			st.Newest.Local().Format("2006-01-02 15:04"))
	}
	hint := "pendiente"
	if st.HintShown {
		hint = "ya mostrado"
	}
	fmt.Fprintf(out, "  aviso LLM   %s\n", hint)
	return nil
}

func runCacheClear(cmd *cobra.Command) error {
	path := config.CacheDir()
	result, err := llm.NewCache(path).Clear()
	if err != nil {
		return exitWith(3, "no se pudo limpiar la caché: %v", err)
	}
	if flagJSON {
		return writeJSON(cmd, cacheClearJSON{
			SchemaVersion:  1,
			Path:           path,
			AnswersRemoved: result.Answers,
			BytesFreed:     result.Bytes,
			HintRemoved:    result.HintRemoved,
		})
	}
	out := cmd.OutOrStdout()
	if result.Answers == 0 && !result.HintRemoved {
		fmt.Fprintln(out, "· la caché ya estaba vacía")
		return nil
	}
	label := "respuestas"
	if result.Answers == 1 {
		label = "respuesta"
	}
	fmt.Fprintf(out, "✓ caché limpiada: %d %s · %s\n", result.Answers, label, humanBytes(result.Bytes))
	if result.HintRemoved {
		fmt.Fprintln(out, "  aviso del LLM reiniciado")
	}
	return nil
}

func runCachePath(cmd *cobra.Command) error {
	path := config.CacheDir()
	_, err := os.Stat(path)
	exists := err == nil
	if flagJSON {
		return writeJSON(cmd, cachePathJSON{
			SchemaVersion: 1,
			Path:          path,
			Exists:        exists,
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}
