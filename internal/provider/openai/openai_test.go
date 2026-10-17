package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
	"pretty-reasonix/internal/provider/openai"
)

// recorded is what the vendor received, guarded because the handler runs on the server's
// goroutine and the test reads it afterwards — under `-race` that difference is visible.
type recorded struct {
	mu     sync.Mutex
	body   []byte
	auth   string
	method string
}

func (r *recorded) note(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.body, r.auth, r.method = body, request.Header.Get("Authorization"), request.Method
}

func (r *recorded) get() ([]byte, string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body, r.auth, r.method
}

// serve answers with a recorded event stream, which is how every case below gets its response: the
// fixtures are the vendor's own shapes, so a change in how this program reads them shows up here
// rather than at a real endpoint.
func serve(t *testing.T, status int, fixture string) (*httptest.Server, *recorded) {
	t.Helper()
	seen := &recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.note(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, fixture)
	}))
	t.Cleanup(server.Close)
	return server, seen
}

// kind builds the provider against a test server, with a key so the request is accepted.
func kind(t *testing.T, baseURL string) provider.Provider {
	t.Helper()
	built, err := openai.New(provider.Config{
		Name:        "vendor",
		BaseURL:     baseURL,
		Model:       "vendor/model",
		APIKey:      func() string { return "test-key" },
		IdleTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return built
}

// collect drains the stream, so a case reads as a list of chunks rather than a loop.
func collect(t *testing.T, chunks <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var read []provider.Chunk
	for chunk := range chunks {
		read = append(read, chunk)
	}
	return read
}

// stream starts a request and returns its chunks.
func stream(t *testing.T, built provider.Provider, request provider.Request) []provider.Chunk {
	t.Helper()
	chunks, err := built.Stream(context.Background(), request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return collect(t, chunks)
}

// kindsIn lists the chunk kinds a case produced, which is what a failure should show first.
func kindsIn(chunks []provider.Chunk) []provider.ChunkKind {
	kinds := make([]provider.ChunkKind, 0, len(chunks))
	for _, chunk := range chunks {
		kinds = append(kinds, chunk.Kind)
	}
	return kinds
}

func TestTheRequestIsSentInTheVendorsShape(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: [DONE]\n\n")

	stream(t, kind(t, server.URL), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are careful"},
			{Role: provider.RoleUser, Content: "read go.mod"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"go.mod"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "module pretty-reasonix"},
		},
		Tools: []provider.ToolSchema{{
			Name: "read_file", Description: "Read a file.",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		Temperature: 0.2,
		MaxTokens:   4096,
		Effort:      provider.EffortHigh,
	})

	body, authorization, method := seen.get()
	if method != http.MethodPost {
		t.Errorf("the request was a %s", method)
	}
	if authorization != "Bearer test-key" {
		t.Errorf("Authorization is %q, want the resolved key", authorization)
	}

	var sent struct {
		Model           string  `json:"model"`
		Stream          bool    `json:"stream"`
		Temperature     float64 `json:"temperature"`
		MaxTokens       int     `json:"max_tokens"`
		ReasoningEffort string  `json:"reasoning_effort"`
		StreamOptions   struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v\n%s", err, body)
	}

	if sent.Model != "vendor/model" {
		t.Errorf("model is %q", sent.Model)
	}
	if !sent.Stream || !sent.StreamOptions.IncludeUsage {
		t.Error("the request does not ask for a stream with usage, so the turn could not be accounted for")
	}
	if sent.Temperature != 0.2 || sent.MaxTokens != 4096 {
		t.Errorf("temperature/max_tokens are %v/%d", sent.Temperature, sent.MaxTokens)
	}
	// The resolved effort reaches the field this protocol uses for it, which is the half of E3-T8
	// that belongs to a kind.
	if sent.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort is %q, want the resolved level", sent.ReasoningEffort)
	}
	if len(sent.Messages) != 4 {
		t.Fatalf("sent %d messages, want 4", len(sent.Messages))
	}
	if calls := sent.Messages[2].ToolCalls; len(calls) != 1 || calls[0].Function.Arguments != `{"path":"go.mod"}` {
		t.Errorf("the assistant turn lost its call: %+v", sent.Messages[2])
	}
	if sent.Messages[3].ToolCallID != "call_1" {
		t.Errorf("the tool result does not name the call it answers: %+v", sent.Messages[3])
	}
	if len(sent.Tools) != 1 || sent.Tools[0].Function.Name != "read_file" ||
		string(sent.Tools[0].Function.Parameters) != `{"type":"object"}` {
		t.Errorf("the tool was not described as a function: %+v", sent.Tools)
	}
}

func TestTextStreamsAcrossDeltas(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"choices":[{"index":0,"delta":{"content":"Well"}}]}

data: {"choices":[{"index":0,"delta":{"content":" hello"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2,"prompt_cache_hit_tokens":8,"completion_tokens_details":{"reasoning_tokens":0}}}

data: [DONE]

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, kind(t, server.URL), provider.Request{})

	var text strings.Builder
	var usage *provider.Usage
	sawDone := false
	for _, chunk := range chunks {
		switch chunk.Kind {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkReasoning, provider.ChunkToolCall, provider.ChunkError,
			provider.ChunkToolCallStart, provider.ChunkToolCallArgsDelta:
			t.Errorf("an unexpected chunk: %v %+v", chunk.Kind, chunk.Err)
		}
	}
	if text.String() != "Well hello" {
		t.Errorf("the answer was %q, want the deltas joined", text.String())
	}
	if usage == nil {
		t.Fatal("the turn was not accounted for")
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 2 || usage.CacheHitTokens != 8 {
		t.Errorf("usage is %+v, want the vendor's numbers", usage)
	}
	if usage.FinishReason != "stop" {
		t.Errorf("finish reason is %q, want the vendor's own word", usage.FinishReason)
	}
	if !sawDone {
		t.Error("the stream never reported that it was done")
	}
}

func TestReasoningStreamsSeparately(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"choices":[{"index":0,"delta":{"reasoning_content":"weighing it"}}]}

data: {"choices":[{"index":0,"delta":{"content":"answered"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, kind(t, server.URL), provider.Request{})

	var reasoning, answer strings.Builder
	for _, chunk := range chunks {
		switch chunk.Kind {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkText:
			answer.WriteString(chunk.Text)
		case provider.ChunkToolCallStart, provider.ChunkToolCallArgsDelta, provider.ChunkToolCall,
			provider.ChunkUsage, provider.ChunkDone, provider.ChunkError:
			// Not what this case is about; the stream, tool-call and error cases cover them.
		}
	}
	if reasoning.String() != "weighing it" {
		t.Errorf("reasoning was %q", reasoning.String())
	}
	if answer.String() != "answered" {
		t.Errorf("the answer was %q", answer.String())
	}
}

// TestToolCallFragmentsAreAccumulatedByIndex is the criterion's core, and the fixture is the case
// that needs it: the fragments of the two calls interleave, so only the index tells them apart.
// Neither is emitted until the vendor says the turn is finished, because a half-parsed argument
// string is not something to hand the permission engine.
func TestToolCallFragmentsAreAccumulatedByIndex(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"grep","arguments":"{\"pat"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"go.mod\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"tern\":\"func\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, kind(t, server.URL), provider.Request{})

	var calls []provider.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == provider.ChunkToolCall {
			calls = append(calls, *chunk.ToolCall)
		}
		if chunk.Kind == provider.ChunkText || chunk.Kind == provider.ChunkReasoning {
			t.Errorf("a tool-call turn produced %v", chunk.Kind)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("emitted %d calls, want 2: %+v", len(calls), calls)
	}
	// In the order the vendor indexed them, not the order the fragments arrived.
	if calls[0].ID != "call_a" || calls[0].Name != "read_file" || calls[0].Arguments != `{"path":"go.mod"}` {
		t.Errorf("the first call is %+v, want read_file with its arguments joined", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].Name != "grep" || calls[1].Arguments != `{"pattern":"func"}` {
		t.Errorf("the second call is %+v, want grep with its arguments joined", calls[1])
	}
}

// TestAnUnfinishedToolCallIsNotEmitted: the other half of "only complete calls are emitted". A
// stream that dies mid-arguments must not hand over a call it cannot vouch for.
func TestAnUnfinishedToolCallIsNotEmitted(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, kind(t, server.URL), provider.Request{})

	for _, chunk := range chunks {
		if chunk.Kind == provider.ChunkToolCall {
			t.Fatalf("a half-parsed call was emitted: %+v", chunk.ToolCall)
		}
	}
}

func TestAnErrorInsideTheStreamIsTyped(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"choices":[{"index":0,"delta":{"content":"starting"}}]}

data: {"error":{"message":"the model is overloaded","type":"server_error"}}

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, kind(t, server.URL), provider.Request{})

	if kinds := kindsIn(chunks); len(kinds) < 2 || kinds[len(kinds)-1] != provider.ChunkError {
		t.Fatalf("the stream ended with %v, want a typed error last", kinds)
	}
	last := chunks[len(chunks)-1]
	if last.Err == nil || !strings.Contains(last.Err.Error(), "overloaded") {
		t.Errorf("the error does not carry the vendor's message: %v", last.Err)
	}
}

func TestARefusalBeforeTheStreamIsAnError(t *testing.T) {
	t.Parallel()
	server, _ := serve(t, http.StatusUnauthorized, "")

	_, err := kind(t, server.URL).Stream(context.Background(), provider.Request{})
	var status *httpclient.StatusError
	if !errors.As(err, &status) {
		t.Fatalf("Stream returned %v, want the status error", err)
	}
	if status.Code != http.StatusUnauthorized {
		t.Errorf("the code is %d", status.Code)
	}
	if status.Retryable {
		t.Error("a refused credential is marked retryable, so the retry ladder would spend it ten times")
	}
}

func TestAMissingKeyIsRefusedWithoutARequest(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: [DONE]\n\n")
	built, err := openai.New(provider.Config{Name: "vendor", BaseURL: server.URL, Model: "vendor/model"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := built.Stream(context.Background(), provider.Request{}); err == nil {
		t.Fatal("a provider with no key accepted a request")
	}
	if body, _, _ := seen.get(); body != nil {
		t.Error("the request was sent anyway, unauthenticated")
	}
}

func TestAConfigurationThatCannotWorkIsRejectedAtBoot(t *testing.T) {
	t.Parallel()
	cases := map[string]provider.Config{
		"no base url": {Model: "vendor/model"},
		"no model":    {BaseURL: "https://api.example.com/v1"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := openai.New(cfg); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

func TestTheKindIsNamedForTheConfigurationEntry(t *testing.T) {
	t.Parallel()
	built, err := openai.New(provider.Config{Name: "my-vendor", BaseURL: "https://api.example.com/v1", Model: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if built.Name() != "my-vendor" {
		t.Errorf("Name() is %q, want the entry's name", built.Name())
	}
}
