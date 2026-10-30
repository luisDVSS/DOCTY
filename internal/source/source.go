package source

import (
	_ "embed"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

//go:embed sources/js.yaml
var jsManifest []byte

// Manifest describes where a language's documentation comes from and which
// pages belong to it.
type Manifest struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Provider       string   `yaml:"provider"`
	Upstream       string   `yaml:"upstream"`
	License        string   `yaml:"license"`
	Select         []string `yaml:"select"`
	SnapshotCommit string   `yaml:"snapshot_commit"`
	PageTypes      []string `yaml:"page_types"`
}

var registry = map[string]Manifest{}

func init() {
	var m Manifest
	if err := yaml.Unmarshal(jsManifest, &m); err != nil {
		panic(fmt.Sprintf("manifest js inválido: %v", err))
	}
	registry[m.ID] = m
}

// All returns every known source, sorted by ID.
func All() []Manifest {
	out := make([]Manifest, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get looks a source up by ID.
func Get(id string) (Manifest, bool) {
	m, ok := registry[id]
	return m, ok
}

// Accepts reports whether a page-type belongs to the source.
func (m Manifest) Accepts(pageType string) bool {
	if len(m.PageTypes) == 0 {
		return true
	}
	for _, t := range m.PageTypes {
		if t == pageType {
			return true
		}
	}
	return false
}
