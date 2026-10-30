package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luisdvss/docty/internal/auth"
	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
)

func setupCLI(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("DOCTY_DATA_DIR", dir)
	t.Setenv("DOCTY_CACHE_DIR", filepath.Join(dir, "cache"))
	t.Setenv("DOCTY_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("DOCTY_LANG", "")
	t.Setenv("DOCTY_API_KEY", "")
	t.Setenv("DOCTY_LLM_PROVIDER", "")
	t.Setenv("DOCTY_LLM_MODEL", "")
	t.Setenv("DOCTY_LLM_BASE_URL", "")
	t.Setenv("DOCTY_NO_PAGER", "1")
	t.Setenv("DOCTY_FORMAT", "tty")

	docs, err := ingest.LoadDir("../../testdata/mdn")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := source.Get("js")
	if !ok {
		t.Fatal("falta el manifest js")
	}
	if err := index.Build(context.Background(), config.IndexPath(), m, "test", docs); err != nil {
		t.Fatal(err)
	}
}

func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRoot()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errb.String(), err
}

func runCLIInput(t *testing.T, input string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRoot()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errb.String(), err
}

func useMemoryKeyring(t *testing.T) *auth.MemoryStore {
	t.Helper()
	store := auth.NewMemoryStore()
	restore := auth.SetStore(store)
	t.Cleanup(restore)
	return store
}

func exitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if err != nil {
		return -1
	}
	return 0
}

func TestCLILookupJSON(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "js", "Array.prototype.map", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", env.SchemaVersion)
	}
	if env.Mode != "lookup" || len(env.Results) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Results[0].Symbol != "Array.prototype.map" {
		t.Errorf("symbol = %q", env.Results[0].Symbol)
	}
	if len(env.Results[0].Sections) == 0 {
		t.Error("sin secciones en el JSON")
	}
}

func TestCLIShortNameResolves(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "js", "map")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Array.prototype.map") {
		t.Errorf("no resolvió Array.prototype.map:\n%s", stdout)
	}
	if strings.Contains(stdout, "no encontré") {
		t.Errorf("no debería fallar:\n%s", stdout)
	}
}

func TestCLISimpleLookup(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "-s", "js", "Array.prototype.map")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Array.prototype.map · método") {
		t.Errorf("falta el encabezado compacto:\n%s", stdout)
	}
	if !strings.Contains(stdout, "map(callbackFn)") {
		t.Errorf("falta la sintaxis:\n%s", stdout)
	}
	if strings.Contains(stdout, "── Parameters ──") || strings.Contains(stdout, "── Description ──") {
		t.Errorf("--simple no debe mostrar secciones largas:\n%s", stdout)
	}
	if strings.Contains(stdout, "Fuente:") {
		t.Errorf("--simple no debe usar la etiqueta Fuente:\n%s", stdout)
	}
	if !strings.Contains(stdout, "https://developer.mozilla.org") {
		t.Errorf("falta la URL:\n%s", stdout)
	}
}

func TestCLISimpleSearch(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "search", "js", "copiar objeto", "-s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Object.assign · método estático") {
		t.Errorf("falta Object.assign:\n%s", stdout)
	}
	if strings.Contains(stdout, "Warning for deep clone") {
		t.Errorf("--simple no debe mostrar el segundo fragmento:\n%s", stdout)
	}
	if strings.Contains(stdout, "```") || strings.Contains(stdout, "js const original") {
		t.Errorf("--simple no debe mostrar fences ni anotaciones en el snippet:\n%s", stdout)
	}
}

func TestRenderDocumentTTYStripsMarkdown(t *testing.T) {
	old := cfgFormat
	cfgFormat = "tty"
	t.Cleanup(func() { cfgFormat = old })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	doc := &ingest.Document{
		Symbol:  "demo",
		Kind:    "function",
		URL:     "https://example.test/demo",
		Summary: "Un **resumen** con `code`.",
		Sections: []ingest.Section{
			{Heading: "Description", Body: "Texto **fuerte**.\n\n```js\nconst x = 1;\n```"},
		},
	}
	if err := renderDocument(root, doc); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, bad := range []string{"**", "```", "##"} {
		if strings.Contains(got, bad) {
			t.Errorf("la salida TTY no debe contener %q:\n%s", bad, got)
		}
	}
	for _, want := range []string{"resumen", "fuerte", "┌─ js", "const x = 1;"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en la salida TTY:\n%s", want, got)
		}
	}
}

func TestRenderDocumentMarkdownKeepsMarkers(t *testing.T) {
	old := cfgFormat
	cfgFormat = "md"
	t.Cleanup(func() { cfgFormat = old })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	doc := &ingest.Document{
		Symbol:   "demo",
		Kind:     "function",
		URL:      "https://example.test/demo",
		Summary:  "Un **resumen**.",
		Sections: []ingest.Section{{Heading: "Description", Body: "Texto **fuerte**."}},
	}
	if err := renderDocument(root, doc); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"## Description", "**fuerte**", "**resumen**"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en Markdown:\n%s", want, got)
		}
	}
}

func TestRenderCandidatesSimple(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	flagSimple = true
	t.Cleanup(func() { flagSimple = false })

	docs := []index.Doc{
		{Symbol: "Map", Kind: "class", Summary: "The **Map** object holds key-value pairs."},
		{Symbol: "Array.prototype.map", Kind: "method", Summary: "The **map()** method creates a new array."},
	}
	renderCandidates(root, "map", "js", docs)

	got := out.String()
	if !strings.Contains(got, "Map · clase") || !strings.Contains(got, "Array.prototype.map · método") {
		t.Errorf("faltan candidatos:\n%s", got)
	}
	if strings.Contains(got, "holds key-value pairs") {
		t.Errorf("--simple no debe mostrar resúmenes de candidatos:\n%s", got)
	}
}

func TestCLIAskSimpleSources(t *testing.T) {
	setupCLI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa Object.assign [1]."}}]}`)
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--simple", "--stream=never")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("falta la respuesta:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Fuentes\n") || !strings.Contains(stdout, "  [1] Object.assign · https://developer.mozilla.org") {
		t.Errorf("faltan fuentes compactas:\n%s", stdout)
	}
	if strings.Contains(stdout, "Fuentes (1)") {
		t.Errorf("--simple no debe mostrar el contador de fuentes:\n%s", stdout)
	}

	stdout, stderr, err = runCLI(t, "ask", "js", "copiar un objeto", "--simple", "--brief")
	if err != nil {
		t.Fatalf("brief: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "Fuentes") {
		t.Errorf("--brief no debe mostrar fuentes:\n%s", stdout)
	}

	stdout, stderr, err = runCLI(t, "ask", "js", "copiar un objeto", "--simple", "--sources", "--refresh")
	if err != nil {
		t.Fatalf("sources: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "[1] Object.assign —") {
		t.Errorf("--sources debe mostrar el fragmento completo:\n%s", stdout)
	}
	if strings.Contains(stdout, "\nFuentes\n") {
		t.Errorf("--simple --sources no debe duplicar la lista compacta:\n%s", stdout)
	}
}

func TestCLISimpleDoesNotChangeJSON(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "js", "Array.prototype.map", "--simple", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.Mode != "lookup" || len(env.Results) != 1 || len(env.Results[0].Sections) == 0 {
		t.Errorf("--simple no debe recortar el JSON: %+v", env)
	}
}

func TestCLINoResultsExitCode(t *testing.T) {
	setupCLI(t)

	_, stderr, err := runCLI(t, "get", "js", "NoExiste")
	if code := exitCode(err); code != 1 {
		t.Errorf("exit = %d, want 1 (stderr=%s)", code, stderr)
	}
}

func TestCLIRetrievalFallbackWithoutLLM(t *testing.T) {
	setupCLI(t)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Object.assign") {
		t.Errorf("no recuperó Object.assign:\n%s", stdout)
	}
	if !strings.Contains(stderr, "sin proveedor LLM") {
		t.Errorf("falta el aviso de LLM:\n%s", stderr)
	}
}

func TestCLIRequireLLMFailsWithExit4(t *testing.T) {
	setupCLI(t)

	_, _, err := runCLI(t, "ask", "js", "copiar un objeto", "--require-llm")
	if code := exitCode(err); code != 4 {
		t.Errorf("exit = %d, want 4", code)
	}
}

func writeLLMConfig(t *testing.T, baseURL string) {
	t.Helper()
	path := os.Getenv("DOCTY_CONFIG")
	if path == "" {
		t.Fatal("DOCTY_CONFIG no está definido")
	}
	content := fmt.Sprintf(`[llm]
provider = "openai-compatible"
base_url = %q
model = "test-model"
api_key = "dummy"
timeout = "5s"
max_tokens = 100
`, baseURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSSE(w io.Writer, f http.Flusher, content string) {
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{"delta": map[string]any{"content": content}},
		},
	})
	fmt.Fprintf(w, "data: %s\n\n", payload)
	f.Flush()
}

func TestCLIAskStreams(t *testing.T) {
	setupCLI(t)

	var sawStream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if v, _ := req["stream"].(bool); v {
			sawStream.Store(true)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("sin flusher")
			return
		}
		writeSSE(w, flusher, "Usa ")
		writeSSE(w, flusher, "Object.assign [1].")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=always")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	if !sawStream.Load() {
		t.Error("la petición no pidió stream")
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("salida sin respuesta:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Fuentes (1)") {
		t.Errorf("salida sin fuentes:\n%s", stdout)
	}
}

func TestCLIAskJSONDoesNotStream(t *testing.T) {
	setupCLI(t)

	var sawStream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if v, _ := req["stream"].(bool); v {
			sawStream.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa Object.assign [1]."}}]}`)
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--json", "--stream=always")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	if sawStream.Load() {
		t.Error("--json no debe pedir stream")
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.Engine != "llm" || env.Answer == "" {
		t.Errorf("envelope = %+v", env)
	}
}

func TestCLIAskStreamNever(t *testing.T) {
	setupCLI(t)

	var sawStream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if v, _ := req["stream"].(bool); v {
			sawStream.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa Object.assign [1]."}}]}`)
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=never")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	if sawStream.Load() {
		t.Error("--stream=never no debe pedir stream")
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("salida sin respuesta:\n%s", stdout)
	}
}

func TestCLIAskAutoDoesNotStreamInPipe(t *testing.T) {
	setupCLI(t)

	var sawStream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if v, _ := req["stream"].(bool); v {
			sawStream.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa Object.assign [1]."}}]}`)
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	if sawStream.Load() {
		t.Error("--stream=auto no debe streamear cuando stdout no es un TTY")
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("salida sin respuesta:\n%s", stdout)
	}
}

func TestCLIAskStreamBriefAndSources(t *testing.T) {
	setupCLI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("sin flusher")
			return
		}
		writeSSE(w, flusher, "Usa Object.assign [1].")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=always", "--brief")
	if err != nil {
		t.Fatalf("brief: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("salida sin respuesta:\n%s", stdout)
	}
	if strings.Contains(stdout, "Fuentes") {
		t.Errorf("--brief no debe mostrar fuentes:\n%s", stdout)
	}

	stdout, stderr, err = runCLI(t, "ask", "js", "copiar un objeto", "--stream=always", "--brief", "--sources", "--refresh")
	if err != nil {
		t.Fatalf("sources: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "Fuentes") {
		t.Errorf("--brief no debe mostrar el encabezado de fuentes:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[1] Object.assign") {
		t.Errorf("--sources no mostró el fragmento:\n%s", stdout)
	}
}

func TestCLIAskCacheHitDoesNotCallLLM(t *testing.T) {
	setupCLI(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("sin flusher")
			return
		}
		writeSSE(w, flusher, "Usa Object.assign [1].")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	if _, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=always"); err != nil {
		t.Fatalf("primera llamada: %v\nstderr=%s", err, stderr)
	}
	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=always")
	if err != nil {
		t.Fatalf("segunda llamada: %v\nstderr=%s", err, stderr)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("llamadas al proveedor = %d, want 1", got)
	}
	if !strings.Contains(stdout, "Usa Object.assign [1].") {
		t.Errorf("la respuesta cacheada no se imprimió:\n%s", stdout)
	}
}

func TestCLIAskInvalidStreamFlag(t *testing.T) {
	setupCLI(t)

	_, _, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=siempre")
	if code := exitCode(err); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestCLIAskStreamInterrupted(t *testing.T) {
	setupCLI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1000")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("sin flusher")
			return
		}
		writeSSE(w, flusher, "Parcial")
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=always")
	if code := exitCode(err); code != 3 {
		t.Fatalf("exit = %d, want 3\nstderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "Parcial") {
		t.Errorf("no se conservó la respuesta parcial:\n%s", stdout)
	}
}

func TestCLIAskModelNotFoundFallsBack(t *testing.T) {
	setupCLI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"The model does not exist","code":"model_not_found"}}`)
	}))
	defer srv.Close()
	writeLLMConfig(t, srv.URL)

	stdout, stderr, err := runCLI(t, "ask", "js", "copiar un objeto", "--stream=never")
	if err != nil {
		t.Fatalf("err = %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"docty config set llm.model", "docty config unset llm.model"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr no contiene %q:\n%s", want, stderr)
		}
	}
	if !strings.Contains(stdout, "Resultados para") {
		t.Errorf("no cayó a los fragmentos:\n%s", stdout)
	}
}

func TestCLIAuthStatusWithoutProvider(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	stdout, _, err := runCLI(t, "auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no configurado") {
		t.Errorf("estado inesperado:\n%s", stdout)
	}
}

func TestCLIAuthLoginStatusLogout(t *testing.T) {
	setupCLI(t)
	store := useMemoryKeyring(t)
	const secret = "test-secret-key"

	stdout, stderr, err := runCLIInput(t, secret+"\n", "auth", "login", "--provider", "groq", "--no-validate")
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatalf("la API key se filtró en la salida:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "clave guardada") || !strings.Contains(stdout, "configuración actualizada") {
		t.Errorf("salida de login inesperada:\n%s", stdout)
	}
	got, err := store.Get(auth.Service, auth.Account("groq", ""))
	if err != nil || got != secret {
		t.Errorf("keyring = %q, %v", got, err)
	}

	data, err := os.ReadFile(os.Getenv("DOCTY_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "api_key") {
		t.Errorf("la config conserva api_key:\n%s", text)
	}
	if !strings.Contains(text, `provider = "groq"`) {
		t.Errorf("la config no tiene provider:\n%s", text)
	}

	stdout, stderr, err = runCLI(t, "auth", "status")
	if err != nil {
		t.Fatalf("status: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "keyring del sistema") || !strings.Contains(stdout, "groq") {
		t.Errorf("estado sin keyring:\n%s", stdout)
	}

	stdout, stderr, err = runCLI(t, "auth", "logout")
	if err != nil {
		t.Fatalf("logout: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "clave eliminada") {
		t.Errorf("salida de logout inesperada:\n%s", stdout)
	}
	if _, err := store.Get(auth.Service, auth.Account("groq", "")); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("la clave sigue en el keyring: %v", err)
	}

	stdout, _, err = runCLI(t, "auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ninguna") {
		t.Errorf("estado después de logout:\n%s", stdout)
	}
}

func TestCLIAuthLoginJSONWithEnvKey(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)
	t.Setenv("DOCTY_API_KEY", "env-secret")

	stdout, stderr, err := runCLI(t, "auth", "login", "--provider", "openrouter", "--no-validate", "--json")
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, "env-secret") {
		t.Fatalf("la API key se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	var login authLoginJSON
	if err := json.Unmarshal([]byte(stdout), &login); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if login.Provider != "openrouter" || login.KeySource != "keyring" || !login.EnvOverride {
		t.Errorf("login JSON = %+v", login)
	}
	if !strings.Contains(stderr, "sigue teniendo prioridad") {
		t.Errorf("falta el aviso de DOCTY_API_KEY:\n%s", stderr)
	}

	t.Setenv("DOCTY_API_KEY", "")
	stdout, stderr, err = runCLI(t, "auth", "status", "--json")
	if err != nil {
		t.Fatalf("status: %v\nstderr=%s", err, stderr)
	}
	var status authStatusJSON
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if status.Provider != "openrouter" || status.KeySource != "keyring" || !status.Configured {
		t.Errorf("status JSON = %+v", status)
	}
}

func TestCLIAuthLoginOpenAICompatible(t *testing.T) {
	setupCLI(t)
	store := useMemoryKeyring(t)
	const secret = "local-secret"
	const baseURL = "http://127.0.0.1:9999/v1"

	stdout, stderr, err := runCLIInput(t, secret+"\n",
		"auth", "login",
		"--provider", "openai-compatible",
		"--base-url", baseURL,
		"--model", "local-model",
		"--api-key-stdin",
		"--no-validate",
	)
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatalf("la API key se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "base URL: "+baseURL) {
		t.Errorf("falta la base URL en la salida:\n%s", stdout)
	}
	got, err := store.Get(auth.Service, auth.Account("openai-compatible", baseURL))
	if err != nil || got != secret {
		t.Errorf("keyring = %q, %v", got, err)
	}

	data, err := os.ReadFile(os.Getenv("DOCTY_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`provider = "openai-compatible"`,
		`base_url = "` + baseURL + `"`,
		`model = "local-model"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en la config:\n%s", want, text)
		}
	}

	stdout, _, err = runCLI(t, "auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, baseURL) || !strings.Contains(stdout, "keyring del sistema") {
		t.Errorf("estado inesperado:\n%s", stdout)
	}
}

func TestCLIAuthLoginKeyStdinOverridesEnv(t *testing.T) {
	setupCLI(t)
	store := useMemoryKeyring(t)
	t.Setenv("DOCTY_API_KEY", "env-key")

	stdout, stderr, err := runCLIInput(t, "stdin-key\n",
		"auth", "login",
		"--provider", "groq",
		"--api-key-stdin",
		"--no-validate",
	)
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, "stdin-key") || strings.Contains(stdout+stderr, "env-key") {
		t.Fatalf("la API key se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	got, err := store.Get(auth.Service, auth.Account("groq", ""))
	if err != nil || got != "stdin-key" {
		t.Errorf("keyring = %q, %v", got, err)
	}
	if !strings.Contains(stderr, "sigue teniendo prioridad") {
		t.Errorf("falta el aviso de DOCTY_API_KEY:\n%s", stderr)
	}
}

func TestCLIAuthLoginInvalidProvider(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	_, _, err := runCLIInput(t, "k\n", "auth", "login", "--provider", "nope", "--no-validate")
	if code := exitCode(err); code != 2 {
		t.Errorf("exit = %d, want 2 (%v)", code, err)
	}
}

func TestCLIAuthLoginMissingKey(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	_, _, err := runCLIInput(t, "", "auth", "login", "--provider", "groq", "--no-validate")
	if code := exitCode(err); code != 2 {
		t.Errorf("exit = %d, want 2 (%v)", code, err)
	}
}

func TestCLIAuthLoginRejectsUnauthorized(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("ruta = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bad" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, _, err := runCLIInput(t, "bad\n",
		"auth", "login",
		"--provider", "openai-compatible",
		"--base-url", srv.URL,
		"--model", "test-model",
	)
	if code := exitCode(err); code != 3 {
		t.Fatalf("exit = %d, want 3 (%v)", code, err)
	}
	if err == nil || !strings.Contains(err.Error(), "rechazó") {
		t.Errorf("error = %v", err)
	}
	if _, statErr := os.Stat(os.Getenv("DOCTY_CONFIG")); !os.IsNotExist(statErr) {
		t.Errorf("no se debería haber creado la config: %v", statErr)
	}
}

func TestCLIAuthLoginValidatesKey(t *testing.T) {
	setupCLI(t)
	store := useMemoryKeyring(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	stdout, stderr, err := runCLIInput(t, "good\n",
		"auth", "login",
		"--provider", "openai-compatible",
		"--base-url", srv.URL,
		"--model", "test-model",
	)
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "clave guardada") {
		t.Errorf("salida inesperada:\n%s", stdout)
	}
	got, err := store.Get(auth.Service, auth.Account("openai-compatible", srv.URL))
	if err != nil || got != "good" {
		t.Errorf("keyring = %q, %v", got, err)
	}
}

func TestCLIAuthStatusOnline(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer online-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	if _, stderr, err := runCLIInput(t, "online-key\n",
		"auth", "login",
		"--provider", "openai-compatible",
		"--base-url", srv.URL,
		"--model", "test-model",
		"--api-key-stdin",
		"--no-validate",
	); err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}

	stdout, stderr, err := runCLI(t, "auth", "status", "--online")
	if err != nil {
		t.Fatalf("status: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Conectividad ✓") {
		t.Errorf("falta la conectividad correcta:\n%s", stdout)
	}
}

func TestCLIAuthLoginCreatesConfigFlagPath(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	_, stderr, err := runCLIInput(t, "flag-key\n",
		"auth", "login",
		"--config", path,
		"--provider", "groq",
		"--no-validate",
	)
	if err != nil {
		t.Fatalf("login: %v\nstderr=%s", err, stderr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no se creó %s: %v", path, err)
	}
}

type failingKeyringStore struct{}

func (failingKeyringStore) Set(service, user, password string) error { return nil }
func (failingKeyringStore) Get(service, user string) (string, error) {
	return "", errors.New("dbus no disponible")
}
func (failingKeyringStore) Delete(service, user string) error { return nil }

func TestCLIAuthStatusReportsKeyringError(t *testing.T) {
	setupCLI(t)
	if err := os.WriteFile(os.Getenv("DOCTY_CONFIG"), []byte("[llm]\nprovider = \"groq\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := auth.SetStore(failingKeyringStore{})
	t.Cleanup(restore)

	stdout, _, err := runCLI(t, "auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no disponible") || !strings.Contains(stdout, "dbus no disponible") {
		t.Errorf("estado inesperado:\n%s", stdout)
	}
}

func TestCLIConfigShowGetSetUnset(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)
	path := os.Getenv("DOCTY_CONFIG")

	stdout, stderr, err := runCLI(t, "config")
	if err != nil {
		t.Fatalf("show: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Configuración efectiva") || !strings.Contains(stdout, "default_lang") {
		t.Errorf("salida inesperada:\n%s", stdout)
	}
	if !strings.Contains(stderr, "LLM no configurado") {
		t.Errorf("falta el aviso de LLM:\n%s", stderr)
	}

	stdout, stderr, err = runCLI(t, "config", "set", "llm.provider", "groq")
	if err != nil {
		t.Fatalf("set provider: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "llm.provider") || !strings.Contains(stdout, path) {
		t.Errorf("salida de set inesperada:\n%s", stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `provider = "groq"`) {
		t.Errorf("la config no tiene provider:\n%s", data)
	}

	stdout, stderr, err = runCLI(t, "config", "get", "llm.provider")
	if err != nil {
		t.Fatalf("get provider: %v\nstderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "groq" {
		t.Errorf("provider = %q", stdout)
	}
	stdout, _, err = runCLI(t, "config", "get", "llm.model")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "openai/gpt-oss-20b" {
		t.Errorf("modelo preset = %q", stdout)
	}
	stdout, _, err = runCLI(t, "config", "get", "llm.base_url")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "https://api.groq.com/openai/v1" {
		t.Errorf("base URL preset = %q", stdout)
	}

	if _, stderr, err := runCLI(t, "config", "set", "llm.model", "test-model"); err != nil {
		t.Fatalf("set model: %v\nstderr=%s", err, stderr)
	}
	stdout, stderr, err = runCLI(t, "config", "unset", "llm.model")
	if err != nil {
		t.Fatalf("unset model: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "eliminado") {
		t.Errorf("salida de unset inesperada:\n%s", stdout)
	}
	stdout, _, err = runCLI(t, "config", "unset", "llm.model")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "no estaba definido") {
		t.Errorf("unset repetido inesperado:\n%s", stdout)
	}

	stdout, _, err = runCLI(t, "config", "path")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != path {
		t.Errorf("path = %q, want %q", stdout, path)
	}
}

func TestCLIConfigDoesNotLeakAPIKey(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)
	const secret = "super-secret-key"
	t.Setenv("DOCTY_API_KEY", secret)

	stdout, stderr, err := runCLI(t, "config", "get", "llm.api_key")
	if err != nil {
		t.Fatalf("get api_key: %v\nstderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "env DOCTY_API_KEY" {
		t.Errorf("origen de api_key = %q", stdout)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatalf("la API key se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	stdout, stderr, err = runCLI(t, "config", "show")
	if err != nil {
		t.Fatalf("show: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatalf("la API key se filtró en show:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	stdout, stderr, err = runCLI(t, "config", "show", "--json")
	if err != nil {
		t.Fatalf("show json: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatalf("la API key se filtró en JSON:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	_, _, err = runCLI(t, "config", "set", "llm.api_key", secret)
	if code := exitCode(err); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	data, err := os.ReadFile(os.Getenv("DOCTY_CONFIG"))
	if err == nil && strings.Contains(string(data), secret) {
		t.Errorf("la API key se escribió en la config:\n%s", data)
	}
}

func TestCLIConfigKeySources(t *testing.T) {
	setupCLI(t)
	store := useMemoryKeyring(t)
	path := os.Getenv("DOCTY_CONFIG")
	const plainSecret = "plain-secret"
	const keyringSecret = "keyring-secret"
	if err := os.WriteFile(path, []byte("[llm]\nprovider = \"groq\"\napi_key = \"plain-secret\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, "config", "get", "llm.api_key")
	if err != nil {
		t.Fatalf("get api_key: %v\nstderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "config llm.api_key" {
		t.Errorf("origen = %q", stdout)
	}
	if strings.Contains(stdout+stderr, plainSecret) {
		t.Fatalf("la clave en texto plano se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	stdout, stderr, err = runCLI(t, "config", "show")
	if err != nil {
		t.Fatalf("show: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout+stderr, plainSecret) {
		t.Fatalf("la clave en texto plano se filtró en show:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "texto plano") {
		t.Errorf("falta el aviso de clave en texto plano:\n%s", stderr)
	}

	if _, _, err := runCLI(t, "config", "unset", "llm.api_key"); err != nil {
		t.Fatalf("unset api_key: %v", err)
	}
	if err := store.Set(auth.Service, auth.Account("groq", ""), keyringSecret); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runCLI(t, "config", "get", "llm.api_key")
	if err != nil {
		t.Fatalf("get keyring: %v\nstderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "keyring" {
		t.Errorf("origen = %q, want keyring", stdout)
	}
	if strings.Contains(stdout+stderr, keyringSecret) {
		t.Fatalf("la clave del keyring se filtró:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

func TestCLIConfigValidation(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	cases := [][]string{
		{"config", "set", "format", "xml"},
		{"config", "set", "pager", "always"},
		{"config", "set", "width", "120"},
		{"config", "set", "default_lang", "ts"},
		{"config", "set", "llm.base_url", "no-es-url"},
		{"config", "set", "llm.timeout", "0s"},
		{"config", "set", "llm.max_tokens", "0"},
		{"config", "set", "llm.temperature", "3"},
		{"config", "set", "llm.answer_lang", "fr"},
		{"config", "set", "llm.provider", "nope"},
	}
	for _, args := range cases {
		if _, _, err := runCLI(t, args...); exitCode(err) != 2 {
			t.Errorf("%v: exit = %d, want 2 (%v)", args, exitCode(err), err)
		}
	}
	if _, _, err := runCLI(t, "config", "get", "no.existe"); exitCode(err) != 2 {
		t.Errorf("get desconocido: exit = %d", exitCode(err))
	}
	if _, _, err := runCLI(t, "config", "unset", "no.existe"); exitCode(err) != 2 {
		t.Errorf("unset desconocido: exit = %d", exitCode(err))
	}
}

func TestCLIConfigProviderWarnings(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	_, stderr, err := runCLI(t, "config", "set", "llm.provider", "openai-compatible")
	if err != nil {
		t.Fatalf("set provider: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "llm.base_url") || !strings.Contains(stderr, "llm.model") {
		t.Errorf("faltan avisos de openai-compatible:\n%s", stderr)
	}

	_, stderr, err = runCLI(t, "config", "set", "llm.base_url", "http://127.0.0.1:1234/v1/")
	if err != nil {
		t.Fatalf("set base_url: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "llm.model") {
		t.Errorf("falta el aviso de model:\n%s", stderr)
	}
	data, err := os.ReadFile(os.Getenv("DOCTY_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `base_url = "http://127.0.0.1:1234/v1"`) {
		t.Errorf("base_url no se normalizó:\n%s", data)
	}

	_, stderr, err = runCLI(t, "config", "set", "llm.model", "local-model")
	if err != nil {
		t.Fatalf("set model: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stderr, "requiere") {
		t.Errorf("no debería avisar con la config completa:\n%s", stderr)
	}

	_, stderr, err = runCLI(t, "config", "set", "llm.provider", "groq")
	if err != nil {
		t.Fatalf("set groq: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "llm.base_url sigue configurado") || !strings.Contains(stderr, "llm.model sigue configurado") {
		t.Errorf("faltan los avisos de valores heredados:\n%s", stderr)
	}
}

func TestCLIConfigEnvOverrideWarning(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)
	t.Setenv("DOCTY_LLM_PROVIDER", "openrouter")

	_, stderr, err := runCLI(t, "config", "set", "llm.provider", "groq")
	if err != nil {
		t.Fatalf("set provider: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "tiene prioridad") || !strings.Contains(stderr, "DOCTY_LLM_PROVIDER") {
		t.Errorf("falta el aviso de prioridad:\n%s", stderr)
	}
	stdout, _, err := runCLI(t, "config", "get", "llm.provider")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "openrouter" {
		t.Errorf("valor efectivo = %q, want openrouter", stdout)
	}
}

func TestCLIConfigJSON(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)

	stdout, stderr, err := runCLI(t, "config", "show", "--json")
	if err != nil {
		t.Fatalf("show: %v\nstderr=%s", err, stderr)
	}
	var show configShowJSON
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if show.SchemaVersion != 1 || show.Config != os.Getenv("DOCTY_CONFIG") {
		t.Errorf("show = %+v", show)
	}
	if _, ok := show.Values["llm.api_key"]["value"]; ok {
		t.Error("api_key no debería incluir value")
	}
	if show.Values["llm.api_key"]["source"] != "none" {
		t.Errorf("api_key = %+v", show.Values["llm.api_key"])
	}

	stdout, stderr, err = runCLI(t, "config", "set", "llm.provider", "groq", "--json")
	if err != nil {
		t.Fatalf("set: %v\nstderr=%s", err, stderr)
	}
	var set configSetJSON
	if err := json.Unmarshal([]byte(stdout), &set); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if set.SchemaVersion != 1 || set.Key != "llm.provider" || set.Value != "groq" || !set.Changed {
		t.Errorf("set = %+v", set)
	}

	stdout, _, err = runCLI(t, "config", "get", "llm.model", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var get configGetJSON
	if err := json.Unmarshal([]byte(stdout), &get); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if get.Value != "openai/gpt-oss-20b" || get.Source != "preset" {
		t.Errorf("get = %+v", get)
	}

	stdout, _, err = runCLI(t, "config", "get", "llm.api_key", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal([]byte(stdout), &key); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if _, ok := key["value"]; ok {
		t.Error("api_key JSON no debería incluir value")
	}
	if key["source"] != "none" || key["set"] != false {
		t.Errorf("api_key JSON = %+v", key)
	}

	stdout, _, err = runCLI(t, "config", "unset", "llm.model", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var unset configUnsetJSON
	if err := json.Unmarshal([]byte(stdout), &unset); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if unset.Removed {
		t.Errorf("unset = %+v", unset)
	}

	stdout, _, err = runCLI(t, "config", "path", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var path configPathJSON
	if err := json.Unmarshal([]byte(stdout), &path); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if path.SchemaVersion != 1 || path.Config != os.Getenv("DOCTY_CONFIG") {
		t.Errorf("path = %+v", path)
	}
}

func TestCLIConfigFlagPath(t *testing.T) {
	setupCLI(t)
	useMemoryKeyring(t)
	path := filepath.Join(t.TempDir(), "sub", "config.toml")

	if _, stderr, err := runCLI(t, "config", "set", "llm.provider", "groq", "--config", path); err != nil {
		t.Fatalf("set: %v\nstderr=%s", err, stderr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no se creó %s: %v", path, err)
	}
	stdout, _, err := runCLI(t, "config", "path", "--config", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != path {
		t.Errorf("path = %q, want %q", stdout, path)
	}
}

func TestCLICacheStatsAndClear(t *testing.T) {
	setupCLI(t)
	cacheDir := os.Getenv("DOCTY_CACHE_DIR")
	answers := filepath.Join(cacheDir, "answers", "ab")
	if err := os.MkdirAll(answers, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"abcdef.json", "abcdef12.json"} {
		if err := os.WriteFile(filepath.Join(answers, name), []byte(`{"Text":"hola"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "llm-hint"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(cacheDir, "keep.txt")
	if err := os.WriteFile(keep, []byte("no tocar"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, "cache")
	if err != nil {
		t.Fatalf("cache: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "respuestas  2") || !strings.Contains(stdout, "ya mostrado") {
		t.Errorf("salida inesperada:\n%s", stdout)
	}

	stdout, _, err = runCLI(t, "cache", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var stats cacheStatsJSON
	if err := json.Unmarshal([]byte(stdout), &stats); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if stats.SchemaVersion != 1 || stats.Answers != 2 || stats.Bytes <= 0 || !stats.HintShown {
		t.Errorf("stats = %+v", stats)
	}
	if stats.Oldest == "" || stats.Newest == "" {
		t.Errorf("faltan fechas: %+v", stats)
	}

	stdout, stderr, err = runCLI(t, "cache", "clear")
	if err != nil {
		t.Fatalf("clear: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "2 respuestas") || !strings.Contains(stdout, "aviso del LLM reiniciado") {
		t.Errorf("salida de clear inesperada:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "answers")); !os.IsNotExist(err) {
		t.Errorf("answers debería haberse eliminado: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "llm-hint")); !os.IsNotExist(err) {
		t.Errorf("llm-hint debería haberse eliminado: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep.txt no debería tocarse: %v", err)
	}

	stdout, _, err = runCLI(t, "cache", "clear")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ya estaba vacía") {
		t.Errorf("segundo clear inesperado:\n%s", stdout)
	}
}

func TestCLICacheClearJSONAndPath(t *testing.T) {
	setupCLI(t)
	cacheDir := os.Getenv("DOCTY_CACHE_DIR")
	answers := filepath.Join(cacheDir, "answers", "cd")
	if err := os.MkdirAll(answers, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(answers, "cdef.json"), []byte(`{"Text":"hola"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "llm-hint"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, "cache", "clear", "--json")
	if err != nil {
		t.Fatalf("clear: %v\nstderr=%s", err, stderr)
	}
	var clear cacheClearJSON
	if err := json.Unmarshal([]byte(stdout), &clear); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if clear.SchemaVersion != 1 || clear.AnswersRemoved != 1 || clear.BytesFreed <= 0 || !clear.HintRemoved {
		t.Errorf("clear = %+v", clear)
	}

	stdout, _, err = runCLI(t, "cache", "path", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var path cachePathJSON
	if err := json.Unmarshal([]byte(stdout), &path); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if path.SchemaVersion != 1 || path.Path != cacheDir {
		t.Errorf("path = %+v", path)
	}
}

func TestCLIAskJSONWithoutLLM(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "ask", "js", "copiar un objeto", "--json", "--no-llm")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.SchemaVersion != 1 || env.Mode != "ask" || env.Engine != "retrieval" {
		t.Errorf("envelope = %+v", env)
	}
	if len(env.Results) == 0 {
		t.Error("faltan resultados de retrieval")
	}
	if len(env.Warnings) == 0 {
		t.Error("faltan avisos en el envelope")
	}
}

func TestCLIJSONUsageError(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "get", "js", "--json")
	if exitCode(err) != 2 {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
	var env jsonErrorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.SchemaVersion != 1 || env.Error.Code != "usage" || env.Error.ExitCode != 2 {
		t.Errorf("error = %+v", env)
	}
}

func TestCLIJSONNoResults(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "js", "qqzzxx", "--json")
	if exitCode(err) != 1 {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
	var env jsonErrorEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if env.Error.Code != "no_results" || env.Error.ExitCode != 1 {
		t.Errorf("error = %+v", env)
	}
}

func TestCLIListJSONEnvelope(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "list", "js", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got listJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if got.SchemaVersion != 1 || got.Lang != "js" || got.Count == 0 || len(got.Results) != got.Count {
		t.Errorf("list = %+v", got)
	}
}

func TestCLISourcesJSONEnvelope(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "sources", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got sourcesJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if got.SchemaVersion != 1 || len(got.Sources) == 0 || got.Sources[0].ID != "js" {
		t.Errorf("sources = %+v", got)
	}
}

func TestCLIIngestJSONEnvelope(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "ingest", "js", "--from", "../../testdata/mdn", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got ingestListJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if got.SchemaVersion != 1 || got.Lang != "js" || got.Count == 0 || len(got.Documents) != got.Count {
		t.Errorf("ingest = %+v", got)
	}
}

func TestCLIDoctorJSON(t *testing.T) {
	setupCLI(t)

	stdout, _, err := runCLI(t, "doctor", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got doctorReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, stdout)
	}
	if got.SchemaVersion != 1 || !got.Index.Exists || got.Index.Symbols == 0 {
		t.Errorf("doctor = %+v", got)
	}
	if got.Effective.Lang != "js" {
		t.Errorf("lang efectivo = %q", got.Effective.Lang)
	}
}

func TestCLIMarkdownPipe(t *testing.T) {
	setupCLI(t)
	t.Setenv("DOCTY_FORMAT", "md")

	stdout, _, err := runCLI(t, "js", "Array.prototype.map")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "# Array.prototype.map\n") {
		t.Errorf("falta el título Markdown:\n%s", stdout)
	}
	if !strings.Contains(stdout, "## ") {
		t.Errorf("faltan secciones Markdown:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Fuente: <https://developer.mozilla.org") {
		t.Errorf("falta la fuente:\n%s", stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Error("el modo Markdown no debe contener ANSI")
	}
}

func TestCLIListMarkdown(t *testing.T) {
	setupCLI(t)
	t.Setenv("DOCTY_FORMAT", "md")

	stdout, _, err := runCLI(t, "list", "js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "## js:") || !strings.Contains(stdout, "- **Array.prototype.map** — método") {
		t.Errorf("list Markdown inesperado:\n%s", stdout)
	}
}

func TestCLISourcesMarkdown(t *testing.T) {
	setupCLI(t)
	t.Setenv("DOCTY_FORMAT", "md")

	stdout, _, err := runCLI(t, "sources")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "## Fuentes\n") || !strings.Contains(stdout, "- **js** — JavaScript") {
		t.Errorf("sources Markdown inesperado:\n%s", stdout)
	}
}
