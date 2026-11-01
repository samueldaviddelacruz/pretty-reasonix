package provider

// ChunkKind names the state a Chunk is in.
type ChunkKind int

const (
	// ChunkText is answer text.
	ChunkText ChunkKind = iota
	// ChunkReasoning is reasoning text, kept apart from the answer.
	ChunkReasoning
	// ChunkToolCall is one complete tool call, never a fragment.
	ChunkToolCall
	// ChunkUsage is the accounting for the request.
	ChunkUsage
	// ChunkDone is the normal end of a stream.
	ChunkDone
	// ChunkError is a failure, either of the transport or of the vendor.
	ChunkError
)

// Chunk is one piece of a streaming answer — exactly one of the states below.
//
// The shape is deliberate. A struct with a Kind tag and four fields that are only sometimes meaningful
// makes states representable that cannot happen: a text chunk carrying a tool call, a usage chunk
// carrying an error. Every consumer would then have to decide what such a value means, and no two would
// decide the same way. One type per state makes the question unaskable, and Kind stays for logging and
// for a test that wants to say which arrived.
type Chunk interface {
	// Kind names the state of this chunk.
	Kind() ChunkKind
	// chunkKind seals the interface to this package: nothing outside it can claim to be a Chunk, so the
	// set of states is closed where it is declared.
	chunkKind()
}

// TextChunk is answer text. It is separate from ReasoningChunk because a frontend renders them in
// different places, and one chunk that had to be asked which it was would put that decision everywhere.
type TextChunk struct{ Text string }

// Kind is ChunkText.
func (TextChunk) Kind() ChunkKind { return ChunkText }

func (TextChunk) chunkKind() {}

// ReasoningChunk is reasoning text: the model thinking, not the answer.
type ReasoningChunk struct{ Text string }

// Kind is ChunkReasoning.
func (ReasoningChunk) Kind() ChunkKind { return ChunkReasoning }

func (ReasoningChunk) chunkKind() {}

// ToolCallChunk is one complete tool call.
//
// The call is a value rather than a pointer on purpose: a variant that exists is populated, so a consumer
// that has a ToolCallChunk has a call and needs no nil check to say so.
type ToolCallChunk struct{ Call ToolCall }

// Kind is ChunkToolCall.
func (ToolCallChunk) Kind() ChunkKind { return ChunkToolCall }

func (ToolCallChunk) chunkKind() {}

// UsageChunk is the accounting for a request.
type UsageChunk struct{ Usage Usage }

// Kind is ChunkUsage.
func (UsageChunk) Kind() ChunkKind { return ChunkUsage }

func (UsageChunk) chunkKind() {}

// DoneChunk is the normal end of a stream. A failed stream does not send one: reporting an ending and a
// failure together is reporting success after a failure.
type DoneChunk struct{}

// Kind is ChunkDone.
func (DoneChunk) Kind() ChunkKind { return ChunkDone }

func (DoneChunk) chunkKind() {}

// ErrorChunk is a failure, whether the transport broke or the vendor refused. Err is required: a caller
// that has an ErrorChunk is entitled to report it, and a nil here would be a silent failure.
type ErrorChunk struct{ Err error }

// Kind is ChunkError.
func (ErrorChunk) Kind() ChunkKind { return ChunkError }

func (ErrorChunk) chunkKind() {}
