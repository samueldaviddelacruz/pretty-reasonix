package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
	sessiontool "pretty-reasonix/internal/tool"
)

// Deps is what a controller is built from.
//
// A struct rather than six arguments: the list grows with the engine, and a caller that passes them
// positionally would one day pass a transcript where a sink was expected and find out at runtime.
type Deps struct {
	Provider   provider.Provider
	Transcript *session.Transcript
	Sink       event.Sink
	// Policy is the posture turns run under until it is changed. Each turn takes its own copy.
	Policy Policy
	Models []model.Model
	Tools  []string
	// Registry is the tools a call may name. Required for a turn that can do anything but talk, and checked
	// at boot so a missing one is an error rather than a turn that silently ignores every tool call.
	Registry *sessiontool.Registry
	// Boundary bounds every tool result on entry (E5-T3).
	Boundary sessiontool.Boundary
	// MaxSteps bounds the loop; zero means DefaultMaxSteps.
	MaxSteps int
	// Schemas are the tool definitions the model is shown. They arrive from the tool registry rather
	// than being derived from Tools, because a name is not a description and a schema is.
	Schemas []provider.ToolSchema
}

// Controller is the only thing a frontend calls.
//
// It holds the four things a turn needs — a provider, a transcript, a sink and the models this session
// may use — so that a frontend never assembles them for itself. That is the rule §6.2 states and this
// type is what makes it true: there is no other exported way in.
type Controller struct {
	provider   provider.Provider
	transcript *session.Transcript
	sink       event.Sink
	models     []model.Model
	tools      []string
	schemas    []provider.ToolSchema
	registry   *sessiontool.Registry
	boundary   sessiontool.Boundary
	maxSteps   int

	mu      sync.Mutex
	policy  Policy
	pending *Recovery
	current *turn
}

// NewController wires a controller. Every field that a turn cannot run without is required: a
// controller without a sink has nowhere to report to.
func NewController(d Deps) (*Controller, error) {
	switch {
	case d.Provider == nil:
		return nil, errors.New("a controller needs a provider")
	case d.Transcript == nil:
		return nil, errors.New("a controller needs a transcript")
	case d.Sink == nil:
		return nil, errors.New("a controller needs a sink")
	}
	policy := d.Policy
	if policy.Preset == "" {
		policy.Preset = PresetBalanced
	}
	return &Controller{
		provider:   d.Provider,
		transcript: d.Transcript,
		sink:       d.Sink,
		models:     d.Models,
		tools:      d.Tools,
		schemas:    d.Schemas,
		registry:   d.Registry,
		boundary:   d.Boundary,
		maxSteps:   maxStepsOr(d.MaxSteps),
		policy:     policy,
	}, nil
}

// SetPolicy changes the posture for turns that have not started yet.
//
// A turn in flight keeps the policy it began with: it was authorised under that posture, and letting a
// change reach backwards would mean the turn finished under terms nobody agreed to. This is the setter
// a frontend calls when a person switches preset, and it affects the next turn only.
func (c *Controller) SetPolicy(policy Policy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if policy.Preset == "" {
		policy.Preset = PresetBalanced
	}
	c.policy = policy
}

// Policy is the posture the next turn will run under.
func (c *Controller) Policy() Policy {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.policy
}

// turn is one running turn: the handle a frontend holds and the channel it is told through.
type turn struct {
	id        string
	done      chan struct{}
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func (t *turn) Done() <-chan struct{} { return t.done }

func (t *turn) Cancel() { t.cancel() }

func (t *turn) finish() { t.closeOnce.Do(func() { close(t.done) }) }

// Run starts a turn and returns a handle to it.
//
// The turn runs on its own goroutine and reports through the sink, because a frontend that blocked here
// could not render the turn it just started. The error is only for a turn that could not begin at all;
// anything later arrives as an event, since by then the caller is already reading.
// The interface return is the point: a frontend must be able to hold a handle without depending on the
// concrete turn type, which is the engine's business and will change.
//
//nolint:ireturn // TurnControl is the port E4-T2 exists to declare
func (c *Controller) Run(ctx context.Context, input TurnInput) (TurnControl, error) {
	if input.Text == "" && !hasModel(input.Model) {
		return nil, errors.New("a turn needs something to answer")
	}

	// A second turn while one is running is refused rather than queued: the transcript is append-only
	// and interleaving two writers into it would make the order of a conversation unanswerable.
	c.mu.Lock()
	if c.current != nil {
		c.mu.Unlock()
		return nil, errors.New("a turn is already running")
	}
	// Captured here, synchronously, and handed to the goroutine as a value. Anything SetPolicy does
	// from this moment on applies to the next turn, which is what makes a mid-turn change harmless
	// rather than a race.
	frozen := c.policy
	if hasModel(input.Model) {
		frozen.Model = input.Model
	}
	if input.Effort.IsSet() {
		frozen.Effort = input.Effort
	}
	// Refused rather than defaulted: a nil context is a caller bug, and substituting Background here
	// would make the turn uncancellable in a way nothing would ever report.
	if ctx == nil {
		return nil, errors.New("a turn needs a context")
	}
	turnCtx, cancel := context.WithCancel(ctx)
	running := &turn{id: nextTurnID(), done: make(chan struct{}), cancel: cancel}
	c.current = running
	c.mu.Unlock()

	go c.run(turnCtx, running, input, frozen)
	return running, nil
}

// run is the turn itself. Until the loop lands it does what a seam honestly can: it records what was
// asked, appends the policy it was authorised under, and says the turn is over.
func (c *Controller) run(ctx context.Context, running *turn, input TurnInput, policy Policy) {
	defer func() {
		c.mu.Lock()
		c.current = nil
		c.mu.Unlock()
		running.finish()
	}()

	c.transcript.Append(session.Message{
		ID:   running.id + "/input",
		Role: provider.RoleUser,
		// RawContent is what arrived, Content is what the model should read. Until something rewrites
		// one into the other they are the same, and the record keeps both slots so that stays true
		// when it changes.
		Content:    input.Text,
		RawContent: input.Text,
	})
	c.sink.Emit(event.TurnStarted{TurnID: running.id, Input: input.Text})

	// A record left by an earlier turn goes in before this one's answer is requested, so the model starts
	// the turn knowing what the last one was doing rather than asking the person to explain again.
	if pending := c.takeRecovery(); pending != nil {
		c.transcript.Append(session.Message{
			ID:           running.id + "/recovery",
			Role:         provider.RoleUser,
			Content:      pending.Render(),
			HostAuthored: true,
		})
		c.sink.Emit(event.Notice{Level: event.NoticeInfo, Code: "turn.recovered", Text: pending.TurnID})
	}
	// Placed after the input and the policy so the record reads as context rather than as the request.
	c.transcript.Append(session.Message{
		ID:           running.id + "/policy",
		Role:         provider.RoleUser,
		Content:      policy.Block(),
		HostAuthored: true,
	})

	// Checked before the request rather than after it: a cancelled turn must not spend a request, and
	// "cancelled" must not be reported as "failed" merely because a provider was reached and refused.
	if ctx.Err() != nil {
		c.recordRecovery(Recovery{TurnID: running.id, Input: input.Text, Cancelled: true})
		c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusCancelled})
		return
	}

	// The turn is a loop: a round, then whatever tools it asked for, then another round. One round would
	// answer the first question and ignore every tool call inside the answer, which is what this ticket
	// exists to fix — the tools and the executor both worked and nothing joined them.
	for range c.maxSteps {
		round, err := c.Round(ctx, running.id, input)
		if err != nil {
			c.endWithError(running, input, round, err)
			return
		}
		if len(round.Calls) == 0 {
			c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusDone, Usage: round.Usage})
			return
		}

		outcome := Executor{Registry: c.registry, Boundary: c.boundary}.Run(ctx, PlanBatch(round.Calls, c.registry))
		for _, message := range outcome.Messages(running.id) {
			c.transcript.Append(message)
		}
		for _, result := range outcome.Results {
			c.sink.Emit(event.ToolResult{
				CallID:    result.Call.ID,
				Output:    result.Result.Visible,
				Truncated: result.Result.Truncated,
				Err:       result.Broken,
			})
		}
		switch {
		case outcome.Interrupted:
			c.recordRecovery(Recovery{TurnID: running.id, Input: input.Text, Cancelled: true})
			c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusCancelled})
			return
		case outcome.Cut:
			// A decision barrier ended the batch. Approvals are E7's, so the turn stops honestly rather
			// than reporting the held-back calls as refused.
			c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusBlocked})
			return
		}
	}
	// The budget ran out with the model still asking. Blocked rather than done, because the turn did not
	// finish and saying so is the difference between a resumable turn and a lost one.
	c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusBlocked})
}

// endWithError records what a failed round had managed and reports it.
func (c *Controller) endWithError(running *turn, input TurnInput, round Round, err error) {
	if errors.Is(err, context.Canceled) {
		// Stopped rather than broken, so the next turn resumes instead of starting over.
		c.recordRecovery(Recovery{TurnID: running.id, Input: input.Text, Cancelled: true, Progress: round.Progress()})
		c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusCancelled, Usage: round.Usage})
		return
	}
	c.recordRecovery(Recovery{TurnID: running.id, Input: input.Text, Err: err, Progress: round.Progress()})
	c.sink.Emit(event.Notice{Level: event.NoticeError, Code: "turn.failed", Text: err.Error()})
	c.sink.Emit(event.TurnDone{TurnID: running.id, Status: event.StatusFailed, Usage: round.Usage})
}

// DefaultMaxSteps is how many round trips a turn may take when nobody says.
//
// Finite because a model that keeps asking forever is a bug rather than a long task, and generous because one
// tool call per round is ordinary: read, edit, run, read the failure, edit again.
const DefaultMaxSteps = 12

// maxStepsOr turns "unset" into the default without letting zero mean infinity.
func maxStepsOr(steps int) int {
	if steps <= 0 {
		return DefaultMaxSteps
	}
	return steps
}

// recordRecovery keeps what the next turn should know, replacing any record still waiting: two interrupted
// turns in a row describe the most recent one, because that is where the work actually stopped.
func (c *Controller) recordRecovery(r Recovery) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = &r
}

// takeRecovery hands over the pending record, if there is one, exactly once.
func (c *Controller) takeRecovery() *Recovery {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pending
	c.pending = nil
	return pending
}

// Messages is the transcript in order, including entries a request never sees.
func (c *Controller) Messages() []session.Message { return c.transcript.All() }

// Models are the ones this session may use.
func (c *Controller) Models() []model.Model { return c.models }

// Tools are the tool names available to the model.
func (c *Controller) Tools() []string { return c.tools }

// Answer delivers a decision for a call an ApprovalRequested event named.
//
// Refused while no turn is running, so a card left on screen from a turn that ended cannot decide a
// later one — the difference between an approval and a blank cheque.
func (c *Controller) Answer(callID string, _ event.Decision) error {
	c.mu.Lock()
	running := c.current
	c.mu.Unlock()
	if running == nil {
		return fmt.Errorf("%w (call %q)", ErrNoTurn, callID)
	}
	return nil
}

// hasModel reports whether a model was resolved, since a zero Model runs against nothing.
func hasModel(m model.Model) bool { return m.Name != "" }

// turnID is monotonic per process, which is all the ordering an in-memory transcript needs. A resumed
// session takes its ids from the log instead (E9-T3).
var (
	turnIDMu  sync.Mutex
	turnIDSeq uint64
)

func nextTurnID() string {
	turnIDMu.Lock()
	defer turnIDMu.Unlock()
	turnIDSeq++
	return fmt.Sprintf("turn-%04d", turnIDSeq)
}
