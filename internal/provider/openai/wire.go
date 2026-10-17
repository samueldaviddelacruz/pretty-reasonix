package openai

import "encoding/json"

// The chat-completions wire shape, only as much of it as this program sends or reads.
//
// Hand-written rather than generated because it is a subset: a field this program never sets is a
// field it cannot be surprised by, and a vendor adding one to a response is already tolerated by
// the decoder ignoring what it does not know.

// chatRequest is one completion request.
type chatRequest struct {
	Model       string         `json:"model"`
	Messages    []chatMessage  `json:"messages"`
	Tools       []chatTool     `json:"tools,omitempty"`
	Temperature float64        `json:"temperature"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Stream      bool           `json:"stream"`
	StreamOpts  *streamOptions `json:"stream_options,omitempty"`
	// ReasoningEffort is the wire field this protocol uses for the resolved level, empty when
	// nothing asked for one.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// streamOptions asks the vendor to report usage on the stream, which it otherwise only does for
// a non-streamed request.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage is one turn of the conversation as the vendor expects it.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// chatToolCall is a call the assistant asked for, in the request and in a reply.
type chatToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function chatFunction `json:"function"`
}

// chatFunction carries a call's name and its arguments as a JSON string, which is why this
// program keeps arguments as a string too: the fragments are concatenated before they are JSON.
type chatFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// chatTool is one tool as the vendor describes it to the model.
type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolSpec `json:"function"`
}

type chatToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// chatChunk is one streamed piece of the reply.
type chatChunk struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
	Error   *chatError   `json:"error"`
}

type chatChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

// chatDelta is what changed in this piece. Reasoning arrives in its own field, which is the one
// thing vendors differ on most: this program reads the DeepSeek spelling and ignores the rest
// rather than guessing at each vendor's.
type chatDelta struct {
	Content          string              `json:"content"`
	ReasoningContent string              `json:"reasoning_content"`
	ToolCalls        []chatToolCallDelta `json:"tool_calls"`
}

// chatToolCallDelta is one fragment of one call. Index is the only thing identifying which call a
// fragment belongs to, which is why the accumulator is keyed on it.
type chatToolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

// chatUsage is what the request cost. The cache counts are DeepSeek's spelling, which is reported
// flat, and the reasoning count is OpenAI's, which is nested; both are read because a vendor that
// reports one usually reports the other under the same name.
type chatUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	CompletionDetails     struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// chatError is the vendor refusing the request inside a 200 response, which some do mid-stream.
type chatError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}
