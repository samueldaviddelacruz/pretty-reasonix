package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
	sessiontool "pretty-reasonix/internal/tool"
)

var (
	// ErrNoRegistry reports a batch with nowhere to look tools up.
	ErrNoRegistry = errors.New("a batch needs a tool registry")
	// ErrNoContext reports a batch with no context, which is a wiring mistake rather than a cancelled turn.
	ErrNoContext = errors.New("a batch needs a context")
	// ErrUnknownTool reports a call naming a tool nobody registered.
	ErrUnknownTool = errors.New("no such tool")
)

// Executor runs a planned batch of tool calls.
type Executor struct {
	// Registry is where a call's tool is looked up.
	Registry *sessiontool.Registry
	// Boundary bounds every result on entry, once (E5-T3).
	Boundary sessiontool.Boundary
	// Concurrency caps how many calls one parallel segment may start at once. Zero means MaxParallel.
	//
	// It exists separately from MaxParallel because the planner already chunks a run to that size, and a
	// caller may want a lower ceiling (a test, or a machine doing something else) without changing the plan.
	Concurrency int
}

// ToolOutcome is what one call produced.
type ToolOutcome struct {
	// Call is the call this is about, so a result can be matched to what asked for it.
	Call provider.ToolCall
	// Result is the bounded output.
	Result sessiontool.Bound
	// Broken is the tool failing as software — a panic guard, a bug. A tool reporting bad news puts it in
	// Result via Result.Err instead, and the turn continues. The two are kept apart so a broken tool never
	// reads as a missing file.
	Broken error
}

// Outcome is what a batch produced.
type Outcome struct {
	// Results are in call order, whatever order they finished in — the transcript reads as a conversation,
	// not as a race.
	Results []ToolOutcome
	// Cut reports that a decision barrier stopped the plan.
	Cut bool
	// Deferred are the calls a barrier held back.
	Deferred []provider.ToolCall
	// Interrupted reports that the turn was cancelled partway, so Results holds what finished and nothing
	// else.
	Interrupted bool
}

// Run executes a plan's segments in order.
//
// Results are written by index into a slice sized before the goroutines start, never appended from them: an
// append from two goroutines is a data race that passes a single-threaded test and corrupts a transcript
// under load (rule [#69]). The plan is what decides which calls may run together; this type only obeys it.
func (e Executor) Run(ctx context.Context, plan Plan) Outcome {
	outcome := Outcome{Cut: plan.Cut, Deferred: plan.Deferred}
	if e.Registry == nil {
		outcome.Results = []ToolOutcome{{Broken: ErrNoRegistry}}
		return outcome
	}
	// Refused rather than defaulted: substituting Background would make every call unbounded and nothing
	// would ever report why.
	if ctx == nil {
		outcome.Results = []ToolOutcome{{Broken: ErrNoContext}}
		return outcome
	}
	limit := e.Concurrency
	if limit <= 0 || limit > MaxParallel {
		limit = MaxParallel
	}

	var done []ToolOutcome
	for _, segment := range plan.Segments {
		// Checked before each segment rather than inside it: a cancelled turn should not start work it will
		// have to abandon, and what it already has is kept.
		if ctx.Err() != nil {
			outcome.Interrupted = true
			break
		}
		if !segment.Parallel || len(segment.Calls) == 1 {
			done = append(done, e.runSerially(ctx, segment.Calls)...)
		} else {
			done = append(done, e.runTogether(ctx, segment.Calls, limit)...)
		}
		if ctx.Err() != nil {
			outcome.Interrupted = true
			break
		}
	}
	outcome.Results = done
	return outcome
}

// runSerially runs calls one at a time, in order.
func (e Executor) runSerially(ctx context.Context, calls []provider.ToolCall) []ToolOutcome {
	results := make([]ToolOutcome, 0, len(calls))
	for _, call := range calls {
		if ctx.Err() != nil {
			break
		}
		results = append(results, e.runOne(ctx, call))
	}
	return results
}

// runTogether runs a parallel segment with a cap, writing each result where it belongs.
func (e Executor) runTogether(ctx context.Context, calls []provider.ToolCall, limit int) []ToolOutcome {
	// Sized before anything starts, and written by index: no goroutine appends.
	results := make([]ToolOutcome, len(calls))
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for index, call := range calls {
		if ctx.Err() != nil {
			results[index] = ToolOutcome{Call: call}
			continue
		}
		wg.Add(1)
		go func(index int, call provider.ToolCall) {
			defer wg.Done()
			// The slot is taken inside the goroutine so acquiring it cannot block the loop that starts them.
			slots <- struct{}{}
			defer func() { <-slots }()
			results[index] = e.runOne(ctx, call)
		}(index, call)
	}
	wg.Wait()
	return results
}

// runOne looks a call's tool up, runs it, and bounds the result.
//
// One path for both failure kinds. A tool that returns bad news has it in Result.Err and the outcome records a
// result; a tool that breaks has it in Broken. Both end up in the same slice, bounded the same way, so the
// transcript cannot grow two shapes for "the tool did not do what was asked".
func (e Executor) runOne(ctx context.Context, call provider.ToolCall) ToolOutcome {
	outcome := ToolOutcome{Call: call}
	found, known := e.Registry.Lookup(call.Name)
	if !known {
		outcome.Broken = fmt.Errorf("%w: %q", ErrUnknownTool, call.Name)
		return outcome
	}

	result, err := found.Execute(ctx, json.RawMessage(call.Arguments))
	// A tool that succeeded keeps its result, even if the turn was cancelled in the microseconds since. The
	// work is done; throwing it away would violate the one thing cancellation is supposed to guarantee, and
	// the old check here did exactly that whenever the cancel landed between Execute returning and this line.
	if err != nil {
		if ctx.Err() != nil {
			// Cancelled while it ran, so the failure is the interruption rather than anything the tool did.
			outcome.Broken = ctx.Err()
			return outcome
		}
		outcome.Broken = fmt.Errorf("%s: %w", call.Name, err)
		return outcome
	}
	bounded, boundErr := e.Boundary.Bound(result)
	if boundErr != nil {
		outcome.Broken = fmt.Errorf("%s: %w", call.Name, boundErr)
		return outcome
	}
	outcome.Result = bounded
	return outcome
}

// Messages turns an outcome into the transcript entries the model reads.
//
// One path, so the two failure kinds cannot drift into two shapes: a broken tool says so in the text the model
// sees rather than ending the turn, because a model that is told "that tool is broken" can choose another and a
// turn that ended cannot.
func (o Outcome) Messages(turnID string) []session.Message {
	messages := make([]session.Message, 0, len(o.Results))
	for index, result := range o.Results {
		content := result.Result.Visible
		switch {
		case result.Broken != nil:
			content = "the tool could not run: " + result.Broken.Error()
		case result.Result.Truncated:
			content += "\n(output truncated)"
		}
		messages = append(messages, session.Message{
			ID:         fmt.Sprintf("%s/tool-%02d", turnID, index),
			Role:       provider.RoleTool,
			Content:    content,
			RawContent: content,
			Hidden:     false,
			ToolCallID: result.Call.ID,
		})
	}
	return messages
}
