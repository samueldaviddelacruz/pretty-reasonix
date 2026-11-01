package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/contract/tool"
	registry "pretty-reasonix/internal/tool"
)

// stub is a tool with a fixed classification, so a test states the scheduling it wants rather than the
// interfaces that produce it.
type stub struct {
	name string
	kind string // "", "read", "stateful", "write", "barrier"
}

func (s stub) Name() string          { return s.name }
func (stub) Description() string     { return "stub" }
func (stub) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (stub) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}
func (s stub) ReadOnly() bool         { return s.kind == "read" || s.kind == "stateful" }
func (s stub) RunsSequentially() bool { return s.kind == "stateful" }
func (s stub) WritesPaths() bool      { return s.kind == "write" }
func (s stub) DecisionBarrier() bool  { return s.kind == "barrier" }

// registryWith builds a registry holding one tool per kind, named after it.
func registryWith(t *testing.T) *registry.Registry {
	t.Helper()
	r := registry.NewRegistry()
	for _, kind := range []string{"read", "stateful", "write", "barrier"} {
		s := stub{name: kind, kind: kind}
		var as tool.Tool = s
		// A read-only tool with no state is the parallel case; give it only the interfaces it needs by
		// wrapping so Classify sees exactly one capability per tool.
		switch kind {
		case "read":
			as = readOnlyStub{s}
		case "stateful":
			as = statefulStub{s}
		case "write":
			as = writerStub{s}
		case "barrier":
			as = barrierStub{s}
		}
		if err := r.Add(as); err != nil {
			t.Fatalf("Add %s: %v", kind, err)
		}
	}
	return r
}

type readOnlyStub struct{ stub }

func (readOnlyStub) ReadOnly() bool { return true }

type statefulStub struct{ stub }

func (statefulStub) ReadOnly() bool         { return true }
func (statefulStub) RunsSequentially() bool { return true }

type writerStub struct{ stub }

func (writerStub) WritesPaths() bool { return true }

type barrierStub struct{ stub }

func (barrierStub) DecisionBarrier() bool { return true }

// calls turns names into calls in order, which is all a batch is.
func calls(names ...string) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(names))
	for index, name := range names {
		out = append(out, provider.ToolCall{ID: fmt.Sprintf("c%d", index), Name: name})
	}
	return out
}

// shape renders a plan as "P:read,read|S:write", so a failure reads as the schedule rather than as a
// struct full of slices.
func shape(plan agent.Plan) string {
	parts := make([]string, 0, len(plan.Segments))
	for _, segment := range plan.Segments {
		names := make([]string, 0, len(segment.Calls))
		for _, call := range segment.Calls {
			names = append(names, call.Name)
		}
		marker := "S"
		if segment.Parallel {
			marker = "P"
		}
		parts = append(parts, marker+":"+strings.Join(names, ","))
	}
	return strings.Join(parts, "|")
}

// TestTheBatchIsCutAtTheFirstBarrier is the criterion's first clause. Everything after the barrier is
// deferred rather than run, because a person has not agreed to it yet.
func TestTheBatchIsCutAtTheFirstBarrier(t *testing.T) {
	t.Parallel()
	plan := agent.PlanBatch(calls("read", "read", "barrier", "read", "write"), registryWith(t))

	if got := shape(plan); got != "P:read,read|S:barrier" {
		t.Errorf("the plan is %q, want the reads then the barrier and nothing after", got)
	}
	if !plan.Cut {
		t.Error("the plan does not report that a barrier stopped it")
	}
	if len(plan.Deferred) != 2 || plan.Deferred[0].Name != "read" || plan.Deferred[1].Name != "write" {
		t.Errorf("deferred is %+v, want the two calls after the barrier in provider order", plan.Deferred)
	}
}

// TestOnlyContiguousReadsAreGrouped is the clause that protects ordering: two reads with a write between
// them are two segments, so the write's position relative to both is exactly what the model asked for.
func TestOnlyContiguousReadsAreGrouped(t *testing.T) {
	t.Parallel()
	plan := agent.PlanBatch(calls("read", "read", "write", "read"), registryWith(t))
	if got := shape(plan); got != "P:read,read|S:write|P:read" {
		t.Errorf("the plan is %q, want the write to break the run rather than be hoisted", got)
	}
}

// TestAStatefulReadIsSerial: read-only is not enough on its own.
func TestAStatefulReadIsSerial(t *testing.T) {
	t.Parallel()
	plan := agent.PlanBatch(calls("read", "stateful", "read"), registryWith(t))
	if got := shape(plan); got != "P:read|S:stateful|P:read" {
		t.Errorf("the plan is %q, want the stateful read alone", got)
	}
}

// TestAnUnknownNameIsSerial: a tool this program cannot describe is a tool it cannot claim is safe.
func TestAnUnknownNameIsSerial(t *testing.T) {
	t.Parallel()
	plan := agent.PlanBatch(calls("read", "mystery", "read"), registryWith(t))
	if got := shape(plan); got != "P:read|S:mystery|P:read" {
		t.Errorf("the plan is %q, want the unknown name run alone", got)
	}
}

// TestNoSegmentExceedsTheCap: the cap is enforced by construction, so the executor never has to remember it.
func TestNoSegmentExceedsTheCap(t *testing.T) {
	t.Parallel()
	names := make([]string, 0, 20)
	for range 20 {
		names = append(names, "read")
	}
	plan := agent.PlanBatch(calls(names...), registryWith(t))

	total := 0
	for _, segment := range plan.Segments {
		total += len(segment.Calls)
		if len(segment.Calls) > agent.MaxParallel {
			t.Errorf("a segment holds %d calls, above the cap of %d", len(segment.Calls), agent.MaxParallel)
		}
		if !segment.Parallel {
			t.Error("a run of read-only calls produced a serial segment")
		}
	}
	if total != 20 {
		t.Errorf("%d calls planned, want all 20", total)
	}
	if len(plan.Segments) != 3 {
		t.Errorf("%d segments for 20 reads at a cap of 8, want 3", len(plan.Segments))
	}
}

// TestNothingToPlan: an empty batch is not an error, it is an empty plan.
func TestNothingToPlan(t *testing.T) {
	t.Parallel()
	if plan := agent.PlanBatch(nil, registryWith(t)); len(plan.Segments) != 0 || plan.Cut {
		t.Errorf("an empty batch produced %+v", plan)
	}
}
