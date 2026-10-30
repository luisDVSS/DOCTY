package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/llm"
	"github.com/spf13/cobra"
)

const (
	statusOK    = "ok"
	statusWarn  = "warn"
	statusError = "error"
)

type doctorReport struct {
	SchemaVersion int             `json:"schema_version"`
	Config        doctorConfig    `json:"config"`
	Index         doctorIndex     `json:"index"`
	Snapshot      doctorSnapshot  `json:"snapshot"`
	Provider      doctorProvider  `json:"provider"`
	Cache         doctorCache     `json:"cache"`
	Effective     doctorEffective `json:"effective"`
}

type doctorConfig struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type doctorIndex struct {
	Status   string `json:"status"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Bytes    int64  `json:"bytes,omitempty"`
	Symbols  int    `json:"symbols,omitempty"`
	Sections int    `json:"sections,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Error    string `json:"error,omitempty"`
}

type doctorSnapshot struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type doctorOnline struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorProvider struct {
	Status     string        `json:"status"`
	Configured bool          `json:"configured"`
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	Online     *doctorOnline `json:"online,omitempty"`
}

type doctorCache struct {
	Status    string `json:"status"`
	Answers   int    `json:"answers"`
	Bytes     int64  `json:"bytes"`
	HintShown bool   `json:"hint_shown"`
}

type doctorEffective struct {
	Lang      string `json:"lang"`
	Format    string `json:"format"`
	Pager     string `json:"pager"`
	Bootstrap bool   `json:"bootstrap"`
}

func newDoctorCmd() *cobra.Command {
	var online bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Revisa el estado de la instalación",
		Long: `Comprueba configuración, índice, fuentes y proveedor de LLM.

Por defecto no hace peticiones de red; usa --online para comprobar también
la conectividad con el proveedor.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				// Doctor debe diagnosticar, no fallar: sigue con defaults.
				cfg = config.Default()
				fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %v\n", err)
			}
			report := collectDoctor(cfg, online)
			if flagJSON {
				return writeJSON(cmd, report)
			}
			printDoctorText(cmd, report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "comprueba también la conectividad")
	return cmd
}

func collectDoctor(cfg config.Config, online bool) doctorReport {
	report := doctorReport{
		SchemaVersion: 1,
		Effective: doctorEffective{
			Lang:      cfg.DefaultLang,
			Format:    cfg.Format,
			Pager:     cfg.Pager,
			Bootstrap: cfg.Bootstrap,
		},
	}

	cfgPath := config.Path()
	if _, err := os.Stat(cfgPath); err == nil {
		report.Config = doctorConfig{Status: statusOK, Path: cfgPath, Exists: true}
	} else {
		report.Config = doctorConfig{Status: statusWarn, Path: cfgPath}
	}

	report.Index = doctorIndex{Path: config.IndexPath()}
	if st, err := os.Stat(config.IndexPath()); err == nil {
		report.Index.Exists = true
		report.Index.Bytes = st.Size()
		report.Index.Status = statusOK
		if ix, err := index.Open(config.IndexPath()); err != nil {
			report.Index.Status = statusError
			report.Index.Error = err.Error()
		} else {
			if docs, sections, err := ix.Counts(context.Background(), cfg.DefaultLang); err == nil {
				report.Index.Symbols = docs
				report.Index.Sections = sections
			} else {
				report.Index.Status = statusWarn
				report.Index.Error = err.Error()
			}
			if info, ok, err := ix.Source(context.Background(), cfg.DefaultLang); err == nil && ok {
				report.Index.Snapshot = info.Version
			}
			ix.Close()
		}
	} else {
		report.Index.Status = statusError
	}

	srcDir := filepath.Join(config.SourcesDir(), cfg.DefaultLang)
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		report.Snapshot = doctorSnapshot{Status: statusOK, Path: srcDir, Exists: true}
	} else {
		report.Snapshot = doctorSnapshot{Status: statusWarn, Path: srcDir}
	}

	report.Provider = doctorProvider{Status: statusWarn}
	if cfg.LLM.Provider == "" {
		report.Provider.Detail = "no configurado · usa: docty auth login"
	} else {
		report.Provider.Configured = true
		report.Provider.Provider = cfg.LLM.Provider
		settings, err := llm.Resolve(cfg.LLM)
		switch {
		case err == nil:
			report.Provider.Status = statusOK
			report.Provider.Model = settings.Model
			if online {
				status, detail := checkProvider(settings)
				report.Provider.Online = &doctorOnline{Status: statusFromSymbol(status), Detail: detail}
			}
		case errors.Is(err, llm.ErrNoKey):
			report.Provider.Status = statusError
			report.Provider.Model = cfg.LLM.Model
			report.Provider.Detail = "falta API key · usa: docty auth login"
		default:
			report.Provider.Status = statusError
			report.Provider.Detail = err.Error()
		}
	}

	st := llm.NewCache(config.CacheDir()).Stats()
	report.Cache = doctorCache{
		Status:    statusWarn,
		Answers:   st.Answers,
		Bytes:     st.Bytes,
		HintShown: st.HintShown,
	}
	if st.Answers > 0 {
		report.Cache.Status = statusOK
	}

	return report
}

func printDoctorText(cmd *cobra.Command, report doctorReport) {
	out := cmd.OutOrStdout()
	line := func(status, label, detail string) {
		fmt.Fprintf(out, "  %-10s %s %s\n", label, statusSymbol(status), detail)
	}

	fmt.Fprintln(out, "Docty — diagnóstico")
	fmt.Fprintln(out)

	cfgDetail := report.Config.Path
	if !report.Config.Exists {
		cfgDetail += " (no existe, usando valores por defecto)"
	}
	line(report.Config.Status, "config", cfgDetail)

	idxDetail := report.Index.Path
	if report.Index.Exists {
		idxDetail += " · " + humanBytes(report.Index.Bytes)
		if report.Index.Symbols > 0 || report.Index.Sections > 0 {
			idxDetail += fmt.Sprintf(" · %d símbolos, %d secciones", report.Index.Symbols, report.Index.Sections)
		}
		if report.Index.Snapshot != "" {
			idxDetail += " · snapshot " + report.Index.Snapshot
		}
		if report.Index.Error != "" {
			idxDetail += " · " + report.Index.Error
		}
	} else {
		idxDetail = "no instalado · ejecuta: docty update js"
	}
	line(report.Index.Status, "índice", idxDetail)

	snapDetail := report.Snapshot.Path
	if !report.Snapshot.Exists {
		snapDetail += " (no descargado)"
	}
	line(report.Snapshot.Status, "snapshot", snapDetail)

	providerDetail := report.Provider.Detail
	if providerDetail == "" && report.Provider.Configured {
		providerDetail = fmt.Sprintf("%s · %s", report.Provider.Provider, report.Provider.Model)
	}
	line(report.Provider.Status, "proveedor", providerDetail)
	if report.Provider.Online != nil {
		line(report.Provider.Online.Status, "conectividad", report.Provider.Online.Detail)
	}

	cacheDetail := "vacía"
	if report.Cache.Answers > 0 {
		label := "respuestas"
		if report.Cache.Answers == 1 {
			label = "respuesta"
		}
		cacheDetail = fmt.Sprintf("%d %s · %s", report.Cache.Answers, label, humanBytes(report.Cache.Bytes))
	}
	line(report.Cache.Status, "caché", cacheDetail)

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  configuración efectiva: %s\n", strings.Join([]string{
		"lang=" + report.Effective.Lang,
		"format=" + report.Effective.Format,
		"pager=" + report.Effective.Pager,
		"bootstrap=" + fmt.Sprint(report.Effective.Bootstrap),
	}, " "))
}

func statusSymbol(status string) string {
	switch status {
	case statusOK:
		return "✓"
	case statusWarn:
		return "·"
	default:
		return "✗"
	}
}

func statusFromSymbol(symbol string) string {
	switch symbol {
	case "✓":
		return statusOK
	case "·":
		return statusWarn
	default:
		return statusError
	}
}

// probeProvider checks the provider's /models endpoint. It returns the HTTP
// status and latency; a network or request error is returned separately.
func probeProvider(s llm.Settings) (int, time.Duration, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, s.BaseURL+"/models", nil)
	if err != nil {
		return 0, 0, err
	}
	if s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, time.Since(start), nil
}

func checkProvider(s llm.Settings) (string, string) {
	status, latency, err := probeProvider(s)
	if err != nil {
		return "✗", "sin conexión: " + err.Error()
	}
	detail := fmt.Sprintf("HTTP %d · %d ms", status, latency.Milliseconds())
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "✗", "clave rechazada · " + detail
	case status == http.StatusNotFound:
		return "·", "no verificable · " + detail
	case status >= 400:
		return "✗", detail
	default:
		return "✓", detail
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
