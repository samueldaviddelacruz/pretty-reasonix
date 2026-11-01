package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/tool"
	registry "pretty-reasonix/internal/tool"
)

// fakeTool is the smallest thing that satisfies the interface, so a test can vary one property at a time.
type fakeTool struct {
	name        string
	description string
	schema      string
}

func (f fakeTool) Name() string            { return f.name }
func (f fakeTool) Description() string     { return f.description }
func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(f.schema) }
func (fakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Output: "ok"}, nil
}

// TestASchemaIsCanonicalisedOnce is the criterion's first half: the same schema renders the same bytes,
// whatever order and spacing it arrived in. A schema that re-encoded differently per request would
// discard the vendor's cached prefix on every call.
func TestASchemaIsCanonicalisedOnce(t *testing.T) {
	t.Parallel()
	first := registry.NewRegistry()
	if err := first.Add(fakeTool{name: "read", schema: `{"type":"object","properties":{"path":{"type":"string"}}}`}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	second := registry.NewRegistry()
	if err := second.Add(fakeTool{name: "read", schema: "{\n  \"properties\": {\"path\": {\"type\": \"string\"}},\n\t\"type\": \"object\"\n}"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := string(second.Schemas()[0].Parameters)
	want := string(first.Schemas()[0].Parameters)
	if got != want {
		t.Errorf("the same schema in a different order canonicalised differently:\n %s\n %s", got, want)
	}
}

// TestNumberSpellingSurvivesCanonicalisation: a float64 round trip would render 1000000 as 1e+06, which
// a strict validator reads as a different number.
func TestNumberSpellingSurvivesCanonicalisation(t *testing.T) {
	t.Parallel()
	r := registry.NewRegistry()
	if err := r.Add(fakeTool{name: "big", schema: `{"maxLength":1000000}`}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := string(r.Schemas()[0].Parameters); !strings.Contains(got, "1000000") {
		t.Errorf("the number was rewritten by canonicalisation: %s", got)
	}
}

// TestSchemasAreInRegistrationOrder pins the property a map range would break: the same registry must
// hand the model its tools in the same order every time.
func TestSchemasAreInRegistrationOrder(t *testing.T) {
	t.Parallel()
	r := registry.NewRegistry()
	for _, name := range []string{"zebra", "apple", "mango"} {
		if err := r.Add(fakeTool{name: name, schema: `{"type":"object"}`}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	schemas := r.Schemas()
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Name)
	}
	if strings.Join(names, ",") != "zebra,apple,mango" {
		t.Errorf("schemas came back as %v, want registration order", names)
	}
}

func TestAToolThatCannotBeCalledIsRefused(t *testing.T) {
	t.Parallel()
	r := registry.NewRegistry()
	cases := map[string]struct {
		t      tool.Tool
		reason error
	}{
		"no name":       {fakeTool{schema: `{}`}, registry.ErrNoName},
		"no schema":     {fakeTool{name: "x"}, registry.ErrNoSchema},
		"a duplicate":   {fakeTool{name: "read", schema: `{}`}, registry.ErrDuplicate},
		"trailing data": {fakeTool{name: "y", schema: `{} {}`}, nil},
	}
	// Seed the duplicate's first registration.
	if err := r.Add(fakeTool{name: "read", schema: `{}`}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for name, tc := range cases {
		err := r.Add(tc.t)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if tc.reason != nil && !errors.Is(err, tc.reason) {
			t.Errorf("%s was refused for the wrong reason: %v", name, err)
		}
	}
}

// TestTheAgentSeesOnlyTheRegistry is the criterion's last clause: what the model is told about tools comes
// from here and nowhere else, which is what stops two frontends offering different sets.
func TestTheAgentSeesOnlyTheRegistry(t *testing.T) {
	t.Parallel()
	r := registry.NewRegistry()
	if err := r.Add(fakeTool{name: "read", description: "Read a file.", schema: `{"type":"object"}`}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("%d schemas, want 1", len(schemas))
	}
	if schemas[0].Name != "read" || schemas[0].Description != "Read a file." {
		t.Errorf("the schema is %+v", schemas[0])
	}
	if _, found := r.Lookup("read"); !found {
		t.Error("a registered tool cannot be looked up, so the loop could not run it")
	}
	if _, found := r.Lookup("absent"); found {
		t.Error("an unregistered name resolved")
	}
	if got := strings.Join(r.Names(), ","); got != "read" {
		t.Errorf("Names is %q", got)
	}
}

// TestBuiltinsIsTheCompiledSet: empty today, because no built-in exists yet. What is pinned is that it is
// a copy — a caller mutating the result must not change what the next caller is told.
func TestBuiltinsIsTheCompiledSet(t *testing.T) {
	t.Parallel()
	first := registry.Builtins()
	if len(first) > 0 {
		first[0] = "tampered"
	}
	second := registry.Builtins()
	if len(second) == len(first) && len(first) > 0 && second[0] == "tampered" {
		t.Error("Builtins handed out the underlying slice")
	}
}
