package tool

import (
	"context"
	"encoding/json"
)

// The optional capabilities. A tool implements one to say something about itself, and the scheduler
// discovers it by type assertion — never by matching a name, and never by a flag a tool has to remember
// to set in two places. A tool that implements none of them is treated as the safest thing it could be:
// serial, and needing a person.

// ReadOnly says a tool changes nothing, so it may run beside another ReadOnly tool without ordering
// mattering.
type ReadOnly interface {
	ReadOnly() bool
}

// Sequential says a tool must not run at the same time as any other, even a read-only one, because it
// carries state between calls.
type Sequential interface {
	RunsSequentially() bool
}

// PlanSafe says a tool may run while the turn is planning, where a writer may not.
type PlanSafe interface {
	PlanModeSafe() bool
}

// Snip is the geometry a tool wants its output cut to, when the budget forces a cut.
//
// Head and Tail are byte counts, not line counts: a tool knows roughly how much of its own output is
// worth keeping at each end, and lines are a rendering decision the frontend makes.
type Snip struct {
	KeepHead int
	KeepTail int
}

// SnipHinter is a tool that knows how its own output should be shortened, rather than having the generic
// rule applied to it.
type SnipHinter interface {
	SnipHint() Snip
}

// Paged says output should be windowed rather than truncated, for a tool whose output is read in pieces —
// a file, a log.
type Paged interface {
	Paged() bool
}

// Preflighter is a tool that can refuse an argument set before doing anything, so a bad call costs
// nothing. It is not a permission check: that is the permission layer's job, and this is the tool's.
type Preflighter interface {
	Preflight(ctx context.Context, args json.RawMessage) error
}

// DecisionBarrier says a call needs a person's answer before anything after it may run, so the scheduler
// stops the batch there rather than gathering more work the person has not agreed to yet.
type DecisionBarrier interface {
	DecisionBarrier() bool
}

// PathWriter says a tool writes to paths it takes from its arguments, so the sandbox must resolve those
// paths before it runs.
type PathWriter interface {
	WritesPaths() bool
}
