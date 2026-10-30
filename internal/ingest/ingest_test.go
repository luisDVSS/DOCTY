package ingest

import (
	"strings"
	"testing"
)

func TestDeriveSymbol(t *testing.T) {
	tests := []struct {
		slug     string
		pageType string
		title    string
		want     string
	}{
		{"Web/JavaScript/Reference/Global_Objects/Array/map", "javascript-instance-method", "Array.prototype.map()", "Array.prototype.map"},
		{"Web/JavaScript/Reference/Global_Objects/Object/assign", "javascript-static-method", "Object.assign()", "Object.assign"},
		{"Web/JavaScript/Reference/Global_Objects/Array/length", "javascript-instance-data-property", "Array.prototype.length", "Array.prototype.length"},
		{"Web/JavaScript/Reference/Global_Objects/Array/Array", "javascript-constructor", "Array() constructor", "Array()"},
		{"Web/JavaScript/Reference/Global_Objects/Array", "javascript-object", "Array", "Array"},
		{"Web/JavaScript/Reference/Global_Objects/parseInt", "javascript-function", "parseInt()", "parseInt"},
		{"Web/JavaScript/Reference/Operators/Spread_syntax", "javascript-operator", "Spread syntax (...)", "Spread syntax"},
		{"Web/JavaScript/Reference/Statements/let", "javascript-statement", "let", "let"},
		{"Web/JavaScript/Reference/Global_Objects/TypedArray/length", "javascript-instance-accessor-property", "TypedArray.prototype.length", "TypedArray.prototype.length"},
		{"Web/JavaScript/Reference/Global_Objects/Array/from", "javascript-static-data-property", "Array.from()", "Array.from"},
		{"Web/JavaScript/Reference/Global_Objects/Array/Symbol.iterator", "javascript-instance-method", "Array.prototype[Symbol.iterator]()", "Array.prototype[Symbol.iterator]"},
		{"Web/JavaScript/Reference/Global_Objects/Array/Symbol.species", "javascript-static-accessor-property", "Array[Symbol.species]", "Array[Symbol.species]"},
		{"Web/JavaScript/Reference/Global_Objects/Temporal", "javascript-namespace", "Temporal", "Temporal"},
		{"Web/JavaScript/Reference/Strict_mode", "guide", "Strict mode", "Strict mode"},
		{"Web/JavaScript/Reference/Errors/Unexpected_type", "javascript-error", `TypeError: "x" is (not) "y"`, `TypeError: "x" is (not) "y"`},
	}

	for _, tt := range tests {
		got := DeriveSymbol(tt.slug, tt.pageType, tt.title)
		if got != tt.want {
			t.Errorf("DeriveSymbol(%q, %q) = %q, want %q", tt.slug, tt.pageType, got, tt.want)
		}
	}
}

func TestExpandMacros(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`the {{jsxref("Array")}} instance`, "the Array instance"},
		{`all {{jsxref("Object/propertyIsEnumerable", "enumerable", "", 1)}} props`, "all enumerable props"},
		{`{{Glossary("DOM")}} tree`, "DOM tree"},
		{`{{DOMxRef("Window.structuredClone", "structuredClone()")}}`, "structuredClone()"},
		{`{{InteractiveExample("JavaScript Demo")}}`, ""},
		{`text {{UnknownMacro("x")}} end`, "text  end"},
		{`{{Specifications}}`, ""},
	}

	for _, tt := range tests {
		if got := ExpandMacros(tt.in); got != tt.want {
			t.Errorf("ExpandMacros(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandMacrosReportsUnknown(t *testing.T) {
	out, unknown := ExpandMacrosReport(`a {{UnknownMacro("x")}} b {{Another}} c {{UnknownMacro("y")}}`)
	if out != "a  b  c" {
		t.Errorf("salida = %q", out)
	}
	if len(unknown) != 2 || unknown[0] != "UnknownMacro" || unknown[1] != "Another" {
		t.Errorf("macros desconocidas = %v", unknown)
	}
}

func TestParseMapPage(t *testing.T) {
	doc, err := LoadFile("../../testdata/mdn/files/en-us/web/javascript/reference/global_objects/array/map/index.md")
	if err != nil {
		t.Fatal(err)
	}

	if doc.Symbol != "Array.prototype.map" {
		t.Errorf("symbol = %q", doc.Symbol)
	}
	if doc.Kind != "method" {
		t.Errorf("kind = %q", doc.Kind)
	}
	if doc.URL != "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map" {
		t.Errorf("url = %q", doc.URL)
	}
	if !strings.Contains(doc.Summary, "creates") {
		t.Errorf("summary inesperado: %q", doc.Summary)
	}
	if strings.Contains(doc.Summary, "{{") {
		t.Errorf("quedaron macros sin expandir: %q", doc.Summary)
	}

	var headings []string
	var paths []string
	for _, s := range doc.Sections {
		headings = append(headings, s.Heading)
		paths = append(paths, s.HeadingPath)
		if s.Ord < 0 {
			t.Errorf("ord inválido en %q", s.Heading)
		}
	}
	for _, want := range []string{"Summary", "Syntax", "Parameters", "See also"} {
		if !contains(headings, want) {
			t.Errorf("falta la sección %q en %v", want, headings)
		}
	}
	if !containsSubstring(paths, "Examples > ") {
		t.Errorf("ningún breadcrumb conserva el heading padre Examples: %v", paths)
	}
	if doc.Sections[0].HeadingPath != "Array.prototype.map" {
		t.Errorf("heading_path del summary = %q", doc.Sections[0].HeadingPath)
	}
	if !strings.Contains(doc.Sections[1].HeadingPath, " > ") {
		t.Errorf("heading_path sin breadcrumb: %q", doc.Sections[1].HeadingPath)
	}
}

func TestParseReportsUnknownMacros(t *testing.T) {
	src := []byte("---\n" +
		"title: Widget\n" +
		"slug: Web/JavaScript/Reference/Global_Objects/Widget\n" +
		"page-type: javascript-class\n" +
		"---\n\n" +
		"Text {{Nope(\"x\")}} and {{Nope(\"y\")}}.\n")
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.UnknownMacros) != 1 || doc.UnknownMacros[0] != "Nope" {
		t.Errorf("unknown_macros = %v", doc.UnknownMacros)
	}
}

func TestSplitSectionsKeepsFencedCodeIntact(t *testing.T) {
	body := "Intro line.\n\n```js\n## this is not a heading\nconst x = 1;\n```\n\n## Real heading\n\nBody.\n"
	sections := SplitSections("X", body)

	if len(sections) != 2 {
		t.Fatalf("secciones = %d, want 2: %+v", len(sections), sections)
	}
	if !strings.Contains(sections[0].Body, "## this is not a heading") {
		t.Errorf("el heading dentro del fence se perdió: %q", sections[0].Body)
	}
	if sections[1].Heading != "Real heading" {
		t.Errorf("heading = %q", sections[1].Heading)
	}
}

func TestSplitSectionsIncludesLevel4(t *testing.T) {
	body := "## Parent\n\nparent body\n\n### Child\n\nchild body\n\n#### Grandchild\n\ngrandchild body\n"
	sections := SplitSections("X", body)

	if len(sections) != 3 {
		t.Fatalf("secciones = %d, want 3: %+v", len(sections), sections)
	}
	if sections[2].Heading != "Grandchild" {
		t.Errorf("heading = %q", sections[2].Heading)
	}
	if want := "X > Parent > Child > Grandchild"; sections[2].HeadingPath != want {
		t.Errorf("heading_path = %q, want %q", sections[2].HeadingPath, want)
	}
}

func TestSplitSectionsSkipsEmptySections(t *testing.T) {
	sections := SplitSections("X", "Intro.\n\n## Empty\n\n## Full\n\nContent.\n")
	if len(sections) != 2 {
		t.Fatalf("secciones = %d, want 2: %+v", len(sections), sections)
	}
	if sections[1].Heading != "Full" {
		t.Errorf("heading = %q", sections[1].Heading)
	}
}

func TestSuggestions(t *testing.T) {
	docs := []*Document{
		{Symbol: "Array.prototype.map"},
		{Symbol: "Array.prototype.flatMap"},
		{Symbol: "Object.assign"},
	}
	got := Suggestions(docs, "array.mapp", 2)
	if len(got) == 0 || got[0] != "Array.prototype.map" {
		t.Errorf("sugerencias = %v", got)
	}
}

func TestFindIsCaseInsensitive(t *testing.T) {
	docs := []*Document{{Symbol: "Array.prototype.map", Title: "Array.prototype.map()"}}
	if _, ok := Find(docs, "array.prototype.MAP"); !ok {
		t.Error("Find no encontró el símbolo sin distinguir mayúsculas")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsSubstring(list []string, want string) bool {
	for _, v := range list {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}
