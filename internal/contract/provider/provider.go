package provider

import (
	"context"
	"encoding/json"
)

// Role is who authored a message. The four roles cover every wire protocol this
// program speaks; a provider maps them onto its own vocabulary, which is why the
// mapping is the provider's business rather than a shape imposed here — Anthropic
// lifts the system turn into a top-level field, and the OpenAI-shaped kinds keep it
// in the list.
type Role string

const (
	// RoleSystem is the operator's instruction: written by the program or by the
	// user's configuration, never by the person in the conversation.
	RoleSystem Role = "system"
	// RoleUser is the person.
	RoleUser Role = "user"
	// RoleAssistant is the model.
	RoleAssistant Role = "assistant"
	// RoleTool carries the result of a tool call.
	RoleTool Role = "tool"
)

// ToolCall is the model asking for a tool to run.
//
// Arguments is the raw JSON the model produced, held as a string rather than as a
// json.RawMessage, because it arrives as a stream of fragments that have to be
// concatenated before they are JSON at all. Parsing happens
// at the boundary where the tool runs, so a call that never manages to become valid
// JSON is reported there rather than treated as a transport failure.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolSchema is one tool as the model sees it. Parameters is JSON Schema, passed
// through unchanged: what a vendor accepts is the vendor's business, and rewriting the
// schema here would mean re-deriving it in every provider.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Message is one turn as a provider receives it.
//
// It is deliberately narrower than the transcript this program keeps
// (internal/session, TDD §6.3). A message that has been folded, hidden from the user
// or bounded for the model is still a Message by the time it is sent; the bookkeeping
// that produced those states belongs to the store, and Role and ToolCall are the
// vocabulary the two share.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`
	// ToolCalls is what the assistant asked for. A turn may carry text as well, which
	// is why one is not exclusive of the other.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID refers to the call a tool result answers, for the protocols that
	// require the pairing.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Name is the tool a result came from, which some protocols want in addition to
	// the id and others do not.
	Name string `json:"name,omitempty"`
}

// Request is one completion request, in the terms every protocol shares.
//
// It holds what a request needs today and nothing an argument could postpone: the
// reasoning-effort knob and the response-format hint arrive with E3-T8 and E3-T5,
// where a vendor's behaviour makes their shape concrete.
type Request struct {
	Messages []Message    `json:"messages"`
	Tools    []ToolSchema `json:"tools,omitempty"`
	// Temperature carries no omitempty on purpose: zero is a value — the setting that
	// asks for the most predictable answer — and omitting it would let a provider
	// substitute its own much higher default. Rule [#11]'s zero-value trap, in the one
	// field where it changes what the model does.
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	// Effort asks the model to think harder or less hard. Unset leaves the choice to the vendor,
	// and which effort a session ends up with is resolved by internal/model before it gets here.
	Effort Effort `json:"effort,omitempty"`
}

// ChunkKind is what one piece of a streamed completion is.
type ChunkKind int

// The kinds, in the order TDD §6.3 fixes. The order is part of the design rather than
// an incidental detail, so TestChunkKindsAreStable pins it: a reorder is a semantic
// change and should be made deliberately rather than by accident.
const (
	// ChunkText is a fragment of the model's answer.
	ChunkText ChunkKind = iota
	// ChunkReasoning is a fragment of its visible reasoning, where a vendor exposes
	// one.
	ChunkReasoning
	// ChunkToolCall is a complete tool call, ready to run.
	ChunkToolCall
	// ChunkUsage reports tokens and cache accounting for the request.
	ChunkUsage
	// ChunkDone ends the stream. It is the last value sent, and the channel is closed
	// after it.
	ChunkDone
	// ChunkError reports a failure that happened after the stream began.
	ChunkError
)

// Chunk is one piece of a streamed completion. Errors travel in it rather than only through
// Stream's error return, which was consumed at call time — so a failure after the first token has
// nowhere else to go, and a truncated answer would be silent.
//
// It is not JSON: chunks never leave this process, and the event log is the durable record.
type Chunk struct {
	Kind     ChunkKind
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Err      error
}

// Usage is what one request cost, in the terms a provider reports and this program has
// to account for: tokens, how much of the prompt was served from cache, and why the
// model stopped.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// The cache counts are separate from the input tokens rather than a subset, because
	// they are what makes a cost estimate possible at all: a cached token is charged
	// differently, and a prompt that is re-sent every round would otherwise look like a
	// fresh expense each time.
	CacheHitTokens  int `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens int `json:"cache_miss_tokens,omitempty"`
	// CacheWriteTokens is what it cost to *put* tokens in the cache. It is not a subset of the input
	// count any more than a hit is: a write is billed differently from a read, and a vendor that
	// charges for one and not the other would otherwise look free. Only some vendors report it, so
	// zero here means "not reported" as often as it means "nothing was written" — a distinction the
	// kinds preserve per vendor and this program cannot recover.
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	// FinishReason is kept in the vendor's own words rather than mapped to an enum. The
	// cases worth acting on are exactly the ones a mapping flattens — a stop for length
	// needs a different follow-up from a stop for completion — and a provider this
	// program has not seen yet should not have its reason silently dropped.
	FinishReason string `json:"finish_reason,omitempty"`
}

// Provider is one configured way of talking to a model.
type Provider interface {
	// Name is the provider entry's own name, for diagnostics, not a value that reaches the wire.
	Name() string
	// Stream starts a completion and returns the chunks as they arrive.
	//
	// The implementation closes the returned channel when the stream ends, success or failure; the
	// caller must not. An error is returned only for a failure before the stream began — after the
	// first chunk the caller is already reading, so later failures arrive as a ChunkError.
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}
