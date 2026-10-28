package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
)

// recorder is a sink that keeps what it was told, so a turn's events can be asserted on rather than
// rendered.
type recorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *recorder) Emit(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

// stubProvider satisfies the provider interface; the seam under test never calls it.
type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }

func (stubProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("the seam does not call the provider yet")
}

func newController(t *testing.T, sink event.Sink) *agent.Controller {
	t.Helper()
	c, err := agent.NewController(stubProvider{}, &session.Transcript{}, sink,
		[]model.Model{{Provider: "stub", Name: "stub-model"}}, []string{"read_file"})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	return c
}

// TestRunReportsTheTurnThroughTheSink is the whole contract of the seam: a turn begins, and it says so.
func TestRunReportsTheTurnThroughTheSink(t *testing.T) {
	t.Parallel()
	sink := &recorder{}
	control, err := newController(t, sink).Run(context.Background(), agent.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	events := sink.all()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	started, ok := events[0].(event.TurnStarted)
	if !ok || started.Input != "hello" {
		t.Fatalf("the first event is %+v, want TurnStarted carrying the input", events[0])
	}
	done, ok := events[1].(event.TurnDone)
	if !ok || done.Status != event.StatusDone {
		t.Fatalf("the last event is %+v, want TurnDone with a done status", events[1])
	}
	if started.TurnID != done.TurnID {
		t.Errorf("the turn began as %q and ended as %q", started.TurnID, done.TurnID)
	}
}

// TestRunRecordsBothTheInputAndWhatWasRead: the two content slots are separate from the first entry,
// so that a later rewrite can change one without losing the other.
func TestRunRecordsBothTheInputAndWhatWasRead(t *testing.T) {
	t.Parallel()
	c := newController(t, &recorder{})
	control, err := c.Run(context.Background(), agent.TurnInput{Text: "what is 2+2"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	messages := c.Messages()
	if len(messages) != 1 {
		t.Fatalf("%d messages, want the input recorded once", len(messages))
	}
	if messages[0].Content != "what is 2+2" || messages[0].RawContent != "what is 2+2" {
		t.Errorf("recorded %+v, want both slots carrying the input", messages[0])
	}
	if messages[0].Role != provider.RoleUser {
		t.Errorf("role is %q, want user", messages[0].Role)
	}
}

// TestACancelledContextEndsTheTurnAsCancelled: the status has to distinguish "answered" from "stopped",
// because a frontend shows them differently.
func TestACancelledContextEndsTheTurnAsCancelled(t *testing.T) {
	t.Parallel()
	sink := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	control, err := newController(t, sink).Run(ctx, agent.TurnInput{Text: "never mind"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	events := sink.all()
	done, ok := events[len(events)-1].(event.TurnDone)
	if !ok || done.Status != event.StatusCancelled {
		t.Fatalf("the turn ended as %+v, want a cancelled status", events[len(events)-1])
	}
}

// TestADecisionWithNoTurnIsRefused is what keeps a card left on screen from deciding a later turn.
func TestADecisionWithNoTurnIsRefused(t *testing.T) {
	t.Parallel()
	err := newController(t, &recorder{}).Answer("call-1", event.Decision{Allow: true})
	if !errors.Is(err, agent.ErrNoTurn) {
		t.Fatalf("error is %v, want ErrNoTurn", err)
	}
	if !strings.Contains(err.Error(), "call-1") {
		t.Errorf("the error does not name the call it refused: %v", err)
	}
}

func TestAControllerNeedsItsParts(t *testing.T) {
	t.Parallel()
	if _, err := agent.NewController(nil, &session.Transcript{}, &recorder{}, nil, nil); err == nil {
		t.Error("a controller with no provider was accepted")
	}
	if _, err := agent.NewController(stubProvider{}, nil, &recorder{}, nil, nil); err == nil {
		t.Error("a controller with no transcript was accepted")
	}
	if _, err := agent.NewController(stubProvider{}, &session.Transcript{}, nil, nil, nil); err == nil {
		t.Error("a controller with no sink was accepted")
	}
}

// The ports are only worth declaring if the controller is what satisfies them.
var (
	_ agent.History      = (*agent.Controller)(nil)
	_ agent.Capabilities = (*agent.Controller)(nil)
	_ agent.Approvals    = (*agent.Controller)(nil)
)
