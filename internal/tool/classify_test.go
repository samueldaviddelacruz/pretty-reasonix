package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"pretty-reasonix/internal/contract/tool"
	registry "pretty-reasonix/internal/tool"
)

// plainTool implements no optional interface at all, which is the case that must be treated most
// carefully: a tool that says nothing about itself gets the safest schedule, not the most permissive one.
type plainTool struct{ name string }

func (p plainTool) Name() string          { return p.name }
func (plainTool) Description() string     { return "a tool with no opinions" }
func (plainTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (plainTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// readTool is read-only and nothing else.
type readTool struct{ plainTool }

func (readTool) ReadOnly() bool { return true }

// readStateful is read-only but carries state, so it must not run beside anything.
type readStateful struct{ readTool }

func (readStateful) RunsSequentially() bool { return true }

// askingTool needs a person's answer before anything after it may run.
type askingTool struct{ readTool }

func (askingTool) DecisionBarrier() bool { return true }

// writingTool modifies the workspace.
type writingTool struct{ plainTool }

func (writingTool) WritesPaths() bool { return true }

// everything claims every capability at once, which is the combination that has to resolve to a single
// answer rather than an argument between two.
type everything struct{ readTool }

func (everything) RunsSequentially() bool { return true }
func (everything) DecisionBarrier() bool  { return true }
func (everything) WritesPaths() bool      { return true }
func (everything) PlanModeSafe() bool     { return true }
func (everything) Paged() bool            { return true }

// TestAddingACapabilityChangesScheduling is E5-T2's criterion: the scheduler learns what a tool may do by
// asserting an interface, so a tool that adds one changes how it is batched without the loop, the
// registry, or a list of names being touched.
func TestAddingACapabilityChangesScheduling(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		tool tool.Tool
		want registry.Class
	}{
		"a plain tool is serial and unsafe to plan with": {
			plainTool{"plain"},
			registry.Class{},
		},
		"read-only may run beside another read-only tool": {
			readTool{plainTool{"read"}},
			registry.Class{Parallel: true},
		},
		"read-only but stateful must not": {
			readStateful{readTool{plainTool{"stateful"}}},
			registry.Class{},
		},
		"a barrier stops the batch": {
			askingTool{readTool{plainTool{"ask"}}},
			registry.Class{Barrier: true},
		},
		"a writer runs serially whatever else it claims": {
			writingTool{plainTool{"write"}},
			registry.Class{Writer: true},
		},
		"every claim at once resolves to one answer": {
			everything{readTool{plainTool{"all"}}},
			registry.Class{Barrier: true, Writer: true, PlanSafe: true, Paged: true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := registry.Classify(tc.tool); got != tc.want {
				t.Errorf("Classify = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestACapabilityIsAssertedNotNamed: the same tool behaves differently only because of the interface, so a
// name that looked like something carries no weight.
func TestACapabilityIsAssertedNotNamed(t *testing.T) {
	t.Parallel()
	// A tool named "read_file" that implements nothing must still be serial. The name is not evidence.
	sneaky := plainTool{name: "read_file"}
	if got := registry.Classify(sneaky); got.Parallel {
		t.Error("a tool was treated as read-only because of its name")
	}
}

// TestASnipHintIsReadOnlyFromAToolThatDeclaresOne: the generic rule belongs to the caller, and a tool with
// no opinion must not be given an arbitrary one here.
func TestASnipHintIsReadOnlyFromAToolThatDeclaresOne(t *testing.T) {
	t.Parallel()
	if _, found := registry.Snip(plainTool{name: "plain"}); found {
		t.Error("a tool with no hint produced one")
	}
	if _, found := registry.Snip(pagedHint{}); !found {
		t.Error("a tool with a hint was ignored")
	}
}

// pagedHint declares a snip geometry and nothing else of interest.
type pagedHint struct{ plainTool }

func (pagedHint) SnipHint() tool.Snip { return tool.Snip{KeepHead: 100, KeepTail: 20} }
