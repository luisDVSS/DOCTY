package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DOCTY_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("DOCTY_LANG", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLang != "js" {
		t.Errorf("default_lang = %q", cfg.DefaultLang)
	}
	if cfg.Format != "auto" {
		t.Errorf("format = %q", cfg.Format)
	}
	if !cfg.Bootstrap {
		t.Error("bootstrap debería ser true por defecto")
	}
}

func TestLoadFileAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := []byte(`
default_lang = "js"
format = "md"

[llm]
provider = "groq"
model = "openai/gpt-oss-20b"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOCTY_CONFIG", path)
	t.Setenv("DOCTY_LANG", "ts")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLang != "ts" {
		t.Errorf("DOCTY_LANG debería ganar al archivo: %q", cfg.DefaultLang)
	}
	if cfg.Format != "md" {
		t.Errorf("format = %q", cfg.Format)
	}
	if cfg.LLM.Provider != "groq" {
		t.Errorf("provider = %q", cfg.LLM.Provider)
	}
}

func TestLoadRejectsInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`format = "xml"`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTY_CONFIG", path)

	if _, err := Load(); err == nil {
		t.Fatal("se esperaba error con format inválido")
	}
}

func TestSaveLLMPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# comentario principal
default_lang = "js"
pager = "never"

[llm]
provider = "groq"
api_key = "plaintext"
model = "old-model"
timeout = "30s"

[other]
x = 1
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTY_CONFIG", path)
	t.Setenv("DOCTY_API_KEY", "")
	t.Setenv("DOCTY_LLM_PROVIDER", "")
	t.Setenv("DOCTY_LLM_MODEL", "")
	t.Setenv("DOCTY_LLM_BASE_URL", "")

	if err := SaveLLM("openrouter", "new-model", "https://example.test/v1"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`# comentario principal`,
		`default_lang = "js"`,
		`pager = "never"`,
		`provider = "openrouter"`,
		`model = "new-model"`,
		`base_url = "https://example.test/v1"`,
		`timeout = "30s"`,
		`[other]`,
		`x = 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en:\n%s", want, text)
		}
	}
	if strings.Contains(text, "api_key") {
		t.Errorf("api_key debería haberse eliminado:\n%s", text)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "openrouter" || cfg.LLM.Model != "new-model" {
		t.Errorf("llm = %+v", cfg.LLM)
	}
	if cfg.LLM.APIKey != "" {
		t.Errorf("api_key no debería cargarse: %q", cfg.LLM.APIKey)
	}
}

func TestSaveLLMRemovesEmptyBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `[llm]
provider = "openai-compatible"
base_url = "http://localhost:8080/v1"
model = "local"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTY_CONFIG", path)

	if err := SaveLLM("groq", "openai/gpt-oss-20b", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "base_url") {
		t.Errorf("base_url debería haberse eliminado:\n%s", data)
	}
}

func TestSaveLLMCreatesNestedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	t.Setenv("DOCTY_CONFIG", path)

	if err := SaveLLM("groq", "openai/gpt-oss-20b", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "[llm]\n") {
		t.Errorf("archivo nuevo inesperado:\n%s", text)
	}
	if !strings.Contains(text, `provider = "groq"`) {
		t.Errorf("falta provider:\n%s", text)
	}
}

func TestLoadWithOrigins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `default_lang = "js"
format = "md"

[llm]
provider = "groq"
model = "m"
timeout = "30s"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTY_CONFIG", path)
	t.Setenv("DOCTY_LANG", "ts")
	t.Setenv("DOCTY_LLM_PROVIDER", "openrouter")
	t.Setenv("DOCTY_API_KEY", "secret")
	t.Setenv("DOCTY_NO_PAGER", "1")

	cfg, origins, err := LoadWithOrigins()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLang != "ts" || origins["default_lang"] != SourceEnv {
		t.Errorf("default_lang = %q, origen = %q", cfg.DefaultLang, origins["default_lang"])
	}
	if cfg.Format != "md" || origins["format"] != SourceConfig {
		t.Errorf("format = %q, origen = %q", cfg.Format, origins["format"])
	}
	if cfg.Pager != "never" || origins["pager"] != SourceEnv {
		t.Errorf("pager = %q, origen = %q", cfg.Pager, origins["pager"])
	}
	if cfg.LLM.Provider != "openrouter" || origins["llm.provider"] != SourceEnv {
		t.Errorf("provider = %q, origen = %q", cfg.LLM.Provider, origins["llm.provider"])
	}
	if cfg.LLM.Model != "m" || origins["llm.model"] != SourceConfig {
		t.Errorf("model = %q, origen = %q", cfg.LLM.Model, origins["llm.model"])
	}
	if cfg.LLM.Timeout != "30s" || origins["llm.timeout"] != SourceConfig {
		t.Errorf("timeout = %q, origen = %q", cfg.LLM.Timeout, origins["llm.timeout"])
	}
	if cfg.LLM.APIKey != "secret" || origins["llm.api_key"] != SourceEnv {
		t.Errorf("api_key origen = %q", origins["llm.api_key"])
	}
	if origins["bootstrap"] != SourceDefault {
		t.Errorf("orígenes por defecto = %+v", origins)
	}
}

func TestUpdatePreservesCommentsAndSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# comentario principal
default_lang = "js"

[llm]
provider = "groq"
timeout = "30s"

[other]
x = 1
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTY_CONFIG", path)

	if changed, err := Set(path, "format", "md"); err != nil || !changed {
		t.Fatalf("Set format: changed=%v err=%v", changed, err)
	}
	if changed, err := Set(path, "bootstrap", false); err != nil || !changed {
		t.Fatalf("Set bootstrap: changed=%v err=%v", changed, err)
	}
	if changed, err := Set(path, "llm.model", "nuevo"); err != nil || !changed {
		t.Fatalf("Set model: changed=%v err=%v", changed, err)
	}
	if changed, err := Unset(path, "llm.provider"); err != nil || !changed {
		t.Fatalf("Unset provider: changed=%v err=%v", changed, err)
	}
	if changed, err := Unset(path, "llm.provider"); err != nil || changed {
		t.Fatalf("Unset provider repetido: changed=%v err=%v", changed, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`# comentario principal`,
		`format = "md"`,
		`bootstrap = false`,
		`model = "nuevo"`,
		`timeout = "30s"`,
		`[other]`,
		`x = 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en:\n%s", want, text)
		}
	}
	if strings.Contains(text, "provider") {
		t.Errorf("provider debería haberse eliminado:\n%s", text)
	}
	if strings.Index(text, `format = "md"`) > strings.Index(text, "[llm]") {
		t.Errorf("format debería quedar en la sección superior:\n%s", text)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Format != "md" || cfg.Bootstrap || cfg.LLM.Model != "nuevo" || cfg.LLM.Provider != "" {
		t.Errorf("config = %+v", cfg)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permisos = %o, want 600", info.Mode().Perm())
	}
}

func TestUpdateCreatesTableAndTypedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("DOCTY_CONFIG", path)

	if _, err := Set(path, "llm.provider", "groq"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "[llm]\n") {
		t.Errorf("archivo inesperado:\n%s", data)
	}

	if _, err := Set(path, "default_lang", "js"); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, "bootstrap", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, "llm.temperature", 0.5); err != nil {
		t.Fatal(err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`default_lang = "js"`,
		`bootstrap = false`,
		`temperature = 0.5`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en:\n%s", want, text)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLang != "js" || cfg.Bootstrap || cfg.LLM.Temperature != 0.5 {
		t.Errorf("config = %+v", cfg)
	}

	if _, err := Set(path, "a.b.c", "x"); err == nil {
		t.Error("se esperaba error con una clave anidada no soportada")
	}
}

func TestUnsetMissingDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.toml")
	changed, err := Unset(path, "llm.provider")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("unset de una clave inexistente no debería cambiar nada")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("no se debería crear el archivo: %v", err)
	}
}
