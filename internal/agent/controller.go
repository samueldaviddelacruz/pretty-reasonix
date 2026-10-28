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
	current *turn
}

// NewController wires a controller. Every argument is required: a controller without a sink has
// nowhere to report to, and one without a provider cannot run.
func NewController(p provider.Provider, transcript *session.Transcript, sink event.Sink, models []model.Model, tools []string) (*Controller, error) {
	switch {
	case p == nil:
		return nil, errors.New("a controller needs a provider")
	case transcript == nil:
		return nil, errors.New("a controller needs a transcript")
	case sink == nil:
		return nil, errors.New("a controller needs a sink")
	}
	return &Controller{provider: p, transcript: transcript, sink: sink, models: models, tools: tools}, nil
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
// The turn runs on its own goroutine and reports through the sink, because a frontend that blocked
// here could not render the turn it just started. The returned error is only for a turn that could not
// begin at all; anything that goes wrong afterwards arrives as an event, since by then the caller is
// already reading.
// The interface return is the point: a frontend must be able to hold a handle without depending on the
// concrete turn type, which is the engine's business and will change.
//
//nolint:ireturn // TurnControl is the port this ticket exists to declare
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
	turnCtx, cancel := context.WithCancel(ctx)
	running := &turn{id: nextTurnID(), done: make(chan struct{}), cancel: cancel}
	c.current = running
	c.mu.Unlock()

	go c.run(turnCtx, running, input)
	return running, nil
}

// run is the turn itself. Until the loop lands it does the two things a seam can honestly do: it
// records what was asked, and it says the turn is over.
func (c *Controller) run(ctx context.Context, running *turn, input TurnInput) {
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
// later one — which is the difference between an approval and a blank cheque.
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

// turnID is monotonic per controller, which is all the ordering an in-memory transcript needs. A
// resumed session gets its ids from the log instead (E9-T3).
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
