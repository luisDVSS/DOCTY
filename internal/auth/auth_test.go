package auth

import (
	"errors"
	"testing"
)

func TestAccount(t *testing.T) {
	tests := []struct {
		provider string
		baseURL  string
		want     string
	}{
		{"groq", "", "api-key:groq"},
		{"Groq", "", "api-key:groq"},
		{"openrouter", "https://ignored", "api-key:openrouter"},
		{"openai-compatible", "http://localhost:8080/v1/", "api-key:openai-compatible:http://localhost:8080/v1"},
		{"openai-compatible", "", "api-key:openai-compatible"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := Account(tt.provider, tt.baseURL); got != tt.want {
			t.Errorf("Account(%q, %q) = %q, want %q", tt.provider, tt.baseURL, got, tt.want)
		}
	}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	restore := SetStore(NewMemoryStore())
	defer restore()

	if _, err := GetKey("groq", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetKey vacío = %v, want ErrNotFound", err)
	}
	if err := SetKey("groq", "", "  secret  "); err != nil {
		t.Fatal(err)
	}
	key, err := GetKey("groq", "")
	if err != nil {
		t.Fatal(err)
	}
	if key != "secret" {
		t.Errorf("key = %q", key)
	}
	if err := DeleteKey("groq", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := GetKey("groq", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("después de borrar: %v", err)
	}
}

func TestOpenAICompatibleKeysAreSeparatedByBaseURL(t *testing.T) {
	restore := SetStore(NewMemoryStore())
	defer restore()

	if err := SetKey("openai-compatible", "http://one/v1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := SetKey("openai-compatible", "http://two/v1", "two"); err != nil {
		t.Fatal(err)
	}
	for base, want := range map[string]string{
		"http://one/v1": "one",
		"http://two/v1": "two",
	} {
		got, err := GetKey("openai-compatible", base)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("GetKey(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestSetKeyRejectsEmpty(t *testing.T) {
	restore := SetStore(NewMemoryStore())
	defer restore()

	if err := SetKey("", "", "key"); err == nil {
		t.Error("se esperaba error sin proveedor")
	}
	if err := SetKey("groq", "", "   "); err == nil {
		t.Error("se esperaba error con clave vacía")
	}
}
