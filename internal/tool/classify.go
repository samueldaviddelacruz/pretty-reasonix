package tool

import "pretty-reasonix/internal/contract/tool"

// Class is how the batch scheduler may treat one tool.
//
// It is a value rather than three separate questions at the call site because the questions interact:
// a barrier outranks everything, and a writer is never parallel whatever else it claims. Answering them
// in three places is how two of them end up disagreeing.
type Class struct {
	// Parallel says the tool may join a parallel segment: it is read-only and not sequential.
	Parallel bool
	// Barrier says a person must answer before anything after this call may run.
	Barrier bool
	// Writer says the tool modifies the workspace, so it must run serially in provider order.
	Writer bool
	// PlanSafe says the tool may run while the turn is planning.
	PlanSafe bool
	// Paged says its output is windowed rather than truncated.
	Paged bool
}

// Classify reads a tool's optional interfaces.
//
// Every claim is a *type assertion*, which is the whole design: a tool says what it is by implementing an
// interface, so the scheduler learns about a new capability without a list somewhere being updated and
// without a name ever being matched. A tool that implements nothing is treated as the safest thing it
// could be — serial, and not plan-safe — because the alternative is a new tool silently acquiring
// privileges by being forgotten.
func Classify(t tool.Tool) Class {
	var class Class

	// Read-only is not enough on its own: a tool that carries state between calls must not run beside
	// anything, however harmless it looks.
	if readOnly, ok := t.(tool.ReadOnly); ok && readOnly.ReadOnly() {
		sequential, carries := t.(tool.Sequential)
		class.Parallel = !carries || !sequential.RunsSequentially()
	}
	if barrier, ok := t.(tool.DecisionBarrier); ok {
		class.Barrier = barrier.DecisionBarrier()
	}
	if writer, ok := t.(tool.PathWriter); ok {
		class.Writer = writer.WritesPaths()
	}
	if planSafe, ok := t.(tool.PlanSafe); ok {
		class.PlanSafe = planSafe.PlanModeSafe()
	}
	if paged, ok := t.(tool.Paged); ok {
		class.Paged = paged.Paged()
	}

	// A barrier and a writer both mean "nothing else at the same time": a batch cut here is cut for
	// everybody, so a tool that claims both is still exactly one of them for scheduling purposes.
	if class.Barrier || class.Writer {
		class.Parallel = false
	}
	return class
}

// Snip holds the head and tail of a tool's output.
//
// It reads the hint only from a tool that declares one: the generic rule is the caller's business, and a
// tool with no opinion should not be given an arbitrary one here.
func Snip(t tool.Tool) (tool.Snip, bool) {
	if hinter, ok := t.(tool.SnipHinter); ok {
		return hinter.SnipHint(), true
	}
	return tool.Snip{}, false
}
