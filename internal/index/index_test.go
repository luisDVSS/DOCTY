package index

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
)

func buildTestIndex(t *testing.T) (*Index, string) {
	t.Helper()

	docs, err := ingest.LoadDir("../../testdata/mdn")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("no se cargaron fixtures")
	}
	m, ok := source.Get("js")
	if !ok {
		t.Fatal("falta el manifest js")
	}

	path := filepath.Join(t.TempDir(), "index.db")
	if err := Build(context.Background(), path, m, "test", docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ix, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix, path
}

func TestBuildAndLookup(t *testing.T) {
	ix, _ := buildTestIndex(t)
	ctx := context.Background()

	doc, err := ix.Lookup(ctx, "js", "Array.prototype.map")
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil {
		t.Fatal("no encontró Array.prototype.map")
	}
	if doc.Kind != "method" {
		t.Errorf("kind = %q", doc.Kind)
	}
	if len(doc.Sections) == 0 {
		t.Error("documento sin secciones")
	}
	if !strings.Contains(doc.Summary, "creates") {
		t.Errorf("summary inesperado: %q", doc.Summary)
	}
}

func TestCandidatesResolvePrototypeAlias(t *testing.T) {
	ix, _ := buildTestIndex(t)

	for _, q := range []string{"array.map", "map"} {
		docs, err := ix.Candidates(context.Background(), "js", q, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) == 0 {
			t.Errorf("%q no devolvió candidatos", q)
			continue
		}
		if docs[0].Symbol != "Array.prototype.map" {
			t.Errorf("%q -> %q, want Array.prototype.map", q, docs[0].Symbol)
		}
	}
}

func TestSearch(t *testing.T) {
	ix, _ := buildTestIndex(t)

	matches, err := ix.Search(context.Background(), "js", "spread", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("sin resultados para spread")
	}
	if !strings.Contains(strings.ToLower(matches[0].Doc.Symbol), "spread") {
		t.Errorf("primer resultado = %q", matches[0].Doc.Symbol)
	}
}

func TestCountsAndSymbols(t *testing.T) {
	ix, _ := buildTestIndex(t)
	ctx := context.Background()

	docs, sections, err := ix.Counts(ctx, "js")
	if err != nil {
		t.Fatal(err)
	}
	if docs != 5 {
		t.Errorf("docs = %d, want 5", docs)
	}
	if sections == 0 {
		t.Error("sections = 0")
	}

	symbols, err := ix.Symbols(ctx, "js")
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != docs {
		t.Errorf("symbols = %d, docs = %d", len(symbols), docs)
	}
}

func TestBuildReplacesExistingIndex(t *testing.T) {
	ix, path := buildTestIndex(t)
	ix.Close()

	docs, err := ingest.LoadDir("../../testdata/mdn")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := source.Get("js")
	if err := Build(context.Background(), path, m, "test-2", docs); err != nil {
		t.Fatalf("segundo Build: %v", err)
	}

	ix2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix2.Close()
	info, ok, err := ix2.Source(context.Background(), "js")
	if err != nil || !ok {
		t.Fatalf("Source: ok=%v err=%v", ok, err)
	}
	if info.Version != "test-2" {
		t.Errorf("version = %q", info.Version)
	}
}

func TestSearchSpanish(t *testing.T) {
	ix, _ := buildTestIndex(t)

	matches, err := ix.Search(context.Background(), "js", "copiar un objeto", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("sin resultados para una consulta en español")
	}
	if matches[0].Doc.Symbol != "Object.assign" {
		t.Errorf("primer resultado = %q, se esperaba Object.assign", matches[0].Doc.Symbol)
	}
}

func TestBuildMatchQuery(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"array map", `"array" "map"*`},
		{"map", `"map"*`},
		{`foo" OR bar`, `"foo" "OR" "bar"*`},
		{"...", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := BuildMatchQuery(tt.in); got != tt.want {
			t.Errorf("BuildMatchQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
