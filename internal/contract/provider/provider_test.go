package provider_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
)

// roundTrip marshals a value and unmarshals it into a fresh one of the same type,
// asserting nothing was lost on the way. It is generic because the point is identical
// for every type here — what a provider sends and what this program recorded have to
// survive a boundary — and six copies of the same four lines would hide that they are
// one check.
//
// Fixtures use compact JSON: json.Marshal re-encodes a json.RawMessage, so a fixture
// with incidental whitespace would not compare equal to itself and the test would be
// measuring the encoder rather than the type.
func roundTrip[T any](t *testing.T, value T) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshalling %#v: %v", value, err)
	}
	into := *new(T)
	if err := json.Unmarshal(body, &into); err != nil {
		t.Fatalf("unmarshalling %s: %v", body, err)
	}
	if !reflect.DeepEqual(value, into) {
		t.Errorf("a round trip changed the value:\nbefore %#v\n after %#v\n json  %s", value, into, body)
	}
}

// TestMessageRoundTrips covers the four shapes a message takes, since the empty-ish
// ones are where a field tag is most likely to be wrong: a message with no tool calls
// must not gain any on the way back.
func TestMessageRoundTrips(t *testing.T) {
	t.Parallel()
	cases := map[string]provider.Message{
		"a system turn": {Role: provider.RoleSystem, Content: "you are careful with files"},
		"a user turn":   {Role: provider.RoleUser, Content: "what does go.mod say?"},
		"an assistant turn that only calls tools": {
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"go.mod"}`},
				{ID: "call_2", Name: "grep", Arguments: `{"pattern":"func"}`},
			},
		},
		"an assistant turn with text and a call": {
			Role:      provider.RoleAssistant,
			Content:   "checking",
			ToolCalls: []provider.ToolCall{{ID: "call_3", Name: "ls", Arguments: `{}`}},
		},
		"a tool result": {
			Role:       provider.RoleTool,
			ToolCallID: "call_1",
			Name:       "read_file",
			Content:    "module pretty-reasonix",
		},
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			roundTrip(t, message)
		})
	}
}

// TestArgumentsStayRawJSONInsideAString pins the deliberate double encoding: the model's
// arguments are JSON that travels as a JSON string, which is what the OpenAI-shaped
// protocols expect and what makes accumulating a streamed argument possible.
func TestArgumentsStayRawJSONInsideAString(t *testing.T) {
	t.Parallel()
	call := provider.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"go.mod"}`}

	body, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(body), `"arguments":"{\"path\":\"go.mod\"}"`) {
		t.Errorf("arguments are not a JSON string holding JSON: %s", body)
	}
}

// TestToolSchemaPassesTheSchemaThrough: the schema is JSON Schema, and rewriting it here
// would mean every provider re-deriving what the tool already declared.
func TestToolSchemaPassesTheSchemaThrough(t *testing.T) {
	t.Parallel()
	schema := provider.ToolSchema{
		Name:        "read_file",
		Description: "Read a file.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}
	roundTrip(t, schema)

	body, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(body), `"required":["path"]`) {
		t.Errorf("the schema was not passed through unchanged: %s", body)
	}
}

// TestRequestRoundTrips checks the whole request, since that is what a provider
// actually receives.
func TestRequestRoundTrips(t *testing.T) {
	t.Parallel()
	request := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are careful"},
			{Role: provider.RoleUser, Content: "read go.mod"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"go.mod"}`}}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "module pretty-reasonix"},
		},
		Tools: []provider.ToolSchema{{
			Name:        "read_file",
			Description: "Read a file.",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		Temperature: 0.2,
		MaxTokens:   4096,
	}
	roundTrip(t, request)
}

// TestAZeroTemperatureIsNotOmitted is the small trap this struct is shaped around: zero
// is the setting that asks for the most predictable answer, so a request that omits it
// lets the provider substitute its own much higher default. Rule [#11]'s zero-value
// trap, in the one field where it changes what the model does.
func TestAZeroTemperatureIsNotOmitted(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(body), `"temperature":0`) {
		t.Errorf("a zero temperature was dropped from %s, so the provider would choose its own default", body)
	}
}

// TestARequestWithoutToolsOmitsThem, and the control that they are present when there
// are some: an empty list sent to a vendor reads as "no tools", which is the same as
// omitting it, so the tag only matters for the bytes.
func TestARequestWithoutToolsOmitsThem(t *testing.T) {
	t.Parallel()
	message := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}

	bare, err := json.Marshal(provider.Request{Messages: message})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(bare), "tools") {
		t.Errorf("a request with no tools mentions them: %s", bare)
	}

	armed, err := json.Marshal(provider.Request{
		Messages: message,
		Tools:    []provider.ToolSchema{{Name: "ls", Parameters: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(armed), `"tools":[{"name":"ls"`) {
		t.Errorf("a request with a tool does not name it: %s", armed)
	}
}

// TestAPlainMessageOmitsTheToolFields: the empty case is where a tag is most likely to
// be wrong, and a message that gained an empty tool_calls array would be read as an
// assistant turn that asked for nothing.
func TestAPlainMessageOmitsTheToolFields(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, field := range []string{"tool_calls", "tool_call_id", "name"} {
		if strings.Contains(string(body), field) {
			t.Errorf("a plain message carries %s: %s", field, body)
		}
	}
}

// TestUsageRoundTrips, including the partial shape: a vendor that reports no cache
// figures is ordinary, and the zero counts must not become negative or be invented.
func TestUsageRoundTrips(t *testing.T) {
	t.Parallel()
	roundTrip(t, provider.Usage{
		InputTokens:     12000,
		OutputTokens:    300,
		CacheHitTokens:  11000,
		CacheMissTokens: 1000,
		ReasoningTokens: 120,
		FinishReason:    "stop",
	})
	roundTrip(t, provider.Usage{InputTokens: 5, OutputTokens: 1})
}

// TestAFailureAfterTheFirstTokenTravelsInTheStream is the construction the interface's
// error return cannot express: a dropped connection mid-answer arrives as a chunk, and
// the same error value has to come back out so a caller can match it.
func TestAFailureAfterTheFirstTokenTravelsInTheStream(t *testing.T) {
	t.Parallel()
	cut := errors.New("the connection was cut")

	chunk := provider.Chunk{Kind: provider.ChunkError, Err: cut}

	if chunk.Kind != provider.ChunkError {
		t.Errorf("Kind = %v, want ChunkError", chunk.Kind)
	}
	if !errors.Is(chunk.Err, cut) {
		t.Error("the chunk does not carry the error that ended the stream")
	}
	if chunk.ToolCall != nil || chunk.Usage != nil || chunk.Text != "" {
		t.Errorf("an error chunk carries payload as well: %#v", chunk)
	}
}

// TestChunkKindsAreStable pins the order TDD §6.3 fixes. The numbers travel with the
// kinds wherever they are compared or recorded, so a reorder is a semantic change and
// should break a test rather than pass unnoticed.
func TestChunkKindsAreStable(t *testing.T) {
	t.Parallel()
	documented := []struct {
		kind provider.ChunkKind
		want int
	}{
		{provider.ChunkText, 0},
		{provider.ChunkReasoning, 1},
		{provider.ChunkToolCallStart, 2},
		{provider.ChunkToolCallArgsDelta, 3},
		{provider.ChunkToolCall, 4},
		{provider.ChunkUsage, 5},
		{provider.ChunkDone, 6},
		{provider.ChunkError, 7},
	}
	for _, entry := range documented {
		if int(entry.kind) != entry.want {
			t.Errorf("chunk kind %d is at %d, want %d: TDD §6.3 fixes the order", entry.kind, int(entry.kind), entry.want)
		}
	}
}

// TestTheRolesAreTheValuesThatReachTheWire. A provider maps roles onto its own
// vocabulary, but the OpenAI-shaped kinds use these strings unchanged, so they are a
// wire value and not merely an internal label.
func TestTheRolesAreTheValuesThatReachTheWire(t *testing.T) {
	t.Parallel()
	documented := map[provider.Role]string{
		provider.RoleSystem:    "system",
		provider.RoleUser:      "user",
		provider.RoleAssistant: "assistant",
		provider.RoleTool:      "tool",
	}
	for role, want := range documented {
		if string(role) != want {
			t.Errorf("role %q, want %q", role, want)
		}
	}
}
