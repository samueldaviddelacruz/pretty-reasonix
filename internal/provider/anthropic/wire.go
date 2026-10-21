package anthropic

import "encoding/json"

// The Messages-API wire shape, only as much of it as this program sends or reads.

// messagesRequest is one completion request.
type messagesRequest struct {
	Model string `json:"model"`
	// MaxTokens is required by this protocol, unlike chat completions where it is optional.
	MaxTokens int `json:"max_tokens"`
	// System is a top-level field rather than a message, which is the structural difference from
	// chat completions that this kind exists to absorb. The contract still models it as a system
	// message, because that is what it is to every caller above.
	System   []systemBlock `json:"system,omitempty"`
	Messages []turn        `json:"messages"`
	Tools    []tool        `json:"tools,omitempty"`
	// No omitempty: zero is a value — the most predictable answer — and dropping it lets the vendor
	// substitute its own default. provider.Request says the same thing for the same reason, and the
	// two kinds have to agree or one setting would mean two things.
	Temperature float64 `json:"temperature"`
	// Thinking carries the reasoning effort as a token budget rather than a name.
	Thinking *thinking `json:"thinking,omitempty"`
	Stream   bool      `json:"stream"`
}

// systemBlock is one piece of the system prompt, and the anchor for a cache breakpoint when the
// vendor supports them.
type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// cacheControl marks the longest stable prefix of a request so the vendor can serve it from cache
// on the next turn. Only a vendor that implements it accepts the field: sending it elsewhere is a
// rejected request rather than a no-op.
type cacheControl struct {
	Type string `json:"type"`
}

// thinking asks for reasoning, and this protocol prices it in tokens.
type thinking struct {
	Type string `json:"type"`
	// BudgetTokens is how much the model may spend thinking.
	BudgetTokens int `json:"budget_tokens"`
}

// turn is one message. Content is always blocks here, even for plain text, because a turn that
// carries a tool call or a tool result cannot be expressed as a string.
type turn struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

// block is one content block. It is a union written as one struct: every variant is identified by
// Type, and the unused fields are omitted, which keeps the encoder honest without an interface
// that would have to be satisfied by three types with no behaviour.
type block struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	// breakpoint
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// tool is one tool as the vendor describes it. The schema field is input_schema here, not
// parameters.
type tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

// streamEvent is one server-sent payload. The protocol reuses one envelope for every event type,
// so one struct with the optional parts is the honest shape rather than a union of six.
type streamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`

	Message *struct {
		Usage usage `json:"usage"`
	} `json:"message"`

	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`

	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`

	Usage *usage `json:"usage"`

	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// usage is what the request cost. This vendor reports cache reads and cache writes separately,
// which is more useful than a single hit count: a write is billed differently from a read.
type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}
