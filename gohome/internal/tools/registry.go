package tools

import (
	"sort"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Registry holds a named set of tools.
type Registry struct {
	m map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Tool)}
}

// Register adds t to the registry, keyed by t.Name().
func (r *Registry) Register(t Tool) {
	r.m[t.Name()] = t
}

// Get returns the tool with the given name, and whether it was found.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.m[name]
	return t, ok
}

// Names returns the sorted list of registered tool names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.m))
	for n := range r.m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Schemas returns a common.ToolDef for each registered tool, in sorted name order.
func (r *Registry) Schemas() []common.ToolDef {
	names := r.Names()
	defs := make([]common.ToolDef, 0, len(names))
	for _, n := range names {
		t := r.m[n]
		defs = append(defs, common.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}

// Without returns a new Registry that contains all tools except the named one.
// The original Registry is not modified.
func (r *Registry) Without(name string) *Registry {
	r2 := NewRegistry()
	for k, v := range r.m {
		if k != name {
			r2.m[k] = v
		}
	}
	return r2
}
