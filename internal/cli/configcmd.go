package cli

import (
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/llm"
	"github.com/luisdvss/docty/internal/source"
	"github.com/spf13/cobra"
)

type configShowJSON struct {
	SchemaVersion int                       `json:"schema_version"`
	Config        string                    `json:"config"`
	Exists        bool                      `json:"exists"`
	Values        map[string]map[string]any `json:"values"`
	Warnings      []string                  `json:"warnings,omitempty"`
}

type configGetJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Key           string `json:"key"`
	Value         any    `json:"value"`
	Source        string `json:"source"`
}

type configKeySourceJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Key           string `json:"key"`
	Source        string `json:"source"`
	Set           bool   `json:"set"`
	KeyringError  string `json:"keyring_error,omitempty"`
}

type configSetJSON struct {
	SchemaVersion int      `json:"schema_version"`
	Key           string   `json:"key"`
	Value         any      `json:"value"`
	Config        string   `json:"config"`
	Changed       bool     `json:"changed"`
	Warnings      []string `json:"warnings,omitempty"`
}

type configUnsetJSON struct {
	SchemaVersion int      `json:"schema_version"`
	Key           string   `json:"key"`
	Removed       bool     `json:"removed"`
	Config        string   `json:"config"`
	Warnings      []string `json:"warnings,omitempty"`
}

type configPathJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Config        string `json:"config"`
	Exists        bool   `json:"exists"`
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Muestra y edita la configuración",
		Long: `Muestra la configuración efectiva y el origen de cada valor.

Los subcomandos get, set y unset permiten consultar y modificar claves sin
editar el TOML a mano. Los comentarios y las secciones desconocidas se
preservan. La API key no se guarda aquí: usa docty auth login.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd)
		},
	}
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigUnsetCmd())
	cmd.AddCommand(newConfigPathCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Muestra la configuración efectiva y su origen",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd)
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <clave>",
		Short: "Muestra el valor efectivo de una clave",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigGet(cmd, args[0])
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <clave> <valor>",
		Short: "Escribe una clave en config.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd, args[0], args[1])
		},
	}
}

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <clave>",
		Short: "Elimina una clave de config.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigUnset(cmd, args[0])
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Muestra la ruta de config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigPath(cmd)
		},
	}
}

func runConfigShow(cmd *cobra.Command) error {
	cfg, origins, err := loadConfigWithOrigins(true)
	if err != nil {
		return err
	}
	path := config.Path()
	exists := fileExists(path)
	values := make(map[string]map[string]any, len(config.KnownKeys()))
	for _, key := range config.KnownKeys() {
		entry := map[string]any{}
		if key == "llm.api_key" {
			_, source, keyErr := llm.ResolveKey(cfg.LLM)
			entry["source"] = string(source)
			entry["set"] = source != llm.KeySourceNone
			if keyErr != nil {
				entry["keyring_error"] = keyErr.Error()
			}
		} else {
			value, origin := effectiveConfigValue(cfg, origins, key)
			entry["value"] = value
			entry["source"] = string(origin)
		}
		values[key] = entry
	}
	warnings := showWarnings(cfg)
	printWarnings(cmd, warnings)

	if flagJSON {
		return writeJSON(cmd, configShowJSON{
			SchemaVersion: 1,
			Config:        path,
			Exists:        exists,
			Values:        values,
			Warnings:      warnings,
		})
	}
	printConfigShow(cmd.OutOrStdout(), cfg, path, exists, values)
	return nil
}

func runConfigGet(cmd *cobra.Command, key string) error {
	if !config.KnownKey(key) {
		return unknownConfigKey(key)
	}
	cfg, origins, err := loadConfigWithOrigins(true)
	if err != nil {
		return err
	}
	if key == "llm.api_key" {
		_, source, keyErr := llm.ResolveKey(cfg.LLM)
		if flagJSON {
			out := configKeySourceJSON{
				SchemaVersion: 1,
				Key:           key,
				Source:        string(source),
				Set:           source != llm.KeySourceNone,
			}
			if keyErr != nil {
				out.KeyringError = keyErr.Error()
			}
			return writeJSON(cmd, out)
		}
		fmt.Fprintln(cmd.OutOrStdout(), keySourceLabel(source))
		if keyErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %v\n", keyErr)
		}
		return nil
	}

	value, origin := effectiveConfigValue(cfg, origins, key)
	if flagJSON {
		return writeJSON(cmd, configGetJSON{
			SchemaVersion: 1,
			Key:           key,
			Value:         value,
			Source:        string(origin),
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), formatConfigValuePlain(value))
	return nil
}

func runConfigSet(cmd *cobra.Command, key, raw string) error {
	if !config.KnownKey(key) {
		return unknownConfigKey(key)
	}
	if key == "llm.api_key" {
		return exitWith(2, "no se guardan API keys en config.toml; usa docty auth login o DOCTY_API_KEY")
	}
	value, err := parseConfigValue(key, raw)
	if err != nil {
		return exitWith(2, "%v", err)
	}
	if err := applyConfigFlagAllowMissing(); err != nil {
		return err
	}
	path := config.Path()
	changed, err := config.Set(path, key, value)
	if err != nil {
		return exitWith(3, "no se pudo actualizar %s: %v", path, err)
	}
	warnings := setWarnings(key)
	printWarnings(cmd, warnings)
	if flagJSON {
		return writeJSON(cmd, configSetJSON{
			SchemaVersion: 1,
			Key:           key,
			Value:         value,
			Config:        path,
			Changed:       changed,
			Warnings:      warnings,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ %s = %s en %s\n", key, formatConfigValue(value), path)
	return nil
}

func runConfigUnset(cmd *cobra.Command, key string) error {
	if !config.KnownKey(key) {
		return unknownConfigKey(key)
	}
	if err := applyConfigFlagAllowMissing(); err != nil {
		return err
	}
	path := config.Path()
	removed, err := config.Unset(path, key)
	if err != nil {
		return exitWith(3, "no se pudo actualizar %s: %v", path, err)
	}
	warnings := unsetWarnings(key)
	printWarnings(cmd, warnings)
	if flagJSON {
		return writeJSON(cmd, configUnsetJSON{
			SchemaVersion: 1,
			Key:           key,
			Removed:       removed,
			Config:        path,
			Warnings:      warnings,
		})
	}
	if removed {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s eliminado de %s\n", key, path)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "· %s no estaba definido en %s\n", key, path)
	}
	return nil
}

func runConfigPath(cmd *cobra.Command) error {
	if err := applyConfigFlagAllowMissing(); err != nil {
		return err
	}
	path := config.Path()
	exists := fileExists(path)
	if flagJSON {
		return writeJSON(cmd, configPathJSON{
			SchemaVersion: 1,
			Config:        path,
			Exists:        exists,
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}

func effectiveConfigValue(cfg config.Config, origins map[string]config.Source, key string) (any, config.Source) {
	value, _ := config.Value(cfg, key)
	origin := origins[key]
	if key == "llm.model" || key == "llm.base_url" {
		raw, _ := value.(string)
		if settings, err := llm.ResolveProvider(cfg.LLM); err == nil {
			if key == "llm.model" {
				value = settings.Model
			} else {
				value = settings.BaseURL
			}
			if strings.TrimSpace(raw) == "" {
				origin = config.SourcePreset
			}
		}
	}
	return value, origin
}

func showWarnings(cfg config.Config) []string {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" || provider == "none" {
		return []string{"LLM no configurado · usa docty auth login"}
	}
	warnings := providerWarnings(cfg)
	if _, err := llm.ResolveProvider(cfg.LLM); err != nil && provider != "openai-compatible" {
		warnings = append(warnings, err.Error())
	}
	_, source, err := llm.ResolveKey(cfg.LLM)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	if source == llm.KeySourceNone && provider != "openai-compatible" {
		warnings = append(warnings, "falta la API key · usa docty auth login")
	}
	if source == llm.KeySourceConfig {
		warnings = append(warnings, "llm.api_key está en texto plano; usa docty auth login")
	}
	return warnings
}

func setWarnings(key string) []string {
	cfg, origins, err := config.LoadWithOrigins()
	if err != nil {
		return []string{"la configuración quedó inválida: " + err.Error()}
	}
	if flagLang != "" {
		origins["default_lang"] = config.SourceFlag
	}
	var warnings []string
	if warning := overrideWarning(key, origins, cfg.LLM.Provider); warning != "" {
		warnings = append(warnings, warning)
	}
	if key == "llm.provider" {
		warnings = append(warnings, providerSwitchWarnings(cfg)...)
	}
	if key == "llm.provider" || key == "llm.base_url" || key == "llm.model" {
		warnings = append(warnings, providerWarnings(cfg)...)
	}
	return warnings
}

func unsetWarnings(key string) []string {
	cfg, origins, err := config.LoadWithOrigins()
	if err != nil {
		return []string{"la configuración quedó inválida: " + err.Error()}
	}
	if flagLang != "" {
		origins["default_lang"] = config.SourceFlag
	}
	var warnings []string
	if warning := overrideWarning(key, origins, cfg.LLM.Provider); warning != "" {
		warnings = append(warnings, warning)
	}
	if key == "llm.provider" || key == "llm.base_url" || key == "llm.model" {
		warnings = append(warnings, providerWarnings(cfg)...)
	}
	return warnings
}

func overrideWarning(key string, origins map[string]config.Source, provider string) string {
	src := origins[key]
	if src != config.SourceEnv && src != config.SourceFlag {
		return ""
	}
	return fmt.Sprintf("%s tiene prioridad sobre %s", configOriginLabel(key, src, provider), key)
}

func providerWarnings(cfg config.Config) []string {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider != "openai-compatible" {
		return nil
	}
	var warnings []string
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		warnings = append(warnings, "openai-compatible requiere llm.base_url")
	}
	if strings.TrimSpace(cfg.LLM.Model) == "" {
		warnings = append(warnings, "openai-compatible requiere llm.model")
	}
	return warnings
}

func providerSwitchWarnings(cfg config.Config) []string {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider != "groq" && provider != "openrouter" {
		return nil
	}
	var warnings []string
	if strings.TrimSpace(cfg.LLM.BaseURL) != "" {
		warnings = append(warnings, "llm.base_url sigue configurado; usa docty config unset llm.base_url si no corresponde")
	}
	if strings.TrimSpace(cfg.LLM.Model) != "" {
		warnings = append(warnings, "llm.model sigue configurado; usa docty config unset llm.model para usar el default del proveedor")
	}
	return warnings
}

func parseConfigValue(key, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	switch key {
	case "default_lang":
		if raw == "" {
			return nil, fmt.Errorf("default_lang no puede estar vacío")
		}
		id := strings.ToLower(raw)
		if _, ok := source.Get(id); !ok {
			return nil, fmt.Errorf("lenguaje desconocido %q", raw)
		}
		return id, nil
	case "format":
		v := strings.ToLower(raw)
		switch v {
		case "auto", "tty", "md", "json":
			return v, nil
		}
		return nil, fmt.Errorf("format inválido %q: usa auto, tty, md o json", raw)
	case "pager":
		v := strings.ToLower(raw)
		switch v {
		case "auto", "never":
			return v, nil
		}
		return nil, fmt.Errorf("pager inválido %q: usa auto o never", raw)
	case "bootstrap":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("bootstrap debe ser true o false")
		}
		return b, nil
	case "llm.provider":
		v := strings.ToLower(raw)
		switch v {
		case "groq", "openrouter", "openai-compatible", "none":
			return v, nil
		}
		return nil, fmt.Errorf("proveedor inválido %q (usa groq, openrouter, openai-compatible o none)", raw)
	case "llm.model":
		if raw == "" {
			return nil, fmt.Errorf("llm.model no puede estar vacío; usa docty config unset llm.model para volver al default")
		}
		return raw, nil
	case "llm.base_url":
		if raw == "" {
			return nil, fmt.Errorf("llm.base_url no puede estar vacío; usa docty config unset llm.base_url")
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("llm.base_url inválida %q", raw)
		}
		if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
			return nil, fmt.Errorf("llm.base_url debe usar http o https")
		}
		return strings.TrimRight(raw, "/"), nil
	case "llm.timeout":
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("llm.timeout inválido %q (ej. 20s)", raw)
		}
		return raw, nil
	case "llm.max_tokens":
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("llm.max_tokens debe ser un entero > 0")
		}
		return n, nil
	case "llm.temperature":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 2 {
			return nil, fmt.Errorf("llm.temperature debe estar entre 0 y 2")
		}
		return f, nil
	case "llm.answer_lang":
		v := strings.ToLower(raw)
		switch v {
		case "auto", "es", "en":
			return v, nil
		}
		return nil, fmt.Errorf("llm.answer_lang inválido %q: usa auto, es o en", raw)
	}
	return nil, fmt.Errorf("clave desconocida %q", key)
}

func printConfigShow(w io.Writer, cfg config.Config, path string, exists bool, values map[string]map[string]any) {
	fmt.Fprintln(w, "Configuración efectiva")
	suffix := ""
	if !exists {
		suffix = " (no existe, usando valores por defecto)"
	}
	fmt.Fprintf(w, "  config  %s%s\n\n", path, suffix)
	for _, key := range []string{"default_lang", "format", "pager", "bootstrap"} {
		printConfigEntry(w, key, values[key], cfg.LLM.Provider)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  [llm]")
	for _, key := range []string{
		"llm.provider",
		"llm.model",
		"llm.base_url",
		"llm.timeout",
		"llm.max_tokens",
		"llm.temperature",
		"llm.answer_lang",
		"llm.api_key",
	} {
		printConfigEntry(w, key, values[key], cfg.LLM.Provider)
	}
}

func printConfigEntry(w io.Writer, key string, entry map[string]any, provider string) {
	displayKey := strings.TrimPrefix(key, "llm.")
	var valueText, sourceText string
	if key == "llm.api_key" {
		valueText = keySourceLabel(llm.KeySource(fmt.Sprint(entry["source"])))
		sourceText = valueText
	} else {
		valueText = formatConfigValue(entry["value"])
		sourceText = configOriginLabel(key, config.Source(fmt.Sprint(entry["source"])), provider)
	}
	fmt.Fprintf(w, "  %-18s = %-44s (%s)\n", displayKey, valueText, sourceText)
}

func printWarnings(cmd *cobra.Command, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %s\n", warning)
	}
}

func configOriginLabel(key string, src config.Source, provider string) string {
	switch src {
	case config.SourceConfig:
		return "config"
	case config.SourceEnv:
		if env := config.EnvVar(key); env != "" {
			return "env " + env
		}
		return "env"
	case config.SourceFlag:
		return "flag --lang"
	case config.SourcePreset:
		return "preset " + provider
	default:
		return "default"
	}
}

func keySourceLabel(source llm.KeySource) string {
	switch source {
	case llm.KeySourceEnv:
		return "env DOCTY_API_KEY"
	case llm.KeySourceConfig:
		return "config llm.api_key"
	case llm.KeySourceKeyring:
		return "keyring"
	default:
		return "none"
	}
}

func formatConfigValue(value any) string {
	switch v := value.(type) {
	case string:
		return strconv.Quote(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func formatConfigValuePlain(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func unknownConfigKey(key string) error {
	return exitWith(2, "clave desconocida %q; claves válidas: %s", key, strings.Join(config.KnownKeys(), ", "))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
