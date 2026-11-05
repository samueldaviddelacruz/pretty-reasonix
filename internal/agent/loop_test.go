package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/contract/tool"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
	registry "pretty-reasonix/internal/tool"
)

// counting is a tool that reports how often it ran, which is the only way to tell "the loop executed it" from
// "the loop recorded it".
type counting struct {
	name string
	ran  *int
	out  string
}

func (c counting) Name() string          { return c.name }
func (counting) Description() string     { return "counts its runs" }
func (counting) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (counting) ReadOnly() bool          { return true }
func (c counting) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	*c.ran++
	return tool.Result{Output: c.out}, nil
}

// TestATurnRunsTheToolAndGoesAroundAgain is E4-T11's criterion. One round answers the first question and ignores
// every tool call inside the answer, so the assertion is on the *second* request: the loop went around.
func TestATurnRunsTheToolAndGoesAroundAgain(t *testing.T) {
	t.Parallel()
	ran := 0
	reg := registry.NewRegistry()
	if err := reg.Add(counting{name: "read_file", ran: &ran, out: "module pretty-reasonix"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	p := &scriptedProvider{answers: [][]provider.Chunk{
		// Round one: the model asks for a file.
		{
			provider.ToolCallChunk{Call: provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"go.mod"}`}},
			provider.DoneChunk{},
		},
		// Round two: it answers using what it read.
		textAnswer("the module is pretty-reasonix"),
	}}

	transcript := &session.Transcript{}
	sink := &recorder{}
	c, err := agent.NewController(agent.Deps{
		Provider:   p,
		Transcript: transcript,
		Sink:       sink,
		Policy:     agent.Policy{Model: model.Model{Provider: "scripted", Name: "m"}},
		Registry:   reg,
		Boundary:   registry.Boundary{Dir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	control, err := c.Run(context.Background(), agent.TurnInput{Text: "what does go.mod declare?"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	if ran != 1 {
		t.Fatalf("the tool ran %d times, want once: the loop recorded the call without executing it", ran)
	}
	if got := len(p.seen()); got != 2 {
		t.Fatalf("%d requests, want 2: one to ask for the file and one to use it", got)
	}

	// The second request must carry the tool's output, which is what makes the answer possible at all.
	second := p.seen()[1]
	var carried bool
	for _, message := range second.Messages {
		if message.Role == provider.RoleTool && message.Content == "module pretty-reasonix" {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("the second request does not carry the tool result: %+v", second.Messages)
	}

	var last event.TurnDone
	for _, e := range sink.all() {
		if done, ok := e.(event.TurnDone); ok {
			last = done
		}
	}
	if last.Status != event.StatusDone {
		t.Errorf("the turn ended %q, want done", last.Status)
	}
}

// TestATurnStopsWhenTheBudgetRunsOut: a model that keeps asking forever is a bug, and the turn says blocked
// rather than done — the difference between a resumable turn and a lost one.
func TestATurnStopsWhenTheBudgetRunsOut(t *testing.T) {
	t.Parallel()
	ran := 0
	reg := registry.NewRegistry()
	if err := reg.Add(counting{name: "read_file", ran: &ran, out: "x"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Every round asks for the same tool and never answers.
	endless := []provider.Chunk{
		provider.ToolCallChunk{Call: provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}},
		provider.DoneChunk{},
	}
	p := &scriptedProvider{answers: [][]provider.Chunk{endless, endless, endless, endless, endless, endless, endless, endless}}
	sink := &recorder{}
	c, err := agent.NewController(agent.Deps{
		Provider:   p,
		Transcript: &session.Transcript{},
		Sink:       sink,
		Policy:     agent.Policy{Model: model.Model{Provider: "scripted", Name: "m"}},
		Registry:   reg,
		Boundary:   registry.Boundary{Dir: t.TempDir()},
		MaxSteps:   3,
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	control, err := c.Run(context.Background(), agent.TurnInput{Text: "loop forever"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-control.Done()

	if ran != 3 {
		t.Errorf("the tool ran %d times, want exactly the budget of 3", ran)
	}
	events := sink.all()
	done, ok := events[len(events)-1].(event.TurnDone)
	if !ok || done.Status != event.StatusBlocked {
		t.Fatalf("the turn ended as %+v, want blocked", events[len(events)-1])
	}
}
