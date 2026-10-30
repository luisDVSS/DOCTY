package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ExitError carries a process exit code through cobra.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func exitWith(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}

var (
	flagJSON       bool
	flagNoColor    bool
	flagConfig     string
	flagLang       string
	flagVerbose    bool
	flagNoPager    bool
	flagNoLLM      bool
	flagRequireLLM bool
	flagSources    bool
	flagBrief      bool
	flagSimple     bool
	flagRefresh    bool
	flagStream     string
)

// Execute builds the command tree and returns the process exit code.
func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		var ee *ExitError
		if errors.As(err, &ee) {
			return ee.Code
		}
		return 2
	}
	return 0
}

// NewRoot assembles the CLI.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "docty",
		Short: "Documentación de programación desde la terminal",
		Long: `Docty consulta documentación de lenguajes de programación desde la
terminal, por símbolo o en lenguaje natural.

Ejemplos:
  docty js Array.prototype.map       consulta un símbolo
  docty -s js Array.prototype.map    consulta compacta
  docty js map                       desambigua por nombre corto
  docty js "copiar un objeto"        responde con el LLM (o muestra fragmentos)
  docty ask js "cómo copio un objeto" pregunta explícitamente
  docty update js                    descarga el snapshot y construye el índice
  docty get js Object.assign         fuerza la búsqueda exacta
  docty index js --from ./mdn        indexa un checkout local
  docty auth login                   guarda la API key en el keyring
  docty config                       muestra la configuración y su origen
  docty cache                        revisa el tamaño de la caché
  docty doctor                       revisa el estado de la instalación`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE:          runQuery,
	}

	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "salida JSON para scripts")
	root.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "desactiva los estilos ANSI (mantiene el renderizado)")
	root.PersistentFlags().StringVar(&flagConfig, "config", "", "ruta alternativa a config.toml")
	root.PersistentFlags().StringVar(&flagLang, "lang", "", "lenguaje por defecto (ej. js)")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "diagnóstico en stderr")
	root.PersistentFlags().BoolVar(&flagNoPager, "no-pager", false, "no usar paginador")
	root.PersistentFlags().BoolVar(&flagNoLLM, "no-llm", false, "no sintetizar respuestas; solo fragmentos")
	root.PersistentFlags().BoolVar(&flagRequireLLM, "require-llm", false, "falla si el LLM no está disponible")
	root.PersistentFlags().BoolVar(&flagSources, "sources", false, "muestra los fragmentos enviados al modelo")
	root.PersistentFlags().BoolVar(&flagBrief, "brief", false, "solo la respuesta, sin fuentes")
	root.PersistentFlags().BoolVarP(&flagSimple, "simple", "s", false, "salida compacta para consultas y respuestas")
	root.PersistentFlags().BoolVar(&flagRefresh, "refresh", false, "ignora la caché de respuestas")
	root.PersistentFlags().StringVar(&flagStream, "stream", "auto", "streaming del LLM: auto, always, never")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newIngestCmd())
	root.AddCommand(newIndexCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newSourcesCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newCacheCmd())
	wrapJSONErrors(root)
	return root
}

// applyConfigFlag points the config loader at an explicit file.
func applyConfigFlag() error {
	if flagConfig == "" {
		return nil
	}
	if _, err := os.Stat(flagConfig); err != nil {
		return exitWith(2, "no se pudo leer %s: %v", flagConfig, err)
	}
	return applyConfigFlagAllowMissing()
}

// applyConfigFlagAllowMissing points the config loader at an explicit file
// without requiring it to exist yet.
func applyConfigFlagAllowMissing() error {
	if flagConfig == "" {
		return nil
	}
	if err := os.Setenv("DOCTY_CONFIG", flagConfig); err != nil {
		return exitWith(2, "no se pudo usar %s: %v", flagConfig, err)
	}
	return nil
}

func wrapJSONErrors(cmd *cobra.Command) {
	if cmd.RunE != nil {
		run := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			err := run(c, args)
			if err != nil && flagJSON {
				code, exit := jsonErrorFor(err)
				writeJSONError(c.OutOrStdout(), code, err.Error(), exit)
			}
			return err
		}
	}
	for _, child := range cmd.Commands() {
		wrapJSONErrors(child)
	}
}

func jsonErrorFor(err error) (string, int) {
	var ee *ExitError
	if errors.As(err, &ee) {
		switch ee.Code {
		case 1:
			return "no_results", 1
		case 2:
			return "usage", 2
		case 3:
			return "source", 3
		case 4:
			return "llm_required", 4
		case 130:
			return "interrupted", 130
		}
		return "error", ee.Code
	}
	return "error", 2
}
