package mdrender

import (
	"io"
	"strings"
	"testing"
)

func plain(s string) string {
	return Render(s, Options{})
}

func TestRenderPlain(t *testing.T) {
	in := "# Título\n\nTexto **fuerte**, *cursiva* y `code`.\n\n- uno\n- dos\n\n> nota\n\n---\n"
	want := "Título\n\nTexto fuerte, cursiva y code.\n\n• uno\n• dos\n\n│ nota\n\n────────────────────────────────────────"
	if got := plain(in); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderCodeFence(t *testing.T) {
	in := "```js\nconst x = 1;\n```\n"
	want := "  ┌─ js\n  │ const x = 1;\n  └─"
	if got := plain(in); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderLink(t *testing.T) {
	in := "Ver [MDN](https://developer.mozilla.org/es/)."
	want := "Ver MDN (https://developer.mozilla.org/es/)."
	if got := plain(in); got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestRenderResolvesRelativeLinks(t *testing.T) {
	base := "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map"
	got := Render("Ver [guía](/en-US/docs/Web/JavaScript) y [aquí](#syntax).", Options{BaseURL: base})
	want := "Ver guía (https://developer.mozilla.org/en-US/docs/Web/JavaScript) y aquí (" + base + "#syntax)."
	if got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderDefinitionList(t *testing.T) {
	in := "- `callbackFn`\n  - : Una función."
	want := "• callbackFn\n    Una función."
	if got := plain(in); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderHeadingKeepsTrailingHash(t *testing.T) {
	if got := plain("# C#\n\n# Cerrado ##"); got != "C#\n\nCerrado" {
		t.Errorf("Render() = %q", got)
	}
}

func TestRenderSanitizesEscapes(t *testing.T) {
	got := plain("texto \x1b[31mrojo\x1b[0m y `\x1b[31m`")
	if strings.Contains(got, "\x1b") {
		t.Errorf("Render() contiene ESC: %q", got)
	}
}

func TestRenderKeepsIdentifierUnderscores(t *testing.T) {
	in := "Usa __proto__ y foo_bar_baz."
	if got := plain(in); got != in {
		t.Errorf("Render() = %q, want %q", got, in)
	}
}

func TestRenderOrderedListAndCheckbox(t *testing.T) {
	in := "1. uno\n2. dos\n\n- [x] hecho\n- [ ] pendiente"
	want := "1. uno\n2. dos\n\n☑ hecho\n☐ pendiente"
	if got := plain(in); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderUnclosedFence(t *testing.T) {
	in := "```\nabc"
	want := "  ┌─\n  │ abc\n  └─"
	if got := plain(in); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderColor(t *testing.T) {
	got := Render("# Hola\n\n**fuerte** y `code`", Options{Color: true})
	for _, want := range []string{ansiBold, ansiCyan, ansiBoldOff, ansiYellow, ansiFgOff} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() = %q, falta %q", got, want)
		}
	}
	for _, unwanted := range []string{"##", "**", "`"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("Render() = %q, no debería contener %q", got, unwanted)
		}
	}
}

func TestWriterColorMatchesRender(t *testing.T) {
	in := "## Título\n\n**negrita `code`** y [enlace](https://example.test)."
	var b strings.Builder
	w := NewWriter(&b, Options{Color: true})
	if _, err := io.WriteString(w, in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(b.String(), "\n")
	want := Render(in, Options{Color: true})
	if got != want {
		t.Errorf("stream =\n%q\nRender =\n%q", got, want)
	}
}

func TestWriterMatchesRender(t *testing.T) {
	in := "# Hola\n\nTexto **fuerte** y `code`.\n\n```js\nconst x = 1;\n```\n"
	var b strings.Builder
	w := NewWriter(&b, Options{})
	for _, part := range []string{"# Ho", "la\n\nTexto **fue", "rte** y `co", "de`.\n\n```js\nconst x = 1;\n``", "`\n"} {
		if _, err := io.WriteString(w, part); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(b.String(), "\n")
	want := Render(in, Options{})
	if got != want {
		t.Errorf("stream =\n%q\nRender =\n%q", got, want)
	}
}
