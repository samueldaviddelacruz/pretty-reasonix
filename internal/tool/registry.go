// Package tool holds the registry the model's tools are added to, and the canonical form of their
// schemas.
//
// The agent sees only the registry: it asks for schemas in registration order and gets the same bytes
// every time, which is what keeps a vendor's prefix cache intact across requests
// (docs/TECHNICAL_DESIGN.md §6.6).
package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/contract/tool"
	"pretty-reasonix/internal/tool/builtin"
)

var (
	// ErrNoName reports a tool without a name, which cannot be called.
	ErrNoName = errors.New("a tool needs a name")
	// ErrNoSchema reports a tool without a schema, which the model cannot be asked to fill in.
	ErrNoSchema = errors.New("a tool needs a schema")
	// ErrDuplicate reports a name that is already registered.
	ErrDuplicate = errors.New("a tool with that name is already registered")
)

// registered is one tool and the canonical form of its schema.
type registered struct {
	tool   tool.Tool
	schema json.RawMessage
}

// Registry is the set of tools one run may call.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]registered
	// order is registration order, and it is load-bearing: a map range would give the model its tools in
	// a different order on different runs, which changes the request bytes and discards the cache.
	order []string
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry { return &Registry{tools: make(map[string]registered)} }

// Add registers a tool, canonicalising its schema once.
//
// Once is the whole point. The schema is rendered into every request, so a tool whose schema was
// re-encoded per request — with its keys in whichever order the encoder happened to reach them — would
// change the request bytes and throw away the vendor's cached prefix on every call.
func (r *Registry) Add(t tool.Tool) error {
	if t == nil {
		return errors.New("a registry will not take a nil tool")
	}
	name := t.Name()
	if name == "" {
		return ErrNoName
	}
	if len(t.Schema()) == 0 {
		return fmt.Errorf("tool %q: %w", name, ErrNoSchema)
	}
	schema, err := canonicalSchema(t.Schema())
	if err != nil {
		return fmt.Errorf("tool %q: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.tools[name]; taken {
		return fmt.Errorf("tool %q: %w", name, ErrDuplicate)
	}
	r.tools[name] = registered{tool: t, schema: schema}
	r.order = append(r.order, name)
	return nil
}

// Schemas is the tools as the model sees them, in registration order.
func (r *Registry) Schemas() []provider.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]provider.ToolSchema, 0, len(r.order))
	for _, name := range r.order {
		entry := r.tools[name]
		schemas = append(schemas, provider.ToolSchema{
			Name:        entry.tool.Name(),
			Description: entry.tool.Description(),
			Parameters:  entry.schema,
		})
	}
	return schemas
}

// Names lists the registered tools in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.order)
}

// Lookup finds a tool by the name the model called.
func (r *Registry) Lookup(name string) (tool.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, found := r.tools[name]
	return entry.tool, found
}

// Builtins lists the tools compiled into this program, by name.
//
// A function rather than the registry's own contents: a session may add tools, but what is compiled in
// is a decision made at build time and a caller should not be able to answer that question differently
// depending on what it happens to have registered so far.
func Builtins() []string { return slices.Clone(builtin.Names()) }

// canonicalSchema re-encodes a schema so the same schema always renders the same bytes.
//
// Encoding/json sorts map keys, so decoding and re-encoding is enough — with UseNumber, because a
// float64 round trip would render 1000000 as 1e+06. That changes the number a strict validator sees, and
// a schema is exactly the place where a number's spelling is part of its meaning.
func canonicalSchema(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decoding the schema: %w", err)
	}
	// Anything after the first value would be silently dropped by re-encoding, so it is an error: a
	// schema with a trailing object is a mistake, not a superset.
	if decoder.More() {
		return nil, errors.New("the schema has trailing data")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding the schema: %w", err)
	}
	return canonical, nil
}
