package responses

import "encoding/json"

// The Responses API wire shape, only as much of it as this program sends or reads.

// apiRequest is one completion request.
type apiRequest struct {
	Model string `json:"model"`
	// Instructions is the system prompt, which this protocol keeps in a field of its own.
	Instructions string `json:"instructions,omitempty"`
	// Input is the conversation as flat items rather than turns.
	Input     []item     `json:"input"`
	Tools     []tool     `json:"tools,omitempty"`
	Reasoning *reasoning `json:"reasoning,omitempty"`
	// Temperature carries no omitempty: zero is a value, and dropping it lets the vendor pick its own
	// much higher default. Every kind says this the same way for the same reason.
	Temperature float64 `json:"temperature"`
	MaxOutput   int     `json:"max_output_tokens,omitempty"`
	Stream      bool    `json:"stream"`
	// Store is false on every request. That is the stateless mode: the vendor keeps nothing, so the
	// local transcript is the whole conversation.
	Store bool `json:"store"`
}

// reasoning carries the effort level, under a field of its own rather than at the top.
type reasoning struct {
	Effort string `json:"effort"`
}

// item is one entry in the conversation. Type is empty for a message and names the kind of item
// otherwise, which is how this protocol keeps a tool call beside the conversation rather than
// inside it.
type item struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// function_call and function_call_output
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// tool is one tool as the vendor describes it. The schema field is `parameters` here, as it is for
// chat completions and unlike the Messages API's `input_schema`.
type tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// streamEvent is one server-sent payload. The protocol names each event in the payload as well as in
// the stream, which is what makes one struct enough.
type streamEvent struct {
	Type     string       `json:"type"`
	Delta    string       `json:"delta"`
	Message  string       `json:"message"`
	Item     *item        `json:"item"`
	Response *apiResponse `json:"response"`
}

// apiResponse is the object inside the terminal events.
type apiResponse struct {
	Status string `json:"status"`
	Usage  usage  `json:"usage"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// usage is what the request cost. The cache and reasoning counts are nested here, unlike chat
// completions where one is flat and the other nested — which is why each kind reads its own shape.
type usage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}
