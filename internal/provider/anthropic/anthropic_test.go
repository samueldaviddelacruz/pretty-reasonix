package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/provider/anthropic"
)

// recorded is what the vendor received, guarded because the handler runs on the server's
// goroutine and the test reads it afterwards.
type recorded struct {
	mu      sync.Mutex
	body    []byte
	headers http.Header
	method  string
	path    string
}

func (r *recorded) note(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.body, r.headers, r.method, r.path = body, request.Header.Clone(), request.Method, request.URL.Path
}

func (r *recorded) get() ([]byte, http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body, r.headers
}

// serve answers with a recorded event stream.
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

func (r *recorded) seenPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path
}

// build makes the kind against a test server, with a key so the request is accepted. Extra
// configuration arrives as the mutation, so each case sets only what it is about.
func build(t *testing.T, baseURL string, adjust func(*provider.Config)) provider.Provider {
	t.Helper()
	cfg := provider.Config{
		Name:        "vendor",
		BaseURL:     baseURL,
		Model:       "vendor/model",
		APIKey:      func() string { return "test-key" },
		IdleTimeout: 5 * time.Second,
	}
	if adjust != nil {
		adjust(&cfg)
	}
	built, err := anthropic.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return built
}

// stream runs a request and drains its chunks.
func stream(t *testing.T, built provider.Provider, request provider.Request) []provider.Chunk {
	t.Helper()
	chunks, err := built.Stream(context.Background(), request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var read []provider.Chunk
	for chunk := range chunks {
		read = append(read, chunk)
	}
	return read
}

// TestTheBaseURLRootIsResolvedAndThePathReDerived is the criterion's first part: this kind derives
// its own version segment, so a base URL written for chat completions works here unchanged.
//
// The assertion is the path the server actually received, which is what makes a doubled /v1
// visible — the kind exposes no accessor for its root, because nothing outside this test needs one.
func TestTheBaseURLRootIsResolvedAndThePathReDerived(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":               "/v1/messages",
		"/":              "/v1/messages",
		"/v1":            "/v1/messages",
		"/v1/":           "/v1/messages",
		"/anthropic":     "/anthropic/v1/messages",
		"/anthropic/v1":  "/anthropic/v1/messages",
		"/anthropic/v1/": "/anthropic/v1/messages",
		"/some/proxy/v1": "/some/proxy/v1/messages",
	}
	for suffix, want := range cases {
		server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
		stream(t, build(t, server.URL+suffix, nil), provider.Request{})
		server.Close()

		if got := seen.seenPath(); got != want {
			t.Errorf("base %q was asked for %q, want %q", suffix, got, want)
		}
	}
}

func TestTheRequestCarriesTheVersionHeaderAndTheKey(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	stream(t, build(t, server.URL, nil), provider.Request{})

	_, headers := seen.get()
	if got := headers.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Errorf("Anthropic-Version is %q, want the protocol revision this kind speaks", got)
	}
	if got := headers.Get("X-Api-Key"); got != "test-key" {
		t.Errorf("X-Api-Key is %q, want the resolved key", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Errorf("an Authorization header was sent as well, which this protocol does not use: %q", got)
	}
}

// TestTheStructuralDifferencesAreAbsorbed is what this kind exists for: the same contract, a
// different shape on the wire.
func TestTheStructuralDifferencesAreAbsorbed(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")

	stream(t, build(t, server.URL, nil), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are careful"},
			{Role: provider.RoleSystem, Content: "and brief"},
			{Role: provider.RoleUser, Content: "read go.mod"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "toolu_1", Name: "read_file", Arguments: `{"path":"go.mod"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "toolu_1", Name: "read_file", Content: "module pretty-reasonix"},
		},
		Tools: []provider.ToolSchema{{
			Name: "read_file", Description: "Read a file.",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		MaxTokens: 512,
	})

	body, _ := seen.get()
	var sent struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		System    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
				Content   string          `json:"content"`
			} `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v\n%s", err, body)
	}

	// The system prompt is a top-level field, joined rather than repeated per message.
	if len(sent.System) != 1 || sent.System[0].Text != "you are careful\n\nand brief" {
		t.Errorf("system is %+v, want the two turns joined into one field", sent.System)
	}
	if !sent.Stream || sent.MaxTokens != 512 {
		t.Errorf("stream/max_tokens are %v/%d", sent.Stream, sent.MaxTokens)
	}
	// The system turns are gone from the message list, which may only carry user and assistant.
	if len(sent.Messages) != 3 {
		t.Fatalf("%d messages sent, want 3 after the system turns were lifted out: %+v", len(sent.Messages), sent.Messages)
	}
	// A tool call is a tool_use block whose input is an object, not the raw string the contract holds.
	call := sent.Messages[1].Content[0]
	if call.Type != "tool_use" || call.ID != "toolu_1" || call.Name != "read_file" {
		t.Errorf("the assistant turn's call is %+v", call)
	}
	if string(call.Input) != `{"path":"go.mod"}` {
		t.Errorf("the call's input is %s, want the arguments as an object", call.Input)
	}
	// A tool result is a block in a user turn rather than a role of its own.
	result := sent.Messages[2]
	if result.Role != "user" || result.Content[0].Type != "tool_result" || result.Content[0].ToolUseID != "toolu_1" {
		t.Errorf("the tool result is %+v, want a tool_result block in a user turn", result)
	}
	// And the schema field has a different name here.
	if len(sent.Tools) != 1 || sent.Tools[0].Name != "read_file" || string(sent.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Errorf("the tool is %+v", sent.Tools)
	}
}

// TestCacheBreakpointsAppearOnlyWhereTheVendorAcceptsThem is the criterion's third part. The field
// is a capability, not a preference: a vendor that does not implement it rejects the request.
func TestCacheBreakpointsAppearOnlyWhereTheVendorAcceptsThem(t *testing.T) {
	t.Parallel()
	request := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are careful"},
			{Role: provider.RoleUser, Content: "hello"},
		},
		Tools: []provider.ToolSchema{{Name: "ls", Parameters: json.RawMessage(`{}`)}},
	}

	countBreakpoints := func(t *testing.T, body []byte) int {
		t.Helper()
		return strings.Count(string(body), `"cache_control":{"type":"ephemeral"}`)
	}

	server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	stream(t, build(t, server.URL, func(cfg *provider.Config) { cfg.PromptCache = true }), request)
	enabled := countBreakpoints(t, mustBody(t, seen))
	// One on the system prefix and one on the last tool: the parts that repeat every turn.
	if enabled != 2 {
		t.Errorf("found %d cache breakpoints with the capability declared, want 2", enabled)
	}

	server, seen = serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	stream(t, build(t, server.URL, nil), request)
	if off := countBreakpoints(t, mustBody(t, seen)); off != 0 {
		t.Errorf("found %d cache breakpoints without the capability, want none: a vendor that does not implement the field rejects the request", off)
	}
}

func mustBody(t *testing.T, seen *recorded) []byte {
	t.Helper()
	body, _ := seen.get()
	return body
}

// TestAnEffortBecomesATokenBudget is where E3-T8's resolved level lands for this protocol.
func TestAnEffortBecomesATokenBudget(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		effort    provider.Effort
		maxTokens int
		want      int // 0 means no thinking block at all
	}{
		"a high effort gets room to think": {effort: provider.EffortHigh, maxTokens: 32768, want: 8192},
		"an ordinary effort gets less":     {effort: provider.EffortMedium, maxTokens: 32768, want: 4096},
		"the budget is clamped by the cap": {effort: provider.EffortHigh, maxTokens: 4096, want: 4095},
		"a cap too small to think in":      {effort: provider.EffortMax, maxTokens: 800, want: 0},
		"no effort asked for":              {effort: provider.EffortUnset, maxTokens: 32768, want: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
			stream(t, build(t, server.URL, nil), provider.Request{Effort: tc.effort, MaxTokens: tc.maxTokens})

			var sent struct {
				Thinking *struct {
					Type         string `json:"type"`
					BudgetTokens int    `json:"budget_tokens"`
				} `json:"thinking"`
			}
			if err := json.Unmarshal(mustBody(t, seen), &sent); err != nil {
				t.Fatalf("the request body is not JSON: %v", err)
			}
			if tc.want == 0 {
				if sent.Thinking != nil {
					t.Errorf("thinking is %+v, want none", sent.Thinking)
					return
				}
				return
			}
			if sent.Thinking == nil {
				t.Fatal("no thinking block was sent")
			}
			if sent.Thinking.Type != "enabled" || sent.Thinking.BudgetTokens != tc.want {
				t.Errorf("thinking is %+v, want an enabled budget of %d", sent.Thinking, tc.want)
			}
		})
	}
}

func TestTextAndThinkingStreamAsSeparateChunks(t *testing.T) {
	t.Parallel()
	const fixture = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":4}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weighing"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}

event: ping
data: {"type":"ping"}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}

event: message_stop
data: {"type":"message_stop"}

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, build(t, server.URL, nil), provider.Request{})

	var reasoning, answer strings.Builder
	var usage *provider.Usage
	sawDone := false
	for _, chunk := range chunks {
		switch chunk.Kind {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkText:
			answer.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkToolCall, provider.ChunkError:
			t.Errorf("an unexpected chunk: %v %+v", chunk.Kind, chunk.Err)
		}
	}
	if reasoning.String() != "weighing" || answer.String() != "answer" {
		t.Errorf("reasoning %q and answer %q, want them kept apart", reasoning.String(), answer.String())
	}
	if usage == nil {
		t.Fatal("the turn was not accounted for")
	}
	if usage.InputTokens != 9 || usage.OutputTokens != 3 || usage.CacheHitTokens != 4 {
		t.Errorf("usage is %+v", usage)
	}
	if usage.FinishReason != "end_turn" {
		t.Errorf("finish reason is %q, want the vendor's own word", usage.FinishReason)
	}
	if !sawDone {
		t.Error("the stream never reported that it was done")
	}
}

// TestAToolCallIsCompleteWhenItsBlockStops: this protocol names the block that finished, which is a
// firmer signal than a finish reason — so the call is emitted as soon as it is whole.
func TestAToolCallIsCompleteWhenItsBlockStops(t *testing.T) {
	t.Parallel()
	const fixture = `event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, build(t, server.URL, nil), provider.Request{})

	var calls []provider.ToolCall
	for _, chunk := range chunks {
		if chunk.Kind == provider.ChunkToolCall {
			calls = append(calls, *chunk.ToolCall)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("emitted %d calls, want 1", len(calls))
	}
	if calls[0].ID != "toolu_1" || calls[0].Name != "read_file" || calls[0].Arguments != `{"path":"go.mod"}` {
		t.Errorf("the call is %+v, want its fragments joined", calls[0])
	}
}

func TestAnUnfinishedBlockIsNotEmitted(t *testing.T) {
	t.Parallel()
	const fixture = `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

`
	server, _ := serve(t, http.StatusOK, fixture)
	for _, chunk := range stream(t, build(t, server.URL, nil), provider.Request{}) {
		if chunk.Kind == provider.ChunkToolCall {
			t.Fatalf("a half-parsed call was emitted: %+v", chunk.ToolCall)
		}
	}
}

func TestAnErrorEventIsTyped(t *testing.T) {
	t.Parallel()
	const fixture = `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"the model is overloaded"}}

`
	server, _ := serve(t, http.StatusOK, fixture)
	chunks := stream(t, build(t, server.URL, nil), provider.Request{})

	last := chunks[len(chunks)-1]
	if last.Kind != provider.ChunkError {
		t.Fatalf("the stream ended with %v, want a typed error", last.Kind)
	}
	if last.Err == nil || !strings.Contains(last.Err.Error(), "overloaded") {
		t.Errorf("the error does not carry the vendor's message: %v", last.Err)
	}
	for _, chunk := range chunks {
		if chunk.Kind == provider.ChunkDone {
			t.Error("a failed stream reported that it was done")
		}
	}
}

// TestAToolCallWithInvalidArgumentsIsRefusedBeforeSending: this protocol wants an object, so a call
// whose arguments never became valid JSON cannot be sent — and saying so beats sending an empty
// object the model would act on.
func TestAToolCallWithInvalidArgumentsIsRefusedBeforeSending(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	built := build(t, server.URL, nil)

	_, err := built.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{
			Role:      provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{ID: "t1", Name: "read_file", Arguments: `{"path":`}},
		}},
	})
	if err == nil {
		t.Fatal("a call with truncated arguments was sent")
	}
	if !strings.Contains(err.Error(), "read_file") {
		t.Errorf("the error does not name the tool: %v", err)
	}
	if body, _ := seen.get(); body != nil {
		t.Error("the request was sent anyway")
	}
}

func TestAMissingKeyIsRefusedWithoutARequest(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	built, err := anthropic.New(provider.Config{Name: "v", BaseURL: server.URL, Model: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := built.Stream(context.Background(), provider.Request{}); err == nil {
		t.Fatal("a provider with no key accepted a request")
	}
	if body, _ := seen.get(); body != nil {
		t.Error("the request was sent anyway, unauthenticated")
	}
}

func TestAConfigurationThatCannotWorkIsRejectedAtBoot(t *testing.T) {
	t.Parallel()
	cases := map[string]provider.Config{
		"no base url": {Model: "vendor/model"},
		"no model":    {BaseURL: "https://api.example.com"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := anthropic.New(cfg); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}
