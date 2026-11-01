package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// blockingProvider stays in its request until the context ends, which is the only way to interrupt a turn
// deterministically: the test waits until the provider has been entered rather than sleeping.
type blockingProvider struct {
	once    sync.Once
	entered chan struct{}
}

func (*blockingProvider) Name() string { return "blocking" }

func (b *blockingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	// Only the first request blocks. The point of the test is that the turn after a cancelled one can
	// start, so a provider that blocked every time would hang on its own scaffolding rather than on the
	// behaviour under test.
	block := false
	b.once.Do(func() {
		block = true
		close(b.entered)
	})
	if !block {
		chunks := make(chan provider.Chunk, 2)
		chunks <- provider.TextChunk{Text: "ok"}
		chunks <- provider.DoneChunk{}
		close(chunks)
		return chunks, nil
	}
	chunks := make(chan provider.Chunk)
	go func() {
		<-ctx.Done()
		close(chunks)
	}()
	return chunks, nil
}

// TestACancelledTurnLeavesARecordForTheNext is E4-T6's criterion: cancelling stops the in-flight request,
// marks the turn interrupted, and folds a bounded record into the *next* turn rather than leaving it to
// start from amnesia.
func TestACancelledTurnLeavesARecordForTheNext(t *testing.T) {
	t.Parallel()
	blocking := &blockingProvider{entered: make(chan struct{})}
	sink := &recorder{}
	transcript := &session.Transcript{}
	c, err := agent.NewController(agent.Deps{
		Provider:   blocking,
		Transcript: transcript,
		Sink:       sink,
		Policy:     policyWith(agent.PresetBalanced),
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	first, err := c.Run(context.Background(), agent.TurnInput{Text: "rewrite the parser"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-blocking.entered
	first.Cancel()
	<-first.Done()

	// The turn that was stopped says so.
	var last event.TurnDone
	for _, e := range sink.all() {
		if done, ok := e.(event.TurnDone); ok {
			last = done
		}
	}
	if last.Status != event.StatusCancelled {
		t.Fatalf("the interrupted turn ended as %q, want cancelled", last.Status)
	}

	// And the next turn starts knowing about it.
	second, err := c.Run(context.Background(), agent.TurnInput{Text: "carry on"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-second.Done()

	var recovery *session.Message
	for index, m := range c.Messages() {
		if m.HostAuthored && strings.HasSuffix(m.ID, "/recovery") {
			recovery = &c.Messages()[index]
		}
	}
	if recovery == nil {
		t.Fatal("the next turn was given no record of the interrupted one")
	}
	if !strings.Contains(recovery.Content, "interrupted") {
		t.Errorf("the record does not say the turn was interrupted: %q", recovery.Content)
	}
	if !strings.Contains(recovery.Content, "rewrite the parser") {
		t.Errorf("the record does not say what was being attempted: %q", recovery.Content)
	}
	if recovery.Role == provider.RoleSystem {
		t.Error("the record was written into the system prompt, which is the cached prefix")
	}
}

// TestTheRecordIsBounded is what keeps the memory from becoming the next turn's cost.
func TestTheRecordIsBounded(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", 10_000)
	record := agent.Recovery{
		TurnID:    "turn-0001",
		Input:     huge,
		Progress:  []string{huge, "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth"},
		Cancelled: true,
	}
	rendered := record.Render()

	if len(rendered) > 4*agent.RecoveryInputBytes {
		t.Errorf("the record is %d bytes, which is not a bounded memory", len(rendered))
	}
	// Counted by content rather than by line prefix: the omission note starts with the same "  - ", and
	// counting prefixes made this assertion fail on correct output.
	for _, dropped := range []string{"first", "second"} {
		if strings.Contains(rendered, "  - "+dropped+"\n") {
			t.Errorf("the record kept an old step %q:\n%s", dropped, rendered)
		}
	}
	// The last steps are the ones worth keeping: what it was doing when it stopped beats what it did first.
	if !strings.Contains(rendered, "tenth") {
		t.Errorf("the record dropped the most recent step:\n%s", rendered)
	}
}

// TestAFailedTurnAlsoLeavesARecord: an interruption and a failure are different, and the record has to say
// which, because resuming and starting over are different next steps.
func TestAFailedTurnAlsoLeavesARecord(t *testing.T) {
	t.Parallel()
	record := agent.Recovery{TurnID: "t", Input: "x", Err: context.DeadlineExceeded}
	if !strings.Contains(record.Render(), "failed") {
		t.Errorf("a failed turn was rendered as an interruption:\n%s", record.Render())
	}
	if strings.Contains(record.Render(), "interrupted") {
		t.Errorf("a failed turn was rendered as an interruption:\n%s", record.Render())
	}
}
