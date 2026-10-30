package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luisdvss/docty/internal/auth"
	"github.com/luisdvss/docty/internal/config"
)

func testSettings(url string) Settings {
	return Settings{
		Provider:  "openai-compatible",
		BaseURL:   url,
		Model:     "test-model",
		APIKey:    "test-key",
		Timeout:   5 * time.Second,
		MaxTokens: 100,
	}
}

func TestAnswerSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("mensajes = %d", len(req.Messages))
		}
		if !strings.Contains(req.Messages[1].Content, "[1] Array.prototype.map") {
			t.Errorf("contexto sin chunk: %q", req.Messages[1].Content)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa map() [1]."}}]}`)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	a, err := c.Answer(context.Background(), "¿qué hace map?", []Chunk{
		{N: 1, Symbol: "Array.prototype.map", Heading: "Summary", URL: "u", Body: "crea un array"},
	}, "es")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Grounded || len(a.Citations) != 1 || a.Citations[0] != 1 {
		t.Errorf("answer = %+v", a)
	}
	if a.Text != "Usa map() [1]." {
		t.Errorf("texto = %q", a.Text)
	}
}

func TestAnswerRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	_, err := c.Answer(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es")
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("err = %v, want ErrRateLimited", err)
	}
}

func TestAnswerUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	_, err := c.Answer(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestAnswerModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found"}}`)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	_, err := c.Answer(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
	for _, want := range []string{"test-model", "docty config set llm.model", "docty config unset llm.model"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want %q", err, want)
		}
	}
}

func TestProviderErrorModelNotFoundByCode(t *testing.T) {
	err := providerError(http.StatusBadRequest, []byte(`{"error":{"message":"unknown model","code":"model_not_found"}}`), "m")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestAnswerUngroundedWhenCitationIsInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"Inventado [9]."}}]}`)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	a, err := c.Answer(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es")
	if err != nil {
		t.Fatal(err)
	}
	if a.Grounded {
		t.Error("una cita fuera de rango no puede ser grounded")
	}
	if len(a.Citations) != 0 {
		t.Errorf("citations = %v", a.Citations)
	}
}

func TestResolvePresets(t *testing.T) {
	restore := auth.SetStore(auth.NewMemoryStore())
	defer restore()
	t.Setenv("DOCTY_API_KEY", "")

	s, err := Resolve(config.LLM{Provider: "groq", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://api.groq.com/openai/v1" || s.Model != "openai/gpt-oss-20b" {
		t.Errorf("settings = %+v", s)
	}

	s, err = Resolve(config.LLM{Provider: "openrouter", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://openrouter.ai/api/v1" || s.Model != "google/gemma-4-31b-it:free" {
		t.Errorf("settings = %+v", s)
	}

	if _, err := Resolve(config.LLM{Provider: "groq"}); !errors.Is(err, ErrNoKey) {
		t.Errorf("err = %v, want ErrNoKey", err)
	}

	if _, err := Resolve(config.LLM{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestResolveProviderDoesNotNeedKey(t *testing.T) {
	s, err := ResolveProvider(config.LLM{Provider: "groq"})
	if err != nil {
		t.Fatal(err)
	}
	if s.APIKey != "" || s.Model == "" || s.BaseURL == "" {
		t.Errorf("settings = %+v", s)
	}
}

func TestResolveKeyPrecedence(t *testing.T) {
	restore := auth.SetStore(auth.NewMemoryStore())
	defer restore()
	if err := auth.SetKey("groq", "", "from-keyring"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOCTY_API_KEY", "from-env")
	key, source, err := ResolveKey(config.LLM{Provider: "groq", APIKey: "from-config"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-env" || source != KeySourceEnv {
		t.Errorf("env: key=%q source=%q", key, source)
	}

	t.Setenv("DOCTY_API_KEY", "")
	key, source, err = ResolveKey(config.LLM{Provider: "groq", APIKey: "from-config"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-config" || source != KeySourceConfig {
		t.Errorf("config: key=%q source=%q", key, source)
	}

	key, source, err = ResolveKey(config.LLM{Provider: "groq"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-keyring" || source != KeySourceKeyring {
		t.Errorf("keyring: key=%q source=%q", key, source)
	}

	key, source, err = ResolveKey(config.LLM{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || source != KeySourceNone {
		t.Errorf("sin clave: key=%q source=%q", key, source)
	}
}

func TestResolveUsesKeyring(t *testing.T) {
	restore := auth.SetStore(auth.NewMemoryStore())
	defer restore()
	t.Setenv("DOCTY_API_KEY", "")
	if err := auth.SetKey("groq", "", "from-keyring"); err != nil {
		t.Fatal(err)
	}

	s, err := Resolve(config.LLM{Provider: "groq"})
	if err != nil {
		t.Fatal(err)
	}
	if s.APIKey != "from-keyring" {
		t.Errorf("APIKey = %q", s.APIKey)
	}
}

type failingKeyring struct{}

func (failingKeyring) Set(service, user, password string) error { return nil }
func (failingKeyring) Get(service, user string) (string, error) {
	return "", errors.New("dbus no disponible")
}
func (failingKeyring) Delete(service, user string) error { return nil }

func TestResolveOpenAICompatibleIgnoresKeyringError(t *testing.T) {
	restore := auth.SetStore(failingKeyring{})
	defer restore()
	t.Setenv("DOCTY_API_KEY", "")

	s, err := Resolve(config.LLM{
		Provider: "openai-compatible",
		BaseURL:  "http://127.0.0.1:1234/v1",
		Model:    "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.APIKey != "" {
		t.Errorf("APIKey = %q", s.APIKey)
	}
}

func TestResolvePresetSurfacesKeyringError(t *testing.T) {
	restore := auth.SetStore(failingKeyring{})
	defer restore()
	t.Setenv("DOCTY_API_KEY", "")

	_, err := Resolve(config.LLM{Provider: "groq"})
	if err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Errorf("err = %v", err)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	c := NewCache(t.TempDir())
	chunks := []Chunk{{N: 1, Symbol: "A", Heading: "H", URL: "u", Body: "b"}}
	key := Key("q", "m", "es", chunks)

	if _, ok := c.Get(key); ok {
		t.Fatal("caché vacía devolvió un valor")
	}
	a := Answer{Text: "hola [1]", Grounded: true, Citations: []int{1}, Model: "m"}
	if err := c.Put(key, a); err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get(key)
	if !ok || got.Text != a.Text || !got.Grounded {
		t.Errorf("got = %+v, ok=%v", got, ok)
	}

	if Key("q2", "m", "es", chunks) == key {
		t.Error("la clave no cambia con la pregunta")
	}
	if Key("q", "m", "en", chunks) == key {
		t.Error("la clave no cambia con el idioma")
	}
}

func TestCacheKeyIncludesChunkContent(t *testing.T) {
	base := []Chunk{{N: 1, Symbol: "A", Kind: "method", Heading: "H", URL: "u", Body: "b"}}
	key := Key("q", "m", "es", base)

	cases := []struct {
		name   string
		chunks []Chunk
	}{
		{"body", []Chunk{{N: 1, Symbol: "A", Kind: "method", Heading: "H", URL: "u", Body: "b2"}}},
		{"kind", []Chunk{{N: 1, Symbol: "A", Kind: "property", Heading: "H", URL: "u", Body: "b"}}},
		{"number", []Chunk{{N: 2, Symbol: "A", Kind: "method", Heading: "H", URL: "u", Body: "b"}}},
		{"symbol", []Chunk{{N: 1, Symbol: "B", Kind: "method", Heading: "H", URL: "u", Body: "b"}}},
	}
	for _, tc := range cases {
		if got := Key("q", "m", "es", tc.chunks); got == key {
			t.Errorf("la clave no cambia con %s", tc.name)
		}
	}
	if got := Key("q", "m", "es", []Chunk{{N: 1, Symbol: "A", Kind: "method", Heading: "H", URL: "u", Body: "b"}}); got != key {
		t.Error("la misma entrada debería producir la misma clave")
	}
}

func TestCacheStatsAndClear(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	for _, q := range []string{"q1", "q2"} {
		chunks := []Chunk{{N: 1, Symbol: "A", Heading: "H", URL: "u", Body: "b"}}
		if err := c.Put(Key(q, "m", "es", chunks), Answer{Text: "hola [1]", Grounded: true}); err != nil {
			t.Fatal(err)
		}
	}

	st := c.Stats()
	if st.Answers != 2 || st.Bytes <= 0 || st.Oldest.IsZero() || st.Newest.IsZero() {
		t.Errorf("stats = %+v", st)
	}
	if st.HintShown {
		t.Error("el aviso no debería estar marcado todavía")
	}

	hint := filepath.Join(dir, "llm-hint")
	if err := os.WriteFile(hint, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := c.Stats(); !st.HintShown {
		t.Error("Stats no detectó el marcador del aviso")
	}

	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("no tocar"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := c.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if result.Answers != 2 || result.Bytes <= 0 || !result.HintRemoved {
		t.Errorf("resultado = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "answers")); !os.IsNotExist(err) {
		t.Errorf("answers debería haberse eliminado: %v", err)
	}
	if _, err := os.Stat(hint); !os.IsNotExist(err) {
		t.Errorf("llm-hint debería haberse eliminado: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep.txt no debería tocarse: %v", err)
	}

	result, err = c.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if result.Answers != 0 || result.Bytes != 0 || result.HintRemoved {
		t.Errorf("segundo clear = %+v", result)
	}
}

func TestAnswerStreamSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if !req.Stream {
			t.Error("la petición no pidió stream")
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("el ResponseWriter no implementa Flusher")
			return
		}
		io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hola \"}}]}\n\n")
		flusher.Flush()
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"mundo [1]\"}}]}\n\n")
		flusher.Flush()
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	var buf bytes.Buffer
	a, err := c.AnswerStream(context.Background(), "q", []Chunk{
		{N: 1, Symbol: "A", Heading: "Summary", URL: "u", Body: "x"},
	}, "es", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Hola mundo [1]" {
		t.Errorf("stream = %q", got)
	}
	if a.Text != "Hola mundo [1]" || !a.Grounded || len(a.Citations) != 1 || a.Citations[0] != 1 {
		t.Errorf("answer = %+v", a)
	}
}

func TestAnswerStreamJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Usa map() [1]."}}]}`)
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	var buf bytes.Buffer
	a, err := c.AnswerStream(context.Background(), "q", []Chunk{
		{N: 1, Symbol: "A", Heading: "Summary", URL: "u", Body: "x"},
	}, "es", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Usa map() [1]." {
		t.Errorf("stream = %q", got)
	}
	if a.Text != "Usa map() [1]." || !a.Grounded {
		t.Errorf("answer = %+v", a)
	}
}

func TestAnswerStreamProviderErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := New(testSettings(srv.URL))
			var buf bytes.Buffer
			_, err := c.AnswerStream(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es", &buf)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
			if buf.Len() != 0 {
				t.Errorf("se escribió %q antes de fallar", buf.String())
			}
		})
	}
}

func TestAnswerStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"error\":{\"message\":\"boom\"}}\n\n")
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	var buf bytes.Buffer
	_, err := c.AnswerStream(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es", &buf)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}

func TestAnswerStreamEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	var buf bytes.Buffer
	_, err := c.AnswerStream(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es", &buf)
	if err == nil || !strings.Contains(err.Error(), "vacía") {
		t.Errorf("err = %v", err)
	}
}

func TestAnswerStreamUngrounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Inventado [9].\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	var buf bytes.Buffer
	a, err := c.AnswerStream(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if a.Grounded || len(a.Citations) != 0 {
		t.Errorf("answer = %+v", a)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestAnswerStreamWriteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hola\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(testSettings(srv.URL))
	a, err := c.AnswerStream(context.Background(), "q", []Chunk{{N: 1, Body: "x"}}, "es", failingWriter{})
	if err == nil {
		t.Fatal("se esperaba un error de escritura")
	}
	if a.Text != "Hola" {
		t.Errorf("texto parcial = %q", a.Text)
	}
}

type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

func TestConsumeSSEMidStreamError(t *testing.T) {
	var got []string
	r := &failingReader{
		data: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hola\"}}]}\n\n"),
		err:  errors.New("boom"),
	}
	err := consumeSSE(r, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
	if len(got) != 1 || got[0] != "Hola" {
		t.Errorf("deltas = %v", got)
	}
}

func TestConsumeSSEIncomplete(t *testing.T) {
	var got []string
	r := strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"Hola\"}}]}\n\n")
	err := consumeSSE(r, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "antes de completarse") {
		t.Errorf("err = %v", err)
	}
	if len(got) != 1 || got[0] != "Hola" {
		t.Errorf("deltas = %v", got)
	}
}

func TestConsumeSSEFinishReasonWithoutDone(t *testing.T) {
	var got []string
	r := strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"Hola\"},\"finish_reason\":\"stop\"}]}\n\n")
	err := consumeSSE(r, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Hola" {
		t.Errorf("deltas = %v", got)
	}
}
