package agent

import (
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/tool"
)

// MaxParallel is how many read-only tools may run at the same time.
//
// A named constant rather than a literal at the call site, and the number is a compromise rather than a
// measurement: high enough that a batch of reads is not a queue, low enough that eight subprocesses do not
// starve the machine the tools run on. A test asserts no segment exceeds it, so raising it is a decision
// somebody makes rather than a line somebody edits.
const MaxParallel = 8

// Segment is a run of calls the executor may start together.
type Segment struct {
	// Calls are in provider order.
	Calls []provider.ToolCall
	// Parallel says the calls in this segment are read-only and independent, so they may run at once.
	Parallel bool
}

// Plan is what one batch of tool calls may do.
type Plan struct {
	// Segments are what to run, in order.
	Segments []Segment
	// Cut reports that a decision barrier stopped the batch. The calls after it are not in Segments,
	// because a person has not agreed to them yet.
	Cut bool
	// Deferred are the calls the barrier held back, in provider order, so the next round can pick them up
	// rather than the model being asked to repeat itself.
	Deferred []provider.ToolCall
}

// PlanBatch cuts a batch of tool calls into segments that may run.
//
// Two rules do the work. The batch stops at the first decision barrier: everything after it is deferred,
// because gathering more work a person has not agreed to is how a batch turns into a surprise. And within
// what remains, only *contiguous* read-only calls are grouped: two reads with a write between them are two
// segments, not one, so the write's ordering with respect to both is preserved exactly as the model asked
// for it. Grouping by kind rather than by position would quietly reorder a conversation with the
// filesystem.
func PlanBatch(calls []provider.ToolCall, registry *tool.Registry) Plan {
	var plan Plan
	var run []provider.ToolCall

	flush := func() {
		// Chunked rather than trusted: a run longer than the cap becomes several segments instead of one
		// that the executor would have to remember to bound.
		for len(run) > 0 {
			n := min(len(run), MaxParallel)
			plan.Segments = append(plan.Segments, Segment{Calls: run[:n], Parallel: true})
			run = run[n:]
		}
	}

	for index, call := range calls {
		class, known := classifyCall(call, registry)
		if known && class.Barrier {
			flush()
			plan.Segments = append(plan.Segments, Segment{Calls: []provider.ToolCall{call}})
			plan.Cut = true
			plan.Deferred = append(plan.Deferred, calls[index+1:]...)
			return plan
		}
		if known && class.Parallel {
			run = append(run, call)
			continue
		}
		// A writer, a sequential tool, or a name nobody registered. An unknown name is serial because a
		// tool this program cannot describe is a tool it cannot claim is safe to run beside another.
		flush()
		plan.Segments = append(plan.Segments, Segment{Calls: []provider.ToolCall{call}})
	}
	flush()
	return plan
}

// classifyCall looks a call's tool up and asks what it may do.
func classifyCall(call provider.ToolCall, registry *tool.Registry) (tool.Class, bool) {
	if registry == nil {
		return tool.Class{}, false
	}
	found, known := registry.Lookup(call.Name)
	if !known {
		return tool.Class{}, false
	}
	return tool.Classify(found), true
}
