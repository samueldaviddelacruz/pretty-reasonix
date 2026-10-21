// Package responses implements the provider kind that speaks the Responses API over an event stream.
//
// It is the third wire format behind the same contract, and the one that differs most in structure:
// the conversation is a flat list of typed items rather than a list of role-carrying messages, a
// tool call is an item of its own rather than part of an assistant turn, the system prompt is an
// `instructions` field, and a reasoning effort is a named level again — but under `reasoning.effort`
// rather than the top level.
//
// It runs statelessly: `store` is false on every request, so the whole conversation is sent each
// time and the vendor keeps nothing between turns. That costs tokens and buys the one property worth
// more here — a session is the local transcript, not something the vendor holds.
//
// It is registered as kind = "responses" by internal/assembly and never by itself (rule [#3]).
package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
	"pretty-reasonix/internal/provider/stream"
)

// resourcePath is appended to a base URL that names only the vendor and its version segment.
const resourcePath = "/responses"

// defaultMaxTokens is used when a request names no cap, so the vendor applies its own default
// rather than this program inventing one. The cap ladder belongs to E3-T10.
const defaultMaxTokens = 0

// Kind is one configured provider speaking the Responses API.
type Kind struct {
	name    string
	baseURL string
	request string
	model   string
	apiKey  func() string
	client  *httpclient.Client
}

// New is the factory internal/assembly registers for kind = "responses".
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New(`kind "responses" needs a base_url`)
	}
	if cfg.Model == "" {
		return nil, errors.New(`kind "responses" needs a model`)
	}
	return &Kind{
		name:    cfg.Name,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		request: resolveEndpoint(cfg.BaseURL, cfg.RequestURL),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client: httpclient.New(httpclient.Config{
			ResponseHeaderTimeout: cfg.IdleTimeout,
			StreamIdleTimeout:     cfg.IdleTimeout,
		}),
	}, nil
}

// resolveEndpoint decides where a request goes.
//
// An override is honoured because a proxy may need a path this kind cannot derive. An override that
// merely repeats the base is discarded instead: it says nothing the base has not already said, and
// treating it as an exact endpoint would drop the resource path and send every request to the
// vendor's root.
func resolveEndpoint(baseURL, requestURL string) string {
	base := strings.TrimRight(baseURL, "/")
	override := strings.TrimRight(requestURL, "/")
	if override == "" || override == base {
		return base + resourcePath
	}
	return override
}

// Name is the provider entry's own name.
func (k *Kind) Name() string { return k.name }

// Stream sends the request and folds the vendor's events into this program's chunks.
//
// The caller must drain the returned channel or cancel ctx. The goroutine that folds the stream
// blocks on the send, so abandoning it without doing one of those leaves it waiting for ever.
func (k *Kind) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	// The key is resolved once: the resolver may read a file, and calling it twice both costs the
	// read and risks sending a different key from the one that passed the check.
	key := ""
	if k.apiKey != nil {
		key = k.apiKey()
	}
	if key == "" {
		return nil, fmt.Errorf("provider %q has no api key configured", k.name)
	}

	body, err := k.encodeRequest(req)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, k.request, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building the request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+key)

	events, err := k.client.Stream(ctx, request)
	if err != nil {
		return nil, err
	}
	return stream.Pump(ctx, events, newFolder()), nil
}

// encodeRequest renders the request in the vendor's shape.
func (k *Kind) encodeRequest(req provider.Request) ([]byte, error) {
	request := apiRequest{
		Model:     k.model,
		Input:     make([]item, 0, len(req.Messages)),
		Tools:     make([]tool, 0, len(req.Tools)),
		Reasoning: reasoningFor(req.Effort),
		Stream:    true,
		// The stateless marker: the vendor keeps nothing, so the local transcript is the whole
		// conversation and a session is not something anybody else holds.
		Store:       false,
		Temperature: req.Temperature,
		MaxOutput:   req.MaxTokens,
	}

	prompts := make([]string, 0, 2)
	for _, message := range req.Messages {
		if message.Role == provider.RoleSystem {
			if message.Content != "" {
				prompts = append(prompts, message.Content)
			}
			continue
		}
		request.Input = append(request.Input, renderMessage(message)...)
	}
	if len(prompts) > 0 {
		request.Instructions = strings.Join(prompts, "\n\n")
	}
	for _, schema := range req.Tools {
		request.Tools = append(request.Tools, tool{
			Type: "function", Name: schema.Name, Description: schema.Description, Parameters: schema.Parameters,
		})
	}
	if request.MaxOutput == defaultMaxTokens {
		request.MaxOutput = 0 // omitted, so the vendor's own cap applies
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding the request: %w", err)
	}
	return body, nil
}

// renderMessage turns one contract message into the items this protocol expects.
//
// A tool call is an item beside the conversation rather than a field on a turn, and a tool result is
// an item of its own too — which is why one message can become several.
func renderMessage(message provider.Message) []item {
	if message.Role == provider.RoleTool {
		return []item{{
			Type:   "function_call_output",
			CallID: message.ToolCallID,
			Output: message.Content,
		}}
	}
	items := make([]item, 0, 1+len(message.ToolCalls))
	if message.Content != "" {
		items = append(items, item{Role: string(message.Role), Content: message.Content})
	}
	for _, call := range message.ToolCalls {
		items = append(items, item{
			Type: "function_call", CallID: call.ID, Name: call.Name, Arguments: call.Arguments,
		})
	}
	return items
}

// effortNames maps the ladder onto the levels this protocol knows.
//
// The ladder has five rungs and the vendor four: `max` is the one that has no counterpart, so it is
// sent as the highest level the vendor does have rather than being dropped. Silently running at a
// lower effort than asked for is the failure E3-T8 exists to prevent, and sending a level the vendor
// would reject is worse than sending the nearest one it accepts.
var effortNames = map[provider.Effort]string{
	provider.EffortMinimal: "minimal",
	provider.EffortLow:     "low",
	provider.EffortMedium:  "medium",
	provider.EffortHigh:    "high",
	provider.EffortMax:     "high",
}

// reasoningFor renders an effort level, or nothing when none was asked for.
func reasoningFor(effort provider.Effort) *reasoning {
	name, known := effortNames[effort]
	if !known {
		return nil
	}
	return &reasoning{Effort: name}
}

// fold accumulates one stream into this program's chunks.
type folder struct {
	usage provider.Usage
}

func newFolder() *folder { return &folder{} }

// Fold folds one payload. It reports the end at the event that says the response is complete, and
// reads the usage from that same event.
func (f *folder) Fold(payload string, emit func(provider.Chunk) bool) (bool, error) {
	if strings.TrimSpace(payload) == "[DONE]" {
		return true, nil
	}
	var event streamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false, fmt.Errorf("decoding a stream event: %w", err)
	}

	switch event.Type {
	case "response.output_text.delta":
		if event.Delta != "" {
			return false, emitOnly(emit, provider.Chunk{Kind: provider.ChunkText, Text: event.Delta})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if event.Delta != "" {
			return false, emitOnly(emit, provider.Chunk{Kind: provider.ChunkReasoning, Text: event.Delta})
		}
	case "response.output_item.done":
		// The item is complete here, which is this protocol's equivalent of a finished content block:
		// the call is emitted whole rather than assembled by the loop.
		if event.Item != nil && event.Item.Type == "function_call" {
			return false, emitOnly(emit, provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: event.Item.CallID, Name: event.Item.Name, Arguments: event.Item.Arguments,
			}})
		}
	case "response.completed":
		if event.Response != nil {
			f.usage.InputTokens = event.Response.Usage.InputTokens
			f.usage.OutputTokens = event.Response.Usage.OutputTokens
			f.usage.CacheHitTokens = event.Response.Usage.InputTokensDetails.CachedTokens
			f.usage.ReasoningTokens = event.Response.Usage.OutputTokensDetails.ReasoningTokens
			f.usage.FinishReason = event.Response.Status
		}
		return true, emitOnly(emit, provider.Chunk{Kind: provider.ChunkUsage, Usage: &f.usage})
	case "response.failed", "error":
		message := "the provider failed the request"
		if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
			message = event.Response.Error.Message
		} else if event.Message != "" {
			message = event.Message
		}
		return true, errors.New(message)
	}
	return false, nil
}

// emitOnly emits one chunk and reports the consumer's state as an error the loop can act on.
func emitOnly(emit func(provider.Chunk) bool, chunk provider.Chunk) error {
	if !emit(chunk) {
		return errConsumerGone
	}
	return nil
}

// errConsumerGone reports that the caller stopped reading, which ends the stream quietly.
var errConsumerGone = errors.New("the consumer stopped reading")
