package agent_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
)

// scriptedProvider answers with a queue of streams, one per call, and records every request it was
// given so a retry can be compared with the attempt it replaced.
type scriptedProvider struct {
	mu       sync.Mutex
	requests []provider.Request
	answers  [][]provider.Chunk
}

func (*scriptedProvider) Name() string { return "scripted" }

func (s *scriptedProvider) Stream(_ context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	if len(s.answers) == 0 {
		return nil, errors.New("the script ran out of answers")
	}
	answer := s.answers[0]
	s.answers = s.answers[1:]

	chunks := make(chan provider.Chunk, len(answer))
	for _, chunk := range answer {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}

func (s *scriptedProvider) seen() []provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.Request(nil), s.requests...)
}

func textAnswer(parts ...string) []provider.Chunk {
	chunks := make([]provider.Chunk, 0, len(parts)+2)
	for _, part := range parts {
		chunks = append(chunks, provider.Chunk{Kind: provider.ChunkText, Text: part})
	}
	chunks = append(chunks,
		provider.Chunk{Kind: provider.ChunkUsage, Usage: &provider.Usage{InputTokens: 11, OutputTokens: 3, FinishReason: "stop"}},
		provider.Chunk{Kind: provider.ChunkDone})
	return chunks
}

// roundController builds a controller whose transcript already holds one user message.
func roundController(t *testing.T, p provider.Provider, sink event.Sink) *agent.Controller {
	t.Helper()
	transcript := &session.Transcript{}
	transcript.Append(session.Message{ID: "u1", Role: provider.RoleUser, Content: "hello"})
	c, err := agent.NewController(agent.Deps{
		Provider:   p,
		Transcript: transcript,
		Sink:       sink,
		Policy: agent.Policy{
			Model:  model.Model{Provider: "scripted", Name: "m", MaxOutputTokens: 256},
			Effort: provider.EffortHigh,
			Preset: agent.PresetBalanced,
		},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	return c
}

// TestATextRoundEmitsDeltasAsTheyArriveAndCommitsOnce is E4-T4's second criterion in its plain form: a
// frontend must be able to render a token before the answer is finished.
func TestATextRoundEmitsDeltasAsTheyArriveAndCommitsOnce(t *testing.T) {
	t.Parallel()
	p := &scriptedProvider{answers: [][]provider.Chunk{textAnswer("Well ", "hello", " there")}}
	sink := &recorder{}

	round, err := roundController(t, p, sink).Round(context.Background(), "turn-1", agent.TurnInput{})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if round.Text != "Well hello there" {
		t.Errorf("text is %q", round.Text)
	}

	var deltas int
	var committed int
	for _, e := range sink.all() {
		switch e.(type) {
		case event.TextDelta:
			deltas++
		case event.MessageCommitted:
			committed++
		}
	}
	if deltas != 3 {
		t.Errorf("%d text deltas, want one per chunk: a frontend that batched them could not stream", deltas)
	}
	if committed != 1 {
		t.Errorf("%d MessageCommitted events, want exactly one for the round", committed)
	}
}

// TestOnlyCompleteToolCallsAreCollected: the contract has no fragment kinds, so anything that reaches the
// round as a ChunkToolCall is whole. This pins that a round does not invent fragments of its own.
func TestOnlyCompleteToolCallsAreCollected(t *testing.T) {
	t.Parallel()
	p := &scriptedProvider{answers: [][]provider.Chunk{{
		{Kind: provider.ChunkText, Text: "calling"},
		{Kind: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"go.mod"}`}},
		{Kind: provider.ChunkDone},
	}}}
	round, err := roundController(t, p, &recorder{}).Round(context.Background(), "turn-1", agent.TurnInput{})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if len(round.Calls) != 1 || round.Calls[0].Arguments != `{"path":"go.mod"}` {
		t.Fatalf("calls are %+v, want the one complete call", round.Calls)
	}
}

// TestARetryReplaysTheIdenticalRequest is E4-T4's first criterion, and the reason the request is built
// once: a rebuild would let the retry answer a different question without anybody noticing.
func TestARetryReplaysTheIdenticalRequest(t *testing.T) {
	t.Parallel()
	p := &scriptedProvider{answers: [][]provider.Chunk{
		// First attempt: a stream cut after one token.
		{
			{Kind: provider.ChunkText, Text: "half"},
			{Kind: provider.ChunkError, Err: &httpclient.StreamInterruptedError{Delivered: 1}},
		},
		// Second attempt: a whole answer.
		textAnswer("the whole answer"),
	}}
	sink := &recorder{}

	round, err := roundController(t, p, sink).Round(context.Background(), "turn-1", agent.TurnInput{})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if round.Text != "the whole answer" {
		t.Errorf("text is %q, want the replayed answer", round.Text)
	}

	seen := p.seen()
	if len(seen) != 2 {
		t.Fatalf("%d attempts, want 2", len(seen))
	}
	// Byte-identical: same messages, same count, nothing inserted between attempts.
	if !reflect.DeepEqual(seen[0], seen[1]) {
		t.Fatalf("the retry did not replay the same request:\nfirst  %+v\nsecond %+v", seen[0], seen[1])
	}
	if len(seen[1].Messages) != 1 {
		t.Errorf("the retry carried %d messages, want the one the first attempt sent", len(seen[1].Messages))
	}
}

// TestASecondCutEndsTheTurn: one replay is a kindness, not a loop.
func TestASecondCutEndsTheTurn(t *testing.T) {
	t.Parallel()
	cut := func() []provider.Chunk {
		return []provider.Chunk{
			{Kind: provider.ChunkText, Text: "half"},
			{Kind: provider.ChunkError, Err: &httpclient.StreamInterruptedError{Delivered: 1}},
		}
	}
	p := &scriptedProvider{answers: [][]provider.Chunk{cut(), cut(), cut()}}

	_, err := roundController(t, p, &recorder{}).Round(context.Background(), "turn-1", agent.TurnInput{})
	if err == nil {
		t.Fatal("a stream cut twice was replayed again")
	}
	if got := len(p.seen()); got != 2 {
		t.Errorf("%d attempts, want exactly 2: one replay and then the error", got)
	}
}

// TestAFailureCommitsNothing: a turn that produced half an answer must not leave it in the record as
// though it were a finished one.
func TestAFailureCommitsNothing(t *testing.T) {
	t.Parallel()
	p := &scriptedProvider{answers: [][]provider.Chunk{{
		{Kind: provider.ChunkText, Text: "half"},
		{Kind: provider.ChunkError, Err: errors.New("the vendor gave up")},
	}}}
	c := roundController(t, p, &recorder{})

	if _, err := c.Round(context.Background(), "turn-1", agent.TurnInput{}); err == nil {
		t.Fatal("a failed round reported success")
	}
	if got := len(c.Messages()); got != 1 {
		t.Errorf("%d transcript entries, want only the user message: %+v", got, c.Messages())
	}
}

// TestAnEmptyAnswerCommitsNothing: a model that said nothing produced nothing, and an empty assistant row
// would be re-sent on every later request for no reason.
func TestAnEmptyAnswerCommitsNothing(t *testing.T) {
	t.Parallel()
	p := &scriptedProvider{answers: [][]provider.Chunk{{{Kind: provider.ChunkDone}}}}
	c := roundController(t, p, &recorder{})

	round, err := c.Round(context.Background(), "turn-1", agent.TurnInput{})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if round.Committed != "" {
		t.Errorf("an empty answer committed %q", round.Committed)
	}
	if got := len(c.Messages()); got != 1 {
		t.Errorf("%d transcript entries, want only the user message", got)
	}
}
