package search

import (
	"os"
	"strings"
	"testing"
)

func TestPlainSnippet(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			"The **`map()`** method of [Array](/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array) instances.",
			"The map() method of Array instances.",
		},
		{
			"```js\nconst x = 1;\n```",
			"const x = 1;",
		},
		{
			"For [deep cloning](/en-US/docs/Glossary/Deep_copy), use `structuredClone()`.",
			"For deep cloning, use structuredClone().",
		},
		{
			"A _source object_ and __proto__.",
			"A source object and __proto__.",
		},
		{
			"Use *this* and **that**.",
			"Use this and that.",
		},
		{
			"Code `a ** b ** c` stays intact.",
			"Code a ** b ** c stays intact.",
		},
		{
			"```js\nconst s = \"**bold**\";\n```",
			"const s = \"**bold**\";",
		},
		{
			"<!-- hidden -->Visible text.",
			"Visible text.",
		},
	}
	for _, tt := range tests {
		if got := PlainSnippet(tt.in); got != tt.want {
			t.Errorf("PlainSnippet(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanForLLMPreservesCodeAndNormalizesLinks(t *testing.T) {
	in := "See [deep cloning](/en-US/docs/Glossary/Deep_copy).\n\n" +
		"```js\nconst x = [1](2);\n```\n"
	got := CleanForLLM(in)
	if !strings.Contains(got, "https://developer.mozilla.org/en-US/docs/Glossary/Deep_copy") {
		t.Errorf("enlace relativo sin normalizar:\n%s", got)
	}
	if !strings.Contains(got, "```js\nconst x = [1](2);\n```") {
		t.Errorf("el bloque de código cambió:\n%s", got)
	}
}

func TestPlainSnippetRealFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/mdn/files/en-us/web/javascript/reference/global_objects/object/assign/index.md")
	if err != nil {
		t.Fatal(err)
	}
	got := PlainSnippet(string(data))
	if strings.Contains(got, "](/") {
		t.Errorf("quedaron enlaces Markdown: %q", got)
	}
	if strings.Contains(got, "```") || strings.Contains(got, "~~~") {
		t.Errorf("quedaron fences: %q", got)
	}
}
