package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
)

// recorder is a sink that keeps what it was told, so a turn's events can be asserted on rather than
// rendered. onEmit runs outside the lock, so a test can change the controller from inside a turn
// without deadlocking on the recorder.
type recorder struct {
	mu     sync.Mutex
	events []event.Event
	onEmit func(event.Event)
}

func (r *recorder) Emit(e event.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	if r.onEmit != nil {
		r.onEmit(e)
	}
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

func policyWith(preset agent.Preset) agent.Policy {
	return agent.Policy{
		Model:  model.Model{Provider: "stub", Name: "stub-model"},
		Effort: provider.EffortHigh,
		Preset: preset,
		Permissions: config.Permissions{
			Mode:  "ask",
			Deny:  []string{"Bash(rm -rf*)"},
			Allow: []string{"Bash(go test:*)"},
		},
		Sandbox: config.Sandbox{Bash: "enforce", Network: true},
	}
}

func newController(t *testing.T, sink event.Sink, policy agent.Policy) *agent.Controller {
	t.Helper()
	c, err := agent.NewController(agent.Deps{
		Provider:   stubProvider{},
		Transcript: &session.Transcript{},
		Sink:       sink,
		Policy:     policy,
		Models:     []model.Model{{Provider: "stub", Name: "stub-model"}},
		Tools:      []string{"read_file"},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	return c
}

// TestRunReportsTheTurnThroughTheSink is the seam's whole contract: a turn begins, and it says so.
func TestRunReportsTheTurnThroughTheSink(t *testing.T) {
	t.Parallel()
	sink := &recorder{}
	control, err := newController(t, sink, policyWith(agent.PresetBalanced)).
		Run(context.Background(), agent.TurnInput{Text: "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	events := sink.all()
	started, ok := events[0].(event.TurnStarted)
	if !ok || started.Input != "hello" {
		t.Fatalf("the first event is %+v, want TurnStarted carrying the input", events[0])
	}
	done, ok := events[len(events)-1].(event.TurnDone)
	if !ok || done.Status != event.StatusDone || done.TurnID != started.TurnID {
		t.Fatalf("the last event is %+v, want a matching TurnDone", events[len(events)-1])
	}
}

// TestThePolicyBlockIsHostAuthoredAndAppendedToTheUserTurn is E4-T3's first criterion. Both halves
// matter: the marker is what lets a reader tell the program's words from the person's, and the position
// is what keeps the system prompt — the prefix a vendor caches — untouched.
func TestThePolicyBlockIsHostAuthoredAndAppendedToTheUserTurn(t *testing.T) {
	t.Parallel()
	c := newController(t, &recorder{}, policyWith(agent.PresetDelivery))
	control, err := c.Run(context.Background(), agent.TurnInput{Text: "fix the build"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	messages := c.Messages()
	if len(messages) != 2 {
		t.Fatalf("%d messages, want the input and the policy block", len(messages))
	}
	if messages[0].Content != "fix the build" || messages[0].HostAuthored {
		t.Errorf("the first message is %+v, want the person's words and no host marker", messages[0])
	}
	block := messages[1]
	if !block.HostAuthored {
		t.Error("the policy block is not marked as host-authorated")
	}
	if block.Role == provider.RoleSystem {
		t.Error("the policy block was written into the system prompt, which is the cached prefix")
	}
	if block.Role != provider.RoleUser {
		t.Errorf("the policy block's role is %q, want user", block.Role)
	}
	if !strings.Contains(block.Content, "delivery") {
		t.Errorf("the block does not name the preset it froze: %q", block.Content)
	}
}

// TestAPresetChangeCannotAlterTheTurnThatIsRunning is E4-T3's second criterion, made deterministic: the
// policy is captured before the goroutine starts, so a change raised from inside the turn — the worst
// case timing — reaches only the next one.
func TestAPresetChangeCannotAlterTheTurnThatIsRunning(t *testing.T) {
	t.Parallel()
	sink := &recorder{}
	c := newController(t, sink, policyWith(agent.PresetBalanced))

	// Change the preset at the earliest possible moment of a turn: while the engine is announcing that
	// the turn began.
	sink.onEmit = func(e event.Event) {
		if _, ok := e.(event.TurnStarted); !ok {
			return
		}
		next := policyWith(agent.PresetDelivery)
		c.SetPolicy(next)
	}

	first, err := c.Run(context.Background(), agent.TurnInput{Text: "first"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-first.Done()

	second, err := c.Run(context.Background(), agent.TurnInput{Text: "second"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-second.Done()

	messages := c.Messages()
	var blocks []string
	for _, m := range messages {
		if m.HostAuthored {
			blocks = append(blocks, m.Content)
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("%d policy blocks, want one per turn", len(blocks))
	}
	if !strings.Contains(blocks[0], "balanced") {
		t.Errorf("the running turn's block changed to the new preset:\n%s", blocks[0])
	}
	if !strings.Contains(blocks[1], "delivery") {
		t.Errorf("the next turn did not pick up the new preset:\n%s", blocks[1])
	}
}

// TestSnapshotCopiesTheRulesRatherThanTheirHeaders is the trap in freezing: assigning the
// configuration's slices would freeze the slice headers, so a reload reusing the backing array would
// change what a turn in flight believes it may do.
func TestSnapshotCopiesTheRulesRatherThanTheirHeaders(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Permissions: config.Permissions{
			Mode:  "ask",
			Deny:  []string{"Bash(rm -rf*)"},
			Allow: []string{"Bash(go test:*)"},
		},
		Sandbox: config.Sandbox{AllowWrite: []string{"/tmp"}, ForbidRead: []string{"${HOME}/.ssh"}},
	}
	frozen := agent.Snapshot(cfg, model.Model{Name: "m"}, provider.EffortHigh, agent.PresetBalanced)

	// Rewrite the configuration in place, exactly as a reload that reused the arrays would.
	cfg.Permissions.Deny[0] = "Bash(curl*)"
	cfg.Permissions.Allow[0] = "Bash(rm*)"
	cfg.Sandbox.AllowWrite[0] = "/"
	cfg.Sandbox.ForbidRead[0] = "/etc"

	if frozen.Permissions.Deny[0] != "Bash(rm -rf*)" {
		t.Errorf("the deny rule changed under the turn: %q", frozen.Permissions.Deny[0])
	}
	if frozen.Permissions.Allow[0] != "Bash(go test:*)" {
		t.Errorf("the allow rule changed under the turn: %q", frozen.Permissions.Allow[0])
	}
	if frozen.Sandbox.AllowWrite[0] != "/tmp" || frozen.Sandbox.ForbidRead[0] != "${HOME}/.ssh" {
		t.Errorf("the sandbox roots changed under the turn: %+v", frozen.Sandbox)
	}
}

// TestACancelledContextEndsTheTurnAsCancelled: a frontend shows "stopped" differently from "answered".
func TestACancelledContextEndsTheTurnAsCancelled(t *testing.T) {
	t.Parallel()
	sink := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	control, err := newController(t, sink, policyWith(agent.PresetBalanced)).
		Run(ctx, agent.TurnInput{Text: "never mind"})
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
	err := newController(t, &recorder{}, policyWith(agent.PresetBalanced)).
		Answer("call-1", event.Decision{Allow: true})
	if !errors.Is(err, agent.ErrNoTurn) {
		t.Fatalf("error is %v, want ErrNoTurn", err)
	}
	if !strings.Contains(err.Error(), "call-1") {
		t.Errorf("the error does not name the call it refused: %v", err)
	}
}

func TestAControllerNeedsItsParts(t *testing.T) {
	t.Parallel()
	base := agent.Deps{Provider: stubProvider{}, Transcript: &session.Transcript{}, Sink: &recorder{}}

	withoutProvider := base
	withoutProvider.Provider = nil
	if _, err := agent.NewController(withoutProvider); err == nil {
		t.Error("a controller with no provider was accepted")
	}

	withoutTranscript := base
	withoutTranscript.Transcript = nil
	if _, err := agent.NewController(withoutTranscript); err == nil {
		t.Error("a controller with no transcript was accepted")
	}

	withoutSink := base
	withoutSink.Sink = nil
	if _, err := agent.NewController(withoutSink); err == nil {
		t.Error("a controller with no sink was accepted")
	}
}

func TestAnUnknownPresetIsRefusedRatherThanDefaulted(t *testing.T) {
	t.Parallel()
	if _, err := agent.ParsePreset("delivry"); err == nil {
		t.Error("a misspelled preset was accepted, which would quietly grant the default posture")
	}
	if got, err := agent.ParsePreset(""); err != nil || got != agent.PresetBalanced {
		t.Errorf("an empty preset is %q/%v, want the balanced default", got, err)
	}
	if got, err := agent.ParsePreset("  DELIVERY "); err != nil || got != agent.PresetDelivery {
		t.Errorf("case and space should be forgiven: %q/%v", got, err)
	}
}

// The ports are only worth declaring if the controller is what satisfies them.
var (
	_ agent.History      = (*agent.Controller)(nil)
	_ agent.Capabilities = (*agent.Controller)(nil)
	_ agent.Approvals    = (*agent.Controller)(nil)
)
