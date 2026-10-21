package responses_test

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
	"pretty-reasonix/internal/provider/responses"
)

// recorded is what the vendor received, guarded because the handler runs on the server's goroutine.
type recorded struct {
	mu     sync.Mutex
	body   []byte
	path   string
	header http.Header
}

func (r *recorded) note(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.body, r.path, r.header = body, request.URL.Path, request.Header.Clone()
}

func (r *recorded) get() ([]byte, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body, r.path
}

// serve answers with a recorded event stream.
func serve(t *testing.T, fixture string) (*httptest.Server, *recorded) {
	t.Helper()
	seen := &recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.note(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fixture)
	}))
	t.Cleanup(server.Close)
	return server, seen
}

// build makes the kind against a test server, with a key so the request is accepted.
func build(t *testing.T, baseURL, requestURL string) provider.Provider {
	t.Helper()
	built, err := responses.New(provider.Config{
		Name:        "vendor",
		BaseURL:     baseURL,
		RequestURL:  requestURL,
		Model:       "vendor/model",
		APIKey:      func() string { return "test-key" },
		IdleTimeout: 5 * time.Second,
	})
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

const stopFixture = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{}}}\n\n"

// TestWhereTheRequestGoes is the criterion's second half: an override is honoured, and one that
// merely repeats the base is discarded so the resource path survives.
func TestWhereTheRequestGoes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		requestURL string
		want       string
	}{
		"no override, so the kind derives the path":            {"", "/v1/responses"},
		"an override repeating the base is discarded":          {"BASE", "/v1/responses"},
		"an override with a trailing slash repeating the base": {"BASE/", "/v1/responses"},
		"a real override is honoured":                          {"EXACT/exact/endpoint", "/exact/endpoint"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, seen := serve(t, stopFixture)
			override := tc.requestURL
			if rest, found := strings.CutPrefix(override, "BASE"); found {
				override = server.URL + "/v1" + rest
			}
			if rest, found := strings.CutPrefix(override, "EXACT"); found {
				override = server.URL + rest
			}
			// The base always names the version segment, as a real configuration does.
			stream(t, build(t, server.URL+"/v1", override), provider.Request{})
			if _, got := seen.get(); got != tc.want {
				t.Errorf("the request went to %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheRequestIsStatelessAndShapedLikeTheVendors is the criterion's first half plus the structural
// mapping: nothing is stored on the vendor, and the conversation is flat items.
func TestTheRequestIsStatelessAndShapedLikeTheVendors(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, stopFixture)
	stream(t, build(t, server.URL+"/v1", ""), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are careful"},
			{Role: provider.RoleUser, Content: "read go.mod"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"go.mod"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "module pretty-reasonix"},
		},
		Tools:     []provider.ToolSchema{{Name: "read_file", Description: "Read a file.", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Effort:    provider.EffortHigh,
		MaxTokens: 2048,
	})

	body, _ := seen.get()
	var sent struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Store        bool   `json:"store"`
		Stream       bool   `json:"stream"`
		MaxOutput    int    `json:"max_output_tokens"`
		Reasoning    *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Input []struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"input"`
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("the request body is not JSON: %v\n%s", err, body)
	}

	// Stateless: the vendor is told to keep nothing, so the local transcript is the conversation.
	if sent.Store {
		t.Error("store is true, so the vendor would keep the conversation between turns")
	}
	if strings.Contains(string(body), "previous_response_id") {
		t.Error("the request continues a stored response, which is not the stateless mode")
	}
	if !sent.Stream || sent.MaxOutput != 2048 {
		t.Errorf("stream/max_output_tokens are %v/%d", sent.Stream, sent.MaxOutput)
	}
	if sent.Instructions != "you are careful" {
		t.Errorf("instructions is %q, want the system prompt lifted out", sent.Instructions)
	}
	if sent.Reasoning == nil || sent.Reasoning.Effort != "high" {
		t.Errorf("reasoning is %+v, want the resolved level under its own field", sent.Reasoning)
	}
	// The user turn, then one item for the call and one for its result: the assistant turn carried no
	// text, so it produces nothing of its own.
	if len(sent.Input) != 3 {
		t.Fatalf("%d input items, want 3: %+v", len(sent.Input), sent.Input)
	}
	if sent.Input[1].Type != "function_call" || sent.Input[1].CallID != "call_1" {
		t.Errorf("the tool call item is %+v", sent.Input[1])
	}
	if sent.Input[2].Type != "function_call_output" || sent.Input[2].Content != "" {
		t.Errorf("the tool result item is %+v", sent.Input[2])
	}
	if len(sent.Tools) != 1 || sent.Tools[0].Type != "function" || sent.Tools[0].Name != "read_file" {
		t.Errorf("the tool is %+v", sent.Tools)
	}
}

// TestTheTopOfTheLadderMapsToTheHighestLevelTheVendorHas: five rungs here, four there, and the gap
// is closed toward the vendor's maximum rather than silently downward.
func TestTheTopOfTheLadderMapsToTheHighestLevelTheVendorHas(t *testing.T) {
	t.Parallel()
	cases := map[provider.Effort]string{
		provider.EffortMinimal: "minimal",
		provider.EffortLow:     "low",
		provider.EffortMedium:  "medium",
		provider.EffortHigh:    "high",
		provider.EffortMax:     "high",
	}
	for effort, want := range cases {
		server, seen := serve(t, stopFixture)
		stream(t, build(t, server.URL+"/v1", ""), provider.Request{Effort: effort})
		var sent struct {
			Reasoning *struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.Unmarshal(mustBody(t, seen), &sent); err != nil {
			t.Fatalf("the request body is not JSON: %v", err)
		}
		if sent.Reasoning == nil || sent.Reasoning.Effort != want {
			t.Errorf("effort %q was sent as %+v, want %q", effort, sent.Reasoning, want)
		}
	}
}

func mustBody(t *testing.T, seen *recorded) []byte {
	t.Helper()
	body, _ := seen.get()
	return body
}

func TestTextReasoningAndCallsStreamSeparately(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"type":"response.reasoning_summary_text.delta","delta":"weighing"}

data: {"type":"response.output_text.delta","delta":"answer"}

data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}

data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":9,"output_tokens":3,"input_tokens_details":{"cached_tokens":5},"output_tokens_details":{"reasoning_tokens":2}}}}

`
	server, _ := serve(t, fixture)
	chunks := stream(t, build(t, server.URL+"/v1", ""), provider.Request{})

	var reasoning, answer strings.Builder
	var calls []provider.ToolCall
	var usage *provider.Usage
	sawDone := false
	for _, chunk := range chunks {
		switch chunk.Kind {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkText:
			answer.WriteString(chunk.Text)
		case provider.ChunkToolCall:
			calls = append(calls, *chunk.ToolCall)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkDone:
			sawDone = true
		case provider.ChunkError:
			t.Fatalf("an unexpected error: %v", chunk.Err)
		}
	}
	if reasoning.String() != "weighing" || answer.String() != "answer" {
		t.Errorf("reasoning %q and answer %q", reasoning.String(), answer.String())
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Arguments != `{"path":"go.mod"}` {
		t.Errorf("calls are %+v", calls)
	}
	if usage == nil || usage.InputTokens != 9 || usage.OutputTokens != 3 || usage.CacheHitTokens != 5 || usage.ReasoningTokens != 2 {
		t.Errorf("usage is %+v", usage)
	}
	if !sawDone {
		t.Error("the stream never reported that it was done")
	}
}

func TestAFailureIsTypedAndDoesNotReportSuccess(t *testing.T) {
	t.Parallel()
	const fixture = `data: {"type":"response.failed","response":{"status":"failed","error":{"message":"the model is overloaded"}}}

`
	server, _ := serve(t, fixture)
	chunks := stream(t, build(t, server.URL+"/v1", ""), provider.Request{})

	if len(chunks) != 1 || chunks[0].Kind != provider.ChunkError {
		t.Fatalf("the stream produced %d chunks, want one typed error", len(chunks))
	}
	if !strings.Contains(chunks[0].Err.Error(), "overloaded") {
		t.Errorf("the error does not carry the vendor's message: %v", chunks[0].Err)
	}
}

func TestAMissingKeyIsRefusedWithoutARequest(t *testing.T) {
	t.Parallel()
	server, seen := serve(t, stopFixture)
	built, err := responses.New(provider.Config{Name: "v", BaseURL: server.URL + "/v1", Model: "m"})
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
	if _, err := responses.New(provider.Config{Model: "m"}); err == nil {
		t.Error("a provider with no base_url was accepted")
	}
	if _, err := responses.New(provider.Config{BaseURL: "https://api.example.com/v1"}); err == nil {
		t.Error("a provider with no model was accepted")
	}
}
