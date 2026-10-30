package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/luisdvss/docty/internal/index"
	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/mdrender"
	"github.com/luisdvss/docty/internal/search"
	"github.com/spf13/cobra"
)

func mdOptions() mdrender.Options {
	return mdrender.Options{Color: colorEnabled()}
}

func mdOptionsFor(baseURL string) mdrender.Options {
	opt := mdOptions()
	opt.BaseURL = baseURL
	return opt
}

// colorEnabled reports whether ANSI styling should be emitted. Color is only
// used for the human TTY renderer; Markdown and JSON stay clean.
func colorEnabled() bool {
	if flagNoColor {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return outputMode() == formatTTY && isTerminalWriter(os.Stdout)
}

func bold(s string) string {
	if !colorEnabled() {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

func kindLabel(kind string) string {
	switch kind {
	case "method":
		return "método"
	case "static-method":
		return "método estático"
	case "property":
		return "propiedad"
	case "static-property":
		return "propiedad estática"
	case "class":
		return "clase"
	case "function":
		return "función"
	case "object":
		return "objeto"
	case "constructor":
		return "constructor"
	case "operator":
		return "operador"
	case "statement":
		return "sentencia"
	case "guide":
		return "guía"
	default:
		return kind
	}
}

func simpleMode() bool { return flagSimple }

// renderDocument prints a full page, or a compact version with --simple.
func renderDocument(cmd *cobra.Command, doc *ingest.Document) error {
	if outputMode() == formatMarkdown {
		if simpleMode() {
			return renderDocumentSimpleMD(cmd, doc)
		}
		return renderDocumentMD(cmd, doc)
	}
	if simpleMode() {
		return renderDocumentSimple(cmd, doc)
	}

	var buf bytes.Buffer
	header := fmt.Sprintf("%s   ·   %s", doc.Symbol, kindLabel(doc.Kind))
	fmt.Fprintln(&buf, bold(header))
	fmt.Fprintln(&buf, divider(header))
	opt := mdOptionsFor(doc.URL)
	if doc.Summary != "" {
		fmt.Fprintln(&buf, mdrender.Render(doc.Summary, opt))
		fmt.Fprintln(&buf)
	}
	for i, s := range doc.Sections {
		if i == 0 && s.Heading == "Summary" {
			continue
		}
		fmt.Fprintf(&buf, "── %s ──\n", s.Heading)
		fmt.Fprintln(&buf, mdrender.Render(s.Body, opt))
		fmt.Fprintln(&buf)
	}
	fmt.Fprintf(&buf, "Fuente: %s\n", doc.URL)
	return writeOutput(cmd, buf.Bytes())
}

// renderDocumentSimple prints the essentials: summary, syntax and URL.
func renderDocumentSimple(cmd *cobra.Command, doc *ingest.Document) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s · %s\n", doc.Symbol, kindLabel(doc.Kind))
	if summary := simpleSummary(doc.Summary); summary != "" {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, summary)
	}
	if syntax := syntaxBlock(doc.Sections); syntax != "" {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, syntax)
	}
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, doc.URL)
	return writeOutput(cmd, buf.Bytes())
}

// renderDocumentMD prints a full page as plain Markdown, suitable for pipes.
func renderDocumentMD(cmd *cobra.Command, doc *ingest.Document) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", doc.Symbol)
	fmt.Fprintf(&b, "_%s_\n\n", kindLabel(doc.Kind))
	if doc.Summary != "" {
		b.WriteString(doc.Summary)
		b.WriteString("\n\n")
	}
	for i, s := range doc.Sections {
		if i == 0 && s.Heading == "Summary" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Heading, s.Body)
	}
	fmt.Fprintf(&b, "Fuente: <%s>\n", doc.URL)
	return writeOutput(cmd, []byte(b.String()))
}

func renderDocumentSimpleMD(cmd *cobra.Command, doc *ingest.Document) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", doc.Symbol)
	if summary := simpleSummary(doc.Summary); summary != "" {
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	if syntax := syntaxBlock(doc.Sections); syntax != "" {
		fmt.Fprintf(&b, "```js\n%s\n```\n\n", syntax)
	}
	b.WriteString(doc.URL)
	b.WriteString("\n")
	return writeOutput(cmd, []byte(b.String()))
}

func simpleSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if i := strings.Index(summary, "\n\n"); i >= 0 {
		summary = summary[:i]
	}
	summary = search.PlainSnippet(summary)
	if summary == "" {
		return ""
	}
	return truncateRunes(summary, 500)
}

func syntaxBlock(sections []ingest.Section) string {
	for _, s := range sections {
		if strings.EqualFold(strings.TrimSpace(s.Heading), "Syntax") {
			return firstFencedCode(s.Body)
		}
	}
	return ""
}

func firstFencedCode(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var code []string
	inFence := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				inFence = true
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			break
		}
		code = append(code, line)
	}
	return strings.TrimSpace(strings.Join(code, "\n"))
}

func simpleSnippet(s string) string {
	return oneLine(s, 140)
}

func divider(header string) string {
	n := utf8.RuneCountInString(header)
	if n > 60 {
		n = 60
	}
	if n < 20 {
		n = 20
	}
	return strings.Repeat("─", n)
}

// renderCandidates prints a disambiguation list.
func renderCandidates(cmd *cobra.Command, query, lang string, docs []index.Doc) {
	if outputMode() == formatMarkdown {
		renderCandidatesMD(cmd, query, lang, docs)
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Varios resultados para %q en %s:\n\n", query, lang)
	if simpleMode() {
		limit := len(docs)
		if limit > 6 {
			limit = 6
		}
		for _, d := range docs[:limit] {
			fmt.Fprintf(out, "  %s · %s\n", d.Symbol, kindLabel(d.Kind))
		}
	} else {
		for _, d := range docs {
			fmt.Fprintf(out, "  %-40s %s\n", d.Symbol, kindLabel(d.Kind))
			if s := oneLine(d.Summary, 80); s != "" {
				fmt.Fprintf(out, "  %-40s %s\n", "", s)
			}
		}
	}
	fmt.Fprintf(out, "\nSé más específico: docty %s %s\n", lang, candidateHint(docs))
}

func renderCandidatesMD(cmd *cobra.Command, query, lang string, docs []index.Doc) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "## Varios resultados para %q en %s\n\n", query, lang)
	limit := len(docs)
	if simpleMode() && limit > 6 {
		limit = 6
	}
	for _, d := range docs[:limit] {
		fmt.Fprintf(out, "- **%s** — %s\n", d.Symbol, kindLabel(d.Kind))
	}
	fmt.Fprintf(out, "\nSé más específico: `docty %s %s`\n", lang, candidateHint(docs))
}

func candidateHint(docs []index.Doc) string {
	if len(docs) == 0 {
		return ""
	}
	for _, d := range docs {
		if d.Kind == "method" || d.Kind == "static-method" {
			return d.Symbol
		}
	}
	return docs[0].Symbol
}

// renderHints prints symbols literally mentioned in the query.
func renderHints(cmd *cobra.Command, hints []index.Doc) {
	if len(hints) == 0 {
		return
	}
	if outputMode() == formatMarkdown {
		renderHintsMD(cmd, hints)
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Símbolos mencionados:")
	if simpleMode() {
		for _, d := range hints {
			fmt.Fprintf(out, "  %s · %s\n", d.Symbol, kindLabel(d.Kind))
		}
		fmt.Fprintln(out)
		return
	}
	for _, d := range hints {
		fmt.Fprintf(out, "  %-30s %s\n", d.Symbol, kindLabel(d.Kind))
		if s := oneLine(d.Summary, 100); s != "" {
			fmt.Fprintf(out, "  %-30s %s\n", "", s)
		}
	}
	fmt.Fprintln(out)
}

func renderHintsMD(cmd *cobra.Command, hints []index.Doc) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "## Símbolos mencionados")
	fmt.Fprintln(out)
	for _, d := range hints {
		fmt.Fprintf(out, "- **%s** — %s\n", d.Symbol, kindLabel(d.Kind))
	}
	fmt.Fprintln(out)
}

// renderMatches prints FTS results grouped by document.
func renderMatches(cmd *cobra.Command, query, lang string, matches []index.Match) {
	if outputMode() == formatMarkdown {
		renderMatchesMD(cmd, query, lang, matches)
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Resultados para %q en %s:\n\n", query, lang)

	maxDocs, maxSnippets := 6, 2
	if simpleMode() {
		maxDocs, maxSnippets = 5, 1
	}
	seen := map[int64]int{}
	docsShown := 0
	for _, m := range matches {
		n := seen[m.Doc.ID]
		if n >= maxSnippets {
			continue
		}
		if n == 0 {
			if docsShown == maxDocs {
				break
			}
			docsShown++
			if simpleMode() {
				fmt.Fprintf(out, "%s · %s\n", m.Doc.Symbol, kindLabel(m.Doc.Kind))
			} else {
				fmt.Fprintf(out, "%s   ·   %s\n", m.Doc.Symbol, kindLabel(m.Doc.Kind))
			}
		}
		snippet := m.Snippet
		if simpleMode() {
			snippet = simpleSnippet(snippet)
		}
		fmt.Fprintf(out, "  %s — %s\n", m.Heading, snippet)
		if n == 0 && !simpleMode() {
			fmt.Fprintln(out)
		}
		seen[m.Doc.ID] = n + 1
	}
}

func renderMatchesMD(cmd *cobra.Command, query, lang string, matches []index.Match) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "## Resultados para %q en %s\n\n", query, lang)

	maxDocs, maxSnippets := 6, 2
	if simpleMode() {
		maxDocs, maxSnippets = 5, 1
	}
	seen := map[int64]int{}
	docsShown := 0
	for _, m := range matches {
		n := seen[m.Doc.ID]
		if n >= maxSnippets {
			continue
		}
		if n == 0 {
			if docsShown == maxDocs {
				break
			}
			docsShown++
			fmt.Fprintf(out, "### %s (%s)\n\n", m.Doc.Symbol, kindLabel(m.Doc.Kind))
		}
		snippet := m.Snippet
		if simpleMode() {
			snippet = simpleSnippet(snippet)
		}
		fmt.Fprintf(out, "**%s** — %s\n\n", m.Heading, snippet)
		seen[m.Doc.ID] = n + 1
	}
}

func oneLine(s string, max int) string {
	s = search.PlainSnippet(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// writeJSONError is used by commands that want a machine-readable error while
// keeping the human message for stderr.
type jsonErrorBody struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code"`
}

type jsonErrorEnvelope struct {
	SchemaVersion int           `json:"schema_version"`
	Error         jsonErrorBody `json:"error"`
}

func writeJSONError(w io.Writer, code, message string, exitCode int) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(jsonErrorEnvelope{
		SchemaVersion: 1,
		Error:         jsonErrorBody{Code: code, Message: message, ExitCode: exitCode},
	})
}
