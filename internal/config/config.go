package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Source describes where an effective configuration value comes from.
type Source string

const (
	SourceDefault Source = "default"
	SourceConfig  Source = "config"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
	SourcePreset  Source = "preset"
)

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		DefaultLang: "js",
		Format:      "auto",
		Pager:       "auto",
		Bootstrap:   true,
		LLM: LLM{
			Provider:   "",
			Model:      "",
			BaseURL:    "",
			APIKey:     "",
			Timeout:    "20s",
			MaxTokens:  600,
			AnswerLang: "auto",
		},
	}
}

// Config is the user configuration as stored in config.toml.
type Config struct {
	DefaultLang string `toml:"default_lang"`
	Format      string `toml:"format"`
	Pager       string `toml:"pager"`
	Bootstrap   bool   `toml:"bootstrap"`
	LLM         LLM    `toml:"llm"`
}

type LLM struct {
	Provider    string  `toml:"provider"`
	Model       string  `toml:"model"`
	BaseURL     string  `toml:"base_url"`
	APIKey      string  `toml:"api_key"`
	Timeout     string  `toml:"timeout"`
	MaxTokens   int     `toml:"max_tokens"`
	Temperature float64 `toml:"temperature"`
	AnswerLang  string  `toml:"answer_lang"`
}

// Path returns the location of the configuration file.
func Path() string {
	if p := os.Getenv("DOCTY_CONFIG"); p != "" {
		return p
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "docty", "config.toml")
}

// DataDir returns the directory that holds regenerable index data.
func DataDir() string {
	if p := os.Getenv("DOCTY_DATA_DIR"); p != "" {
		return p
	}
	if p := os.Getenv("XDG_DATA_HOME"); p != "" {
		return filepath.Join(p, "docty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".docty")
	}
	return filepath.Join(home, ".local", "share", "docty")
}

// CacheDir returns the directory that holds caches.
func CacheDir() string {
	if p := os.Getenv("DOCTY_CACHE_DIR"); p != "" {
		return p
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = filepath.Join(DataDir(), "cache")
	}
	return filepath.Join(base, "docty")
}

// IndexPath is the SQLite index.
func IndexPath() string { return filepath.Join(DataDir(), "index.db") }

// SourcesDir holds downloaded documentation snapshots.
func SourcesDir() string { return filepath.Join(DataDir(), "sources") }

// Load reads the configuration file (if any) and applies environment
// overrides. Flags, when present, are applied by the caller on top.
func Load() (Config, error) {
	cfg, _, err := LoadWithOrigins()
	return cfg, err
}

// LoadWithOrigins is like Load but also reports where each effective value
// came from. The map always contains every known key.
func LoadWithOrigins() (Config, map[string]Source, error) {
	cfg := Default()
	origins := defaultOrigins()

	path := Path()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, origins, fmt.Errorf("configuración inválida en %s: %w", path, err)
		}
		var raw rawConfig
		if err := toml.Unmarshal(data, &raw); err != nil {
			return cfg, origins, fmt.Errorf("configuración inválida en %s: %w", path, err)
		}
		markFileOrigins(raw, origins)
	case os.IsNotExist(err):
		// Defaults are fine.
	default:
		return cfg, origins, fmt.Errorf("no se pudo leer %s: %w", path, err)
	}

	if v := os.Getenv("DOCTY_LANG"); v != "" {
		cfg.DefaultLang = v
		origins["default_lang"] = SourceEnv
	}
	if v := os.Getenv("DOCTY_FORMAT"); v != "" {
		cfg.Format = v
		origins["format"] = SourceEnv
	}
	if v := os.Getenv("DOCTY_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
		origins["llm.provider"] = SourceEnv
	}
	if v := os.Getenv("DOCTY_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
		origins["llm.model"] = SourceEnv
	}
	if v := os.Getenv("DOCTY_LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
		origins["llm.base_url"] = SourceEnv
	}
	if v := os.Getenv("DOCTY_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
		origins["llm.api_key"] = SourceEnv
	}
	if v, ok := os.LookupEnv("DOCTY_NO_PAGER"); ok {
		if parsed, err := strconv.ParseBool(v); err == nil && parsed {
			cfg.Pager = "never"
			origins["pager"] = SourceEnv
		}
	}
	if cfg.DefaultLang == "" {
		cfg.DefaultLang = "js"
		origins["default_lang"] = SourceDefault
	}
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if !validFormat(cfg.Format) {
		return cfg, origins, fmt.Errorf("formato inválido %q: usa auto, tty, md o json", cfg.Format)
	}
	return cfg, origins, nil
}

type rawConfig struct {
	DefaultLang *string `toml:"default_lang"`
	Format      *string `toml:"format"`
	Pager       *string `toml:"pager"`
	Bootstrap   *bool   `toml:"bootstrap"`
	LLM         *rawLLM `toml:"llm"`
}

type rawLLM struct {
	Provider    *string  `toml:"provider"`
	Model       *string  `toml:"model"`
	BaseURL     *string  `toml:"base_url"`
	APIKey      *string  `toml:"api_key"`
	Timeout     *string  `toml:"timeout"`
	MaxTokens   *int     `toml:"max_tokens"`
	Temperature *float64 `toml:"temperature"`
	AnswerLang  *string  `toml:"answer_lang"`
}

func defaultOrigins() map[string]Source {
	origins := make(map[string]Source, len(knownKeys))
	for _, key := range knownKeys {
		origins[key] = SourceDefault
	}
	return origins
}

func markFileOrigins(raw rawConfig, origins map[string]Source) {
	if raw.DefaultLang != nil {
		origins["default_lang"] = SourceConfig
	}
	if raw.Format != nil {
		origins["format"] = SourceConfig
	}
	if raw.Pager != nil {
		origins["pager"] = SourceConfig
	}
	if raw.Bootstrap != nil {
		origins["bootstrap"] = SourceConfig
	}
	if raw.LLM == nil {
		return
	}
	if raw.LLM.Provider != nil {
		origins["llm.provider"] = SourceConfig
	}
	if raw.LLM.Model != nil {
		origins["llm.model"] = SourceConfig
	}
	if raw.LLM.BaseURL != nil {
		origins["llm.base_url"] = SourceConfig
	}
	if raw.LLM.APIKey != nil {
		origins["llm.api_key"] = SourceConfig
	}
	if raw.LLM.Timeout != nil {
		origins["llm.timeout"] = SourceConfig
	}
	if raw.LLM.MaxTokens != nil {
		origins["llm.max_tokens"] = SourceConfig
	}
	if raw.LLM.Temperature != nil {
		origins["llm.temperature"] = SourceConfig
	}
	if raw.LLM.AnswerLang != nil {
		origins["llm.answer_lang"] = SourceConfig
	}
}

func validFormat(s string) bool {
	switch strings.ToLower(s) {
	case "auto", "tty", "md", "json":
		return true
	}
	return false
}

var knownKeys = []string{
	"default_lang",
	"format",
	"pager",
	"bootstrap",
	"llm.provider",
	"llm.model",
	"llm.base_url",
	"llm.api_key",
	"llm.timeout",
	"llm.max_tokens",
	"llm.temperature",
	"llm.answer_lang",
}

// KnownKeys returns the configuration keys understood by config get/set/unset.
func KnownKeys() []string {
	return append([]string(nil), knownKeys...)
}

// KnownKey reports whether key is a supported configuration key.
func KnownKey(key string) bool {
	for _, k := range knownKeys {
		if k == key {
			return true
		}
	}
	return false
}

// EnvVar returns the environment variable that overrides key, if any.
func EnvVar(key string) string {
	switch key {
	case "default_lang":
		return "DOCTY_LANG"
	case "format":
		return "DOCTY_FORMAT"
	case "pager":
		return "DOCTY_NO_PAGER"
	case "llm.provider":
		return "DOCTY_LLM_PROVIDER"
	case "llm.model":
		return "DOCTY_LLM_MODEL"
	case "llm.base_url":
		return "DOCTY_LLM_BASE_URL"
	case "llm.api_key":
		return "DOCTY_API_KEY"
	}
	return ""
}

// Value returns the effective value for a known key. The API key is
// intentionally not returned; use llm.ResolveKey for its origin.
func Value(cfg Config, key string) (any, bool) {
	switch key {
	case "default_lang":
		return cfg.DefaultLang, true
	case "format":
		return cfg.Format, true
	case "pager":
		return cfg.Pager, true
	case "bootstrap":
		return cfg.Bootstrap, true
	case "llm.provider":
		return cfg.LLM.Provider, true
	case "llm.model":
		return cfg.LLM.Model, true
	case "llm.base_url":
		return cfg.LLM.BaseURL, true
	case "llm.api_key":
		return nil, true
	case "llm.timeout":
		return cfg.LLM.Timeout, true
	case "llm.max_tokens":
		return cfg.LLM.MaxTokens, true
	case "llm.temperature":
		return cfg.LLM.Temperature, true
	case "llm.answer_lang":
		return cfg.LLM.AnswerLang, true
	}
	return nil, false
}

// Change is one update to the TOML file. A nil Value removes the key.
type Change struct {
	Key   string
	Value any
}

// Set writes one key to path, preserving comments and unrelated sections.
func Set(path, key string, value any) (bool, error) {
	return Update(path, []Change{{Key: key, Value: value}})
}

// Unset removes one key from path. It reports whether the key was present.
func Unset(path, key string) (bool, error) {
	return Update(path, []Change{{Key: key}})
}

// Update applies changes atomically. It returns whether the file changed.
func Update(path string, changes []Change) (bool, error) {
	var lines []string
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && len(data) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}

	changed := false
	for _, change := range changes {
		var c bool
		lines, c, err = applyChange(lines, change)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	if !changed {
		return false, nil
	}

	text := strings.Join(lines, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".docty-config-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

func applyChange(lines []string, change Change) ([]string, bool, error) {
	table, leaf, err := splitKey(change.Key)
	if err != nil {
		return lines, false, err
	}

	start, end, found := findSection(lines, table)
	if !found {
		if change.Value == nil {
			return lines, false, nil
		}
		if table != "" {
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, "["+table+"]")
			start = len(lines) - 1
			end = len(lines)
		} else {
			start = -1
			end = len(lines)
		}
	}

	sectionStart := 0
	if table != "" {
		sectionStart = start + 1
	}

	section := make([]string, 0, end-sectionStart+1)
	foundKey := false
	for i := sectionStart; i < end; i++ {
		line := lines[i]
		key, ok := tomlKey(line)
		if ok && key == leaf {
			foundKey = true
			if change.Value == nil {
				continue
			}
			section = append(section, formatKeyValue(leaf, change.Value))
			continue
		}
		section = append(section, line)
	}

	if change.Value == nil {
		if !foundKey {
			return lines, false, nil
		}
		out := append([]string{}, lines[:sectionStart]...)
		out = append(out, section...)
		out = append(out, lines[end:]...)
		return out, true, nil
	}

	if !foundKey {
		insertAt := len(section)
		for insertAt > 0 && strings.TrimSpace(section[insertAt-1]) == "" {
			insertAt--
		}
		line := formatKeyValue(leaf, change.Value)
		section = append(section[:insertAt], append([]string{line}, section[insertAt:]...)...)
	}

	out := append([]string{}, lines[:sectionStart]...)
	out = append(out, section...)
	out = append(out, lines[end:]...)
	return out, true, nil
}

func findSection(lines []string, table string) (start, end int, found bool) {
	if table == "" {
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "[") {
				return -1, i, true
			}
		}
		return -1, len(lines), true
	}

	start = -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "[") {
			continue
		}
		if start < 0 {
			if headerName(t) == table {
				start = i
			}
			continue
		}
		return start, i, true
	}
	if start >= 0 {
		return start, len(lines), true
	}
	return -1, len(lines), false
}

func headerName(t string) string {
	t = strings.TrimSpace(t)
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	end := strings.IndexByte(t, ']')
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(t[1:end])
}

func splitKey(key string) (table, leaf string, err error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("clave vacía")
	}
	parts := strings.Split(key, ".")
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("clave inválida %q", key)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("clave no soportada %q", key)
	}
}

func formatKeyValue(key string, value any) string {
	return key + " = " + formatTOMLValue(value)
}

func formatTOMLValue(value any) string {
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
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return strconv.Quote(fmt.Sprint(value))
	}
}

func tomlKey(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
		return "", false
	}
	eq := strings.IndexByte(t, '=')
	if eq < 0 {
		return "", false
	}
	key := strings.TrimSpace(t[:eq])
	if key == "" {
		return "", false
	}
	return key, true
}

// SaveLLM updates the [llm] table in config.toml. It sets provider, model and
// base_url (an empty base_url removes the key) and removes any plaintext
// api_key. Other settings, comments and sections are preserved.
func SaveLLM(provider, model, baseURL string) error {
	changes := []Change{
		{Key: "llm.provider", Value: provider},
		{Key: "llm.model", Value: model},
	}
	if baseURL == "" {
		changes = append(changes, Change{Key: "llm.base_url"})
	} else {
		changes = append(changes, Change{Key: "llm.base_url", Value: baseURL})
	}
	changes = append(changes, Change{Key: "llm.api_key"})
	_, err := Update(Path(), changes)
	return err
}
