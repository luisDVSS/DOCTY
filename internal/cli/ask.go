package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luisdvss/docty/internal/config"
	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/llm"
	"github.com/luisdvss/docty/internal/mdrender"
	"github.com/luisdvss/docty/internal/search"
	"github.com/spf13/cobra"
)

func newAskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask [lang] <pregunta>",
		Short: "Responde en lenguaje natural usando la documentación indexada",
		Long: `Recupera los fragmentos más relevantes y pide a un modelo abierto
(Groq, OpenRouter o cualquier endpoint compatible) una respuesta citada.

Sin proveedor configurado devuelve los fragmentos y no falla.

Ejemplos:
  docty ask js "cómo copio un objeto sin mutarlo"
  docty ask "diferencias entre let y const" --sources
  docty ask js "qué hace structuredClone" --no-llm
  docty ask js "copiar un objeto" --simple`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			lang, rest := splitLang(cfg.DefaultLang, args)
			if len(rest) == 0 {
				return exitWith(2, "falta la pregunta: docty ask %s \"...\"", lang)
			}
			return runAsk(cmd, cfg, lang, strings.Join(rest, " "))
		},
	}
	return cmd
}

func runAsk(cmd *cobra.Command, cfg config.Config, lang, question string) error {
	ctx := context.Background()
	ix, err := openIndex(lang)
	if err != nil {
		return err
	}
	defer ix.Close()

	start := time.Now()
	matches, err := ix.Search(ctx, lang, question, 20)
	if err != nil {
		return exitWith(3, "%v", err)
	}
	if len(matches) == 0 {
		return noResultsForIndex(cmd, ix, lang, question)
	}

	handled, warnings, err := synthesize(cmd, cfg, lang, question, matches, start)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	if flagJSON {
		return outputJSONMode(cmd, question, lang, "ask", "retrieval",
			retrievalResults(symbolHints(ctx, ix, lang, question), matches),
			warnings, time.Since(start))
	}
	renderHints(cmd, symbolHints(ctx, ix, lang, question))
	renderMatches(cmd, question, lang, matches)
	return nil
}

// synthesize tries to answer with the configured provider. It returns
// handled=false when the caller should fall back to retrieval output, together
// with human-readable warnings for the JSON envelope.
func synthesize(cmd *cobra.Command, cfg config.Config, lang, question string, matches []index.Match, start time.Time) (bool, []string, error) {
	if flagNoLLM {
		return false, []string{"--no-llm: mostrando fragmentos"}, nil
	}

	settings, err := llm.Resolve(cfg.LLM)
	if err != nil {
		if flagRequireLLM {
			return true, nil, exitWith(4, "%v", err)
		}
		var warning string
		switch {
		case errors.Is(err, llm.ErrNotConfigured):
			showLLMHint(cmd)
			warning = "sin proveedor LLM configurado; mostrando fragmentos"
		case errors.Is(err, llm.ErrNoKey):
			warning = "falta la API key del proveedor; mostrando fragmentos"
			fmt.Fprintln(cmd.ErrOrStderr(), "aviso: falta la API key del proveedor (usa docty auth login); mostrando fragmentos")
		default:
			warning = fmt.Sprintf("%v; mostrando fragmentos", err)
			fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %v; mostrando fragmentos\n", err)
		}
		return false, []string{warning}, nil
	}

	chunks := buildChunks(matches, 6, 12000)
	if len(chunks) == 0 {
		return false, []string{"no hay fragmentos suficientes; mostrando resultados"}, nil
	}

	answerLang := resolveAnswerLang(cfg, question)
	cache := llm.NewCache(config.CacheDir())
	key := llm.Key(question, settings.Model+"@"+settings.BaseURL, answerLang, chunks)

	var (
		answer llm.Answer
		cached bool
	)
	if !flagRefresh {
		if a, ok := cache.Get(key); ok {
			answer, cached = a, true
		}
	}

	var warnings []string
	if !cached {
		client := llm.New(settings)
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		stream, err := shouldStream(cmd)
		if err != nil {
			return true, nil, err
		}
		if stream {
			out := cmd.OutOrStdout()
			cw := &countingWriter{w: out}
			var rw *mdrender.Writer
			var streamOut io.Writer = cw
			if outputMode() == formatTTY {
				rw = mdrender.NewWriter(cw, mdOptions())
				streamOut = rw
			}
			answer, err = client.AnswerStream(ctx, question, chunks, answerLang, streamOut)
			if err != nil {
				if cw.n == 0 && strings.TrimSpace(answer.Text) == "" {
					if flagRequireLLM {
						return true, nil, exitWith(4, "%v", err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no se pudo sintetizar (%v); mostrando fragmentos\n", err)
					return false, []string{fmt.Sprintf("no se pudo sintetizar (%v); mostrando fragmentos", err)}, nil
				}
				if rw != nil {
					_ = rw.Close()
				}
				fmt.Fprintln(out)
				return true, nil, exitWith(3, "la respuesta se interrumpió: %v", err)
			}
			if rw != nil {
				if cerr := rw.Close(); cerr != nil {
					return true, nil, exitWith(3, "escribiendo la respuesta: %v", cerr)
				}
			}
			_ = cache.Put(key, answer)
			if !answer.Grounded {
				warnings = append(warnings, "la respuesta no cita fuentes")
				fmt.Fprintln(cmd.ErrOrStderr(), "aviso: la respuesta no cita fuentes; revísala con --sources")
			}
			if cw.last != '\n' {
				fmt.Fprintln(out)
			}
			return true, warnings, renderAnswerTail(cmd, answer, chunks)
		}

		answer, err = client.Answer(ctx, question, chunks, answerLang)
		if err != nil {
			if flagRequireLLM {
				return true, nil, exitWith(4, "%v", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no se pudo sintetizar (%v); mostrando fragmentos\n", err)
			return false, []string{fmt.Sprintf("no se pudo sintetizar (%v); mostrando fragmentos", err)}, nil
		}
		_ = cache.Put(key, answer)
	}

	if !answer.Grounded {
		warnings = append(warnings, "la respuesta no cita fuentes")
		fmt.Fprintln(cmd.ErrOrStderr(), "aviso: la respuesta no cita fuentes; revísala con --sources")
	}
	if flagJSON {
		return true, warnings, outputJSONAnswer(cmd, question, lang, answer, chunks, cached, warnings, time.Since(start))
	}
	return true, warnings, renderAnswer(cmd, answer, chunks)
}

func buildChunks(matches []index.Match, maxChunks, maxChars int) []llm.Chunk {
	seen := map[string]bool{}
	var out []llm.Chunk
	total := 0

	for _, m := range matches {
		if len(out) == maxChunks || total >= maxChars {
			break
		}
		key := m.Doc.Symbol + "|" + m.Heading
		if seen[key] {
			continue
		}
		body := search.CleanForLLM(m.Body)
		if body == "" {
			continue
		}
		if len(strings.Fields(body)) < 25 && !strings.EqualFold(m.Heading, "Summary") {
			continue
		}
		if remaining := maxChars - total; len(body) > remaining {
			body = truncateRunes(body, remaining)
		}
		seen[key] = true
		total += len(body)
		out = append(out, llm.Chunk{
			N:       len(out) + 1,
			Symbol:  m.Doc.Symbol,
			Kind:    m.Doc.Kind,
			Heading: m.Heading,
			URL:     m.Doc.URL,
			Body:    body,
		})
	}
	return out
}

func resolveAnswerLang(cfg config.Config, question string) string {
	switch strings.ToLower(cfg.LLM.AnswerLang) {
	case "es", "en":
		return strings.ToLower(cfg.LLM.AnswerLang)
	case "auto", "":
		if search.IsSpanish(question) {
			return "es"
		}
		return "en"
	default:
		return "es"
	}
}

func renderAnswer(cmd *cobra.Command, answer llm.Answer, chunks []llm.Chunk) error {
	var buf bytes.Buffer
	if outputMode() == formatTTY {
		fmt.Fprintln(&buf, mdrender.Render(answer.Text, mdOptions()))
	} else {
		fmt.Fprintln(&buf, answer.Text)
	}
	buf.WriteString(sourcesText(answer, chunks))
	return writeOutput(cmd, buf.Bytes())
}

// renderAnswerTail prints only the sources after the answer has already been
// streamed to the terminal. It bypasses the pager on purpose.
func renderAnswerTail(cmd *cobra.Command, answer llm.Answer, chunks []llm.Chunk) error {
	_, err := io.WriteString(cmd.OutOrStdout(), sourcesText(answer, chunks))
	return err
}

func sourcesText(answer llm.Answer, chunks []llm.Chunk) string {
	var b strings.Builder
	md := outputMode() == formatMarkdown

	cited := answer.Citations
	if len(cited) == 0 && !simpleMode() {
		for _, c := range chunks {
			cited = append(cited, c.N)
		}
	}

	showCompact := !flagBrief && (!simpleMode() || !flagSources)
	if showCompact {
		switch {
		case md:
			fmt.Fprintf(&b, "\n## Fuentes (%d)\n\n", len(cited))
			for _, n := range cited {
				if n < 1 || n > len(chunks) {
					continue
				}
				c := chunks[n-1]
				fmt.Fprintf(&b, "- [%d] %s — %s\n", n, c.Symbol, c.URL)
			}
		case simpleMode():
			var lines []string
			for _, n := range cited {
				if n < 1 || n > len(chunks) {
					continue
				}
				c := chunks[n-1]
				lines = append(lines, fmt.Sprintf("  [%d] %s · %s\n", n, c.Symbol, c.URL))
			}
			if len(lines) > 0 {
				b.WriteString("\nFuentes\n")
				for _, line := range lines {
					b.WriteString(line)
				}
			}
		default:
			fmt.Fprintf(&b, "\nFuentes (%d)\n", len(cited))
			for _, n := range cited {
				if n < 1 || n > len(chunks) {
					continue
				}
				c := chunks[n-1]
				fmt.Fprintf(&b, "  %d. %s · %s\n", n, c.Symbol, c.URL)
			}
		}
	}

	if flagSources {
		b.WriteString("\n")
		for _, c := range chunks {
			if md {
				fmt.Fprintf(&b, "### [%d] %s — %s\n\n%s\n\n", c.N, c.Symbol, c.Heading, c.Body)
				continue
			}
			body := c.Body
			if outputMode() == formatTTY {
				body = mdrender.Render(body, mdOptionsFor(c.URL))
			}
			fmt.Fprintf(&b, "[%d] %s — %s\n%s\n\n", c.N, c.Symbol, c.Heading, body)
		}
	}
	return b.String()
}

type countingWriter struct {
	w    io.Writer
	n    int
	last byte
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	if n > 0 {
		c.last = p[n-1]
	}
	return n, err
}

// shouldStream decides whether a fresh LLM answer should be streamed. JSON
// output is never streamed because it must stay parseable.
func shouldStream(cmd *cobra.Command) (bool, error) {
	if flagJSON {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(flagStream)) {
	case "", "auto":
		return isTerminalWriter(cmd.OutOrStdout()), nil
	case "always", "on", "true":
		return true, nil
	case "never", "off", "false":
		return false, nil
	default:
		return false, exitWith(2, "valor inválido para --stream: %q (usa auto, always o never)", flagStream)
	}
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func validateStreamFlag() error {
	switch strings.ToLower(strings.TrimSpace(flagStream)) {
	case "", "auto", "always", "on", "true", "never", "off", "false":
		return nil
	default:
		return exitWith(2, "valor inválido para --stream: %q (usa auto, always o never)", flagStream)
	}
}

func showLLMHint(cmd *cobra.Command) {
	marker := filepath.Join(config.CacheDir(), "llm-hint")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "aviso: sin proveedor LLM configurado; mostrando fragmentos.")
	fmt.Fprintln(cmd.ErrOrStderr(), "       Actívalo en ~/.config/docty/config.toml (llm.provider, llm.model)")
	fmt.Fprintln(cmd.ErrOrStderr(), "       y exporta DOCTY_API_KEY. Groq y OpenRouter tienen tier gratuito.")
	_ = os.MkdirAll(config.CacheDir(), 0o755)
	_ = os.WriteFile(marker, []byte("1\n"), 0o600)
}

func outputJSONAnswer(cmd *cobra.Command, query, lang string, answer llm.Answer, chunks []llm.Chunk, cached bool, warnings []string, elapsed time.Duration) error {
	grounded := answer.Grounded
	env := jsonEnvelope{
		SchemaVersion: 1,
		Query:         query,
		Lang:          lang,
		Mode:          "ask",
		Engine:        "llm",
		Answer:        answer.Text,
		Grounded:      &grounded,
		Cached:        &cached,
		Results:       chunksToResults(chunks),
		Warnings:      warnings,
		TimingsMS:     map[string]int64{"llm": elapsed.Milliseconds(), "total": elapsed.Milliseconds()},
	}
	for _, n := range answer.Citations {
		if n >= 1 && n <= len(chunks) {
			env.Citations = append(env.Citations, jsonCitation{
				N:      n,
				Symbol: chunks[n-1].Symbol,
				URL:    chunks[n-1].URL,
			})
		}
	}
	return writeJSON(cmd, env)
}

func chunksToResults(chunks []llm.Chunk) []jsonResult {
	out := make([]jsonResult, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, jsonResult{
			Symbol: c.Symbol,
			Kind:   c.Kind,
			Title:  c.Heading,
			URL:    c.URL,
			Score:  1,
			Sections: []jsonSection{
				{Heading: c.Heading, HeadingPath: c.Symbol + " > " + c.Heading, Body: c.Body},
			},
		})
	}
	return out
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
