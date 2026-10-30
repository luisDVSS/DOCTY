// Package llm talks to OpenAI-compatible chat completion endpoints. It is
// deliberately provider-agnostic: Groq, OpenRouter or any compatible server.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luisdvss/docty/internal/auth"
	"github.com/luisdvss/docty/internal/config"
)

const promptVersion = "v1"

var (
	ErrNotConfigured = errors.New("no hay proveedor LLM configurado")
	ErrNoKey         = errors.New("falta la API key del proveedor (usa docty auth login o DOCTY_API_KEY)")
	ErrRateLimited   = errors.New("el proveedor devolvió 429 (límite de peticiones)")
	ErrUnauthorized  = errors.New("el proveedor rechazó la API key")
	ErrModelNotFound = errors.New("el modelo configurado no existe o no está disponible")
)

// KeySource describes where the effective API key came from.
type KeySource string

const (
	KeySourceNone    KeySource = "none"
	KeySourceEnv     KeySource = "env"
	KeySourceConfig  KeySource = "config"
	KeySourceKeyring KeySource = "keyring"
)

type preset struct {
	baseURL string
	model   string
}

var presets = map[string]preset{
	"groq":       {baseURL: "https://api.groq.com/openai/v1", model: "openai/gpt-oss-20b"},
	"openrouter": {baseURL: "https://openrouter.ai/api/v1", model: "google/gemma-4-31b-it:free"},
}

// Settings is the resolved configuration of a provider.
type Settings struct {
	Provider    string
	BaseURL     string
	Model       string
	APIKey      string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
}

// ResolveProvider validates the config and fills in provider defaults without
// requiring an API key.
func ResolveProvider(c config.LLM) (Settings, error) {
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" || p == "none" {
		return Settings{}, ErrNotConfigured
	}

	pre, known := presets[p]
	if !known && p != "openai-compatible" {
		return Settings{}, fmt.Errorf("proveedor LLM desconocido %q (usa groq, openrouter u openai-compatible)", c.Provider)
	}

	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = pre.baseURL
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = pre.model
	}
	if base == "" || model == "" {
		return Settings{}, errors.New("openai-compatible requiere base_url y model")
	}

	timeout := 20 * time.Second
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err == nil {
			timeout = d
		}
	}
	maxTokens := c.MaxTokens
	if maxTokens == 0 {
		maxTokens = 600
	}

	return Settings{
		Provider:    p,
		BaseURL:     base,
		Model:       model,
		Timeout:     timeout,
		MaxTokens:   maxTokens,
		Temperature: c.Temperature,
	}, nil
}

// ResolveKey returns the effective API key and its origin. Precedence:
// DOCTY_API_KEY, llm.api_key, then the system keyring.
func ResolveKey(c config.LLM) (string, KeySource, error) {
	if v := strings.TrimSpace(os.Getenv("DOCTY_API_KEY")); v != "" {
		return v, KeySourceEnv, nil
	}
	if v := strings.TrimSpace(c.APIKey); v != "" {
		return v, KeySourceConfig, nil
	}

	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" || p == "none" {
		return "", KeySourceNone, nil
	}
	key, err := auth.GetKey(p, c.BaseURL)
	if err == nil {
		if key == "" {
			return "", KeySourceNone, nil
		}
		return key, KeySourceKeyring, nil
	}
	if errors.Is(err, auth.ErrNotFound) {
		return "", KeySourceNone, nil
	}
	return "", KeySourceNone, fmt.Errorf("no se pudo leer la clave del keyring: %w", err)
}

// Resolve validates the config, fills in provider defaults and resolves the
// API key.
func Resolve(c config.LLM) (Settings, error) {
	s, err := ResolveProvider(c)
	if err != nil {
		return Settings{}, err
	}
	key, _, err := ResolveKey(c)
	if err != nil && s.Provider != "openai-compatible" {
		return Settings{}, err
	}
	if key == "" && s.Provider != "openai-compatible" {
		return Settings{}, ErrNoKey
	}
	s.APIKey = key
	return s, nil
}

// Chunk is one piece of retrieved documentation sent as context.
type Chunk struct {
	N       int
	Symbol  string
	Kind    string
	Heading string
	URL     string
	Body    string
}

// Answer is the synthesized result.
type Answer struct {
	Text      string
	Grounded  bool
	Citations []int
	Model     string
}

// Client calls the provider.
type Client struct {
	settings Settings
	http     *http.Client
}

func New(s Settings) *Client {
	return &Client{settings: s, http: &http.Client{Timeout: s.Timeout}}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Answer asks the model to answer the question using only the chunks.
func (c *Client) Answer(ctx context.Context, question string, chunks []Chunk, answerLang string) (Answer, error) {
	if len(chunks) == 0 {
		return Answer{}, errors.New("no hay contexto para responder")
	}

	ctx, cancel := context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()

	resp, err := c.send(ctx, c.http, question, chunks, answerLang, false)
	if err != nil {
		return Answer{}, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Answer{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Answer{}, providerError(resp.StatusCode, payload, c.settings.Model)
	}

	text, err := parseChatResponse(payload)
	if err != nil {
		return Answer{}, err
	}
	citations, grounded := validateCitations(text, len(chunks))
	return Answer{
		Text:      text,
		Grounded:  grounded,
		Citations: citations,
		Model:     c.settings.Model,
	}, nil
}

// AnswerStream asks the model and writes each text delta to w as it arrives.
// Every character of Answer.Text is written exactly once, so callers must not
// print the returned text again. When the provider ignores the stream flag and
// returns a normal JSON completion, the whole answer is written in one call.
func (c *Client) AnswerStream(ctx context.Context, question string, chunks []Chunk, answerLang string, w io.Writer) (Answer, error) {
	if len(chunks) == 0 {
		return Answer{}, errors.New("no hay contexto para responder")
	}
	if w == nil {
		w = io.Discard
	}

	// http.Client.Timeout covers the whole request, including reading the
	// body. The regular timeout is too tight for a slow stream, so give
	// streaming a generous floor while still bounding a hung connection.
	timeout := c.settings.Timeout
	if timeout < 90*time.Second {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// No client-level timeout: ctx is the single source of truth.
	hc := &http.Client{}
	resp, err := c.send(ctx, hc, question, chunks, answerLang, true)
	if err != nil {
		return Answer{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return Answer{}, providerError(resp.StatusCode, payload, c.settings.Model)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		// Some compatible servers ignore "stream" and answer with JSON.
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return Answer{}, err
		}
		text, err := parseChatResponse(payload)
		if err != nil {
			return Answer{}, err
		}
		if _, err := io.WriteString(w, text); err != nil {
			return Answer{Text: text, Model: c.settings.Model}, err
		}
		citations, grounded := validateCitations(text, len(chunks))
		return Answer{
			Text:      text,
			Grounded:  grounded,
			Citations: citations,
			Model:     c.settings.Model,
		}, nil
	}

	var b strings.Builder
	err = consumeSSE(resp.Body, func(delta string) error {
		b.WriteString(delta)
		_, werr := io.WriteString(w, delta)
		return werr
	})
	text := strings.TrimSpace(b.String())
	if err != nil {
		return Answer{Text: text, Model: c.settings.Model}, err
	}
	if text == "" {
		return Answer{}, errors.New("el proveedor devolvió una respuesta vacía")
	}
	citations, grounded := validateCitations(text, len(chunks))
	return Answer{
		Text:      text,
		Grounded:  grounded,
		Citations: citations,
		Model:     c.settings.Model,
	}, nil
}

// send builds and executes a chat completion request.
func (c *Client) send(ctx context.Context, hc *http.Client, question string, chunks []Chunk, answerLang string, stream bool) (*http.Response, error) {
	body := chatRequest{
		Model:       c.settings.Model,
		Temperature: c.settings.Temperature,
		MaxTokens:   c.settings.MaxTokens,
		Stream:      stream,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt(answerLang)},
			{Role: "user", Content: userPrompt(question, chunks)},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.settings.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.settings.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.settings.APIKey)
	}
	if c.settings.Provider == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/luisdvss/docty")
		req.Header.Set("X-Title", "Docty")
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("conectando con %s: %w", c.settings.Provider, err)
	}
	return resp, nil
}

func providerError(status int, payload []byte, model string) error {
	if status == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrUnauthorized
	}
	var cr chatResponse
	_ = json.Unmarshal(payload, &cr)
	msg := strings.TrimSpace(string(payload))
	if cr.Error != nil && cr.Error.Message != "" {
		msg = cr.Error.Message
	}
	if status == http.StatusNotFound || (cr.Error != nil && cr.Error.Code == "model_not_found") {
		return fmt.Errorf(
			"%w: %q (HTTP %d). Revisa llm.model con `docty config set llm.model <modelo>` o vuelve al default con `docty config unset llm.model`; si el modelo es correcto, revisa llm.base_url. Detalle: %s",
			ErrModelNotFound, model, status, truncate(msg, 300),
		)
	}
	return fmt.Errorf("el proveedor devolvió HTTP %d: %s", status, truncate(msg, 300))
}

func parseChatResponse(payload []byte) (string, error) {
	var cr chatResponse
	if err := json.Unmarshal(payload, &cr); err != nil {
		return "", fmt.Errorf("respuesta inválida del proveedor: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("el proveedor devolvió un error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", errors.New("el proveedor devolvió una respuesta vacía")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// consumeSSE parses OpenAI-style server-sent events. Events are separated by
// blank lines; each event may span several "data:" lines. Comments and other
// SSE fields are ignored.
func consumeSSE(r io.Reader, onDelta func(string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	var data []string
	done := false
	sawFinish := false

	dispatch := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}
		var sr streamResponse
		if err := json.Unmarshal([]byte(payload), &sr); err != nil {
			return fmt.Errorf("respuesta SSE inválida: %w", err)
		}
		if sr.Error != nil && sr.Error.Message != "" {
			return fmt.Errorf("el proveedor devolvió un error: %s", sr.Error.Message)
		}
		for _, ch := range sr.Choices {
			if ch.FinishReason != "" {
				sawFinish = true
			}
			if ch.Delta.Content == "" {
				continue
			}
			if err := onDelta(ch.Delta.Content); err != nil {
				return err
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
			if done {
				return nil
			}
		case strings.HasPrefix(line, ":"):
			// SSE comment (OpenRouter sends ": OPENROUTER PROCESSING").
		case strings.HasPrefix(line, "data:"):
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			if v != "" {
				data = append(data, v)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !done {
		if err := dispatch(); err != nil {
			return err
		}
	}
	if !done && !sawFinish {
		return errors.New("el stream terminó antes de completarse")
	}
	return nil
}

func systemPrompt(answerLang string) string {
	lang := "español"
	if answerLang == "en" {
		lang = "inglés"
	}
	return fmt.Sprintf(`Eres un asistente de documentación técnica. Responde SOLO con la
información del CONTEXTO. Si el contexto no alcanza, dilo explícitamente y no
inventes APIs ni firmas.

Reglas:
- Responde en %s.
- Cita cada afirmación con [n] según el CONTEXTO.
- Los identificadores, firmas y títulos de API van en su forma original.
- Incluye como máximo un ejemplo de código, copiado del CONTEXTO.
- Sé conciso: máximo 150 palabras.`, lang)
}

func userPrompt(question string, chunks []Chunk) string {
	var b strings.Builder
	b.WriteString("CONTEXTO:\n\n")
	for _, ch := range chunks {
		fmt.Fprintf(&b, "[%d] %s — %s\n%s\n\n", ch.N, ch.Symbol, ch.Heading, truncate(ch.Body, 1800))
	}
	b.WriteString("PREGUNTA: ")
	b.WriteString(question)
	return b.String()
}

var citationRE = regexp.MustCompile(`\[(\d+)\]`)

func validateCitations(text string, chunkCount int) ([]int, bool) {
	seen := map[int]bool{}
	var out []int
	valid := true
	for _, m := range citationRE.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > chunkCount {
			valid = false
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, valid
}

func truncate(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
