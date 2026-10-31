package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
	"pretty-reasonix/internal/session"
)

// ErrEmptyRequest reports a round with nothing to send.
var ErrEmptyRequest = errors.New("the request would carry no messages")

// Round is one request to the model and everything it produced.
type Round struct {
	// Text and Reasoning are what arrived, joined in the order it arrived.
	Text      string
	Reasoning string
	// Calls are the complete tool calls. The contract has no fragment kinds, so a call in this slice is
	// whole by construction rather than by a check here.
	Calls []provider.ToolCall
	// Usage is the last accounting the vendor sent, or zero when it sent none.
	Usage provider.Usage
	// Committed is the transcript entry the round added, when it added one.
	Committed string
}

// Round runs one model round: build the request once, stream it, fold the answer into the transcript.
//
// The request is built once and reused for a retry, which is the session's first criterion stated as
// code. Rebuilding on retry would let the retry answer a different question — a transcript that grew, a
// tool list that changed, or a map iterated in another order — and the model would have no way to say so.
func (c *Controller) Round(ctx context.Context, turnID string, input TurnInput) (Round, error) {
	if ctx == nil {
		return Round{}, errors.New("a round needs a context")
	}
	policy := c.Policy()
	if hasModel(input.Model) {
		policy.Model = input.Model
	}
	if input.Effort.IsSet() {
		policy.Effort = input.Effort
	}

	request, err := c.buildRequest(policy)
	if err != nil {
		return Round{}, err
	}
	// The frozen form, kept for the digest and for a retry. Nothing may be added to it between attempts:
	// a synthetic message inserted on a retry is the same failure as a rebuild, one step quieter.
	digest, err := digestOf(request)
	if err != nil {
		return Round{}, err
	}

	var round Round
	for attempt := 1; ; attempt++ {
		round, err = c.stream(ctx, turnID, request, digest)
		if err == nil {
			break
		}
		// A mid-stream cut may be replayed exactly once, and with the identical request. Anything else,
		// or a second cut, or a caller who has gone away, ends the turn with the error.
		_, cut := errors.AsType[*httpclient.StreamInterruptedError](err)
		if !cut || attempt >= 2 || ctx.Err() != nil {
			return Round{}, err
		}
	}

	round.Committed = c.commit(turnID, round)
	return round, nil
}

// buildRequest assembles the request from the transcript as it stands.
func (c *Controller) buildRequest(policy Policy) (provider.Request, error) {
	messages := c.transcript.Fold()
	if len(messages) == 0 {
		return provider.Request{}, ErrEmptyRequest
	}
	return provider.Request{
		Messages: messages,
		Tools:    c.schemas,
		// Temperature travels in the policy because it is a per-turn posture, not a provider fact: a
		// turn that asks for the most predictable answer and one that asks for variety run the same
		// endpoint.
		Temperature: policy.Temperature,
		MaxTokens:   policy.Model.MaxOutputTokens,
		Effort:      policy.Effort,
	}, nil
}

// stream sends the frozen request and folds what comes back, emitting deltas as they arrive so a
// frontend can render a token before the answer is finished.
func (c *Controller) stream(ctx context.Context, turnID string, request provider.Request, digest string) (Round, error) {
	chunks, err := c.provider.Stream(ctx, request)
	if err != nil {
		return Round{}, err
	}
	itemID := nextItemID()

	var round Round
	for chunk := range chunks {
		if ctx.Err() != nil {
			return Round{}, ctx.Err()
		}
		switch chunk.Kind {
		case provider.ChunkText:
			round.Text += chunk.Text
			c.sink.Emit(event.TextDelta{TurnID: turnID, ItemID: itemID, Text: chunk.Text})
		case provider.ChunkReasoning:
			round.Reasoning += chunk.Text
			c.sink.Emit(event.ReasoningDelta{TurnID: turnID, ItemID: itemID, Text: chunk.Text})
		case provider.ChunkToolCall:
			if chunk.ToolCall != nil {
				round.Calls = append(round.Calls, *chunk.ToolCall)
			}
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				round.Usage = *chunk.Usage
			}
		case provider.ChunkDone:
			// A normal ending. The round is finished, not failed, and collecting continues only so that
			// anything the vendor sent after its marker is not silently dropped.
		case provider.ChunkError:
			return Round{}, fmt.Errorf("%w (request %s)", chunk.Err, digest)
		}
	}
	return round, nil
}

// commit records the round in the transcript and reports it.
//
// An empty answer is not recorded: a model that said nothing and called nothing produced nothing, and an
// empty assistant turn in the record would be a row the UI renders and the next request re-sends for no
// reason.
func (c *Controller) commit(turnID string, round Round) string {
	if round.Text == "" && round.Reasoning == "" && len(round.Calls) == 0 {
		if round.Usage != (provider.Usage{}) {
			c.sink.Emit(event.UsageReported{TurnID: turnID, Usage: round.Usage})
		}
		return ""
	}
	itemID := turnID + "/assistant"
	c.transcript.Append(session.Message{
		ID:        itemID,
		Role:      provider.RoleAssistant,
		Content:   round.Text,
		ToolCalls: round.Calls,
	})
	c.sink.Emit(event.MessageCommitted{TurnID: turnID, ItemID: itemID, Message: provider.Message{
		Role:      provider.RoleAssistant,
		Content:   round.Text,
		ToolCalls: round.Calls,
	}})
	if round.Usage != (provider.Usage{}) {
		c.sink.Emit(event.UsageReported{TurnID: turnID, Usage: round.Usage})
	}
	return itemID
}

// digestOf renders a request's identity, so a retry can be shown to have sent the same thing rather than
// merely to have been built from the same code.
func digestOf(request provider.Request) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encoding the request: %w", err)
	}
	// FNV-1a over the encoded bytes: this is an identity check between two attempts in one process, not
	// a signature, and a cryptographic hash would suggest a guarantee it is not making.
	var hash uint64 = 14695981039346656037
	for _, b := range body {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return fmt.Sprintf("%016x", hash), nil
}

// nextItemID names one assistant item. Monotonic per process, like the turn ids, because an in-memory
// transcript only needs order; a resumed session takes its ids from the log (E9-T3).
var (
	itemIDMu  sync.Mutex
	itemIDSeq uint64
)

func nextItemID() string {
	itemIDMu.Lock()
	defer itemIDMu.Unlock()
	itemIDSeq++
	return fmt.Sprintf("item-%04d", itemIDSeq)
}
