package tools

import (
	"encoding/json"
	"testing"
)

func makeTestTool(name string) *fakeTool {
	return &fakeTool{
		name:        name,
		description: "desc for " + name,
		schema:      json.RawMessage(`{"type":"object"}`),
	}
}

func TestRegistry_GetFound(t *testing.T) {
	r := NewRegistry()
	ft := makeTestTool("fake")
	r.Register(ft)

	got, ok := r.Get("fake")
	if !ok {
		t.Fatal("expected to find 'fake'")
	}
	if got.Name() != "fake" {
		t.Errorf("got %q, want 'fake'", got.Name())
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nope")
	if ok {
		t.Fatal("expected ok==false for missing tool")
	}
}

func TestRegistry_Schemas(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestTool("alpha"))

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Name != "alpha" {
		t.Errorf("got name %q, want 'alpha'", schemas[0].Name)
	}
	if schemas[0].Description != "desc for alpha" {
		t.Errorf("got description %q", schemas[0].Description)
	}
}

func TestRegistry_NamesAreSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestTool("zebra"))
	r.Register(makeTestTool("alpha"))
	r.Register(makeTestTool("mango"))

	names := r.Names()
	expected := []string{"alpha", "mango", "zebra"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestRegistry_Without(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestTool("alpha"))
	r.Register(makeTestTool("beta"))

	r2 := r.Without("alpha")

	// r2 must not have alpha
	if _, ok := r2.Get("alpha"); ok {
		t.Error("Without should remove 'alpha' from copy")
	}
	// r2 must still have beta
	if _, ok := r2.Get("beta"); !ok {
		t.Error("Without should keep 'beta'")
	}
	// original must still have alpha (no mutation)
	if _, ok := r.Get("alpha"); !ok {
		t.Error("original registry must not be mutated by Without")
	}
}
