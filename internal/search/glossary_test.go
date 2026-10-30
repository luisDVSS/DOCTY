package search

import (
	"strings"
	"testing"
)

func TestExpandQuerySpanish(t *testing.T) {
	expr, expanded := ExpandQuery("copiar un objeto")
	if !expanded {
		t.Fatal("se esperaba expansión")
	}
	for _, want := range []string{"object", "copy", "clone", " AND "} {
		if !strings.Contains(expr, want) {
			t.Errorf("expresión %q no contiene %q", expr, want)
		}
	}
	if strings.Contains(expr, `"un"`) {
		t.Errorf("la stopword 'un' no debería aparecer: %q", expr)
	}
}

func TestExpandQueryLooseUsesOr(t *testing.T) {
	expr, _ := ExpandQueryLoose("copiar un objeto")
	if !strings.Contains(expr, " OR ") {
		t.Errorf("expresión laxa sin OR: %q", expr)
	}
}

func TestExpandQueryAccents(t *testing.T) {
	expr, expanded := ExpandQuery("cómo declaro una función")
	if !expanded {
		t.Fatal("se esperaba expansión")
	}
	if !strings.Contains(expr, "function") {
		t.Errorf("no tradujo función: %q", expr)
	}
}

func TestExpandQueryPlural(t *testing.T) {
	expr, expanded := ExpandQuery("objetos")
	if !expanded || !strings.Contains(expr, "object") {
		t.Errorf("plural no resuelto: %q (expanded=%v)", expr, expanded)
	}
}

func TestExpandQueryEnglishPassesThrough(t *testing.T) {
	_, expanded := ExpandQuery("array map")
	if expanded {
		t.Error("una consulta inglesa no debería marcar expansión")
	}
}

func TestExpandQueryQuotesAreEscaped(t *testing.T) {
	expr, _ := ExpandQuery(`objeto" OR "x`)
	if strings.Contains(expr, `" OR "`) && strings.Count(expr, `"`)%2 != 0 {
		t.Errorf("comillas sin escapar: %q", expr)
	}
}

func TestIsSpanish(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"cómo copio un objeto", true},
		{"cómo usar Array.prototype.map", true},
		{"como usar Array.prototype.map", true},
		{"copiar un objeto", true},
		{"qué es una promesa", true},
		{"diferencias entre let y const", true},
		{"la diferencia entre map y filter", true},
		{"como usar map en un array", true},
		{"mapa", true},
		{"mapa de array", true},
		{"el error", true},
		{"array map", false},
		{"a map", false},
		{"copy an object", false},
		{"how do I copy an object", false},
		{"what is the difference between let and const", false},
		{"error handling", false},
		{"variable scope", false},
		{"function map", false},
	}
	for _, tt := range tests {
		if got := IsSpanish(tt.query); got != tt.want {
			t.Errorf("IsSpanish(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}
