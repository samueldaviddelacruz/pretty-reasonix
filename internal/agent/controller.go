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

	mu      sync.Mutex
	policy  Policy
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

	// The policy goes after the user's own words and never into the system prompt. The system prompt is
	// the prefix a vendor's cache is keyed on, so rewriting it every turn would discard the cache on
	// every request; and a host-authored block is exactly the kind of thing that would change.
	c.transcript.Append(session.Message{
		ID:           running.id + "/policy",
		Role:         provider.RoleUser,
		Content:      policy.Block(),
		HostAuthored: true,
	})

	status := event.StatusDone
	if ctx.Err() != nil {
		status = event.StatusCancelled
	}
	c.sink.Emit(event.TurnDone{TurnID: running.id, Status: status})
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
