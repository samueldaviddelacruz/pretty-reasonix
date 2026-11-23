// White-box: startSession needs a real terminal on both streams, so the shutdown order it depends on is tested through
// the unexported function it calls.
//
//nolint:testpackage // the subject is unexported
package cli

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/contract/tool"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
	registry "pretty-reasonix/internal/tool"
)

// waitingTool is a tool still running when the person quits: it holds the turn open until its context is cancelled,
// which is the window in which a session closed too early loses what the turn writes next.
type waitingTool struct{ entered chan<- struct{} }

func (waitingTool) Name() string            { return "read_file" }
func (waitingTool) Description() string     { return "waits for cancellation" }
func (waitingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (waitingTool) ReadOnly() bool          { return true }
func (w waitingTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	close(w.entered)
	<-ctx.Done()
	return tool.Result{Output: "stopped part way"}, nil
}

// oneToolCall answers the first request with a call to read_file, and refuses any other: a second round would mean the
// turn ignored its cancellation.
type oneToolCall struct{ once sync.Once }

func (*oneToolCall) Name() string { return "scripted" }

func (p *oneToolCall) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	chunks := make(chan provider.Chunk, 2)
	answered := false
	p.once.Do(func() {
		chunks <- provider.ToolCallChunk{Call: provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}}
		answered = true
	})
	if !answered {
		chunks <- provider.TextChunk{Text: "a round after cancellation"}
	}
	chunks <- provider.DoneChunk{}
	close(chunks)
	return chunks, nil
}

type discard struct{}

func (discard) Emit(event.Event) {}

// TestQuittingMidTurnSavesWhatTheTurnWrote is E10-T11. The person quits while a tool is still running. The turn is
// cancelled, the tool returns, and the executor appends its result to the transcript. That append happens on the
// turn's goroutine after the quit, so a session closed before the turn has stopped loses it, and loses it silently.
// Run with -race: closing first is also a data race on the transcript, which the detector reports.
func TestQuittingMidTurnSavesWhatTheTurnWrote(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-quit")
	transcript, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	entered := make(chan struct{})
	tools := registry.NewRegistry()
	if addErr := tools.Add(waitingTool{entered: entered}); addErr != nil {
		t.Fatalf("Add: %v", addErr)
	}
	controller, err := agent.NewController(agent.Deps{
		Provider:   &oneToolCall{},
		Transcript: transcript,
		Sink:       discard{},
		Policy:     agent.Policy{Model: model.Model{Provider: "scripted", Name: "m"}},
		Registry:   tools,
		Boundary:   registry.Boundary{Dir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	turnCtx, stopTurns := context.WithCancel(context.Background())
	running, err := controller.Run(turnCtx, agent.TurnInput{Text: "read it"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool never started, so there is no turn in flight to quit from")
	}

	if closeErr := endSession(stopTurns, running, transcript.Close); closeErr != nil {
		t.Fatalf("endSession reported %v, want a session that closed cleanly", closeErr)
	}
	select {
	case <-running.Done():
	default:
		t.Fatal("endSession returned while the turn was still running")
	}

	saved, err := session.ReadTranscript(layout.Events, layout.Canonical)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	for _, message := range saved {
		if message.Role == provider.RoleTool && message.ToolCallID == "c1" {
			return
		}
	}
	t.Fatalf("the tool result the turn wrote after the quit is not in the log, which holds %d messages: %+v",
		len(saved), saved)
}

// TestEndingWithNoTurnStillCloses: quitting before anything was sent is the commonest exit, and it must close.
func TestEndingWithNoTurnStillCloses(t *testing.T) {
	t.Parallel()
	closed, stopped := false, false
	err := endSession(func() { stopped = true }, nil, func() error { closed = true; return nil })
	if err != nil || !closed || !stopped {
		t.Fatalf("err=%v closed=%v stopped=%v, want a cancelled, closed session", err, closed, stopped)
	}
}
