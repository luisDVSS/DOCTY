package source

import (
	"reflect"
	"testing"
)

func TestSparsePaths(t *testing.T) {
	got := sparsePaths([]string{
		"files/en-us/web/javascript/**",
		"files/en-us/web/api/*",
		"docs/",
		"",
	})
	want := []string{
		"files/en-us/web/javascript",
		"files/en-us/web/api",
		"docs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sparsePaths = %v, want %v", got, want)
	}
}

func TestJSManifestIsPinned(t *testing.T) {
	m, ok := Get("js")
	if !ok {
		t.Fatal("falta el manifest js")
	}
	if m.SnapshotCommit == "" {
		t.Error("el manifest debería fijar snapshot_commit")
	}
	if m.Upstream == "" || len(m.Select) == 0 || len(m.PageTypes) == 0 {
		t.Errorf("manifest incompleto: %+v", m)
	}
}
