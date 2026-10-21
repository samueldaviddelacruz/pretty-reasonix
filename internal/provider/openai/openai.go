// Package openai implements the provider kind that speaks chat completions over an event stream,
// which is the shape most vendors imitate and several vendors are.
//
// It is registered as kind = "openai" by internal/assembly and never by itself (rule [#3]).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
	"pretty-reasonix/internal/provider/stream"
)

// endpointPath is appended to a provider's base URL. The base names the vendor and its version
// segment — https://api.openai.com/v1 — so what is left to append is the resource.
const endpointPath = "/chat/completions"

// streamDone is the marker a vendor sends instead of a final chunk.
const streamDone = "[DONE]"

// Kind is one configured provider speaking chat completions.
type Kind struct {
	name    string
	baseURL string
	model   string
	apiKey  func() string
	client  *httpclient.Client
}

// New is the factory internal/assembly registers for kind = "openai".
//
// It rejects a configuration it cannot use rather than failing at the first request, because the
// mistake is in a configuration file and the person who can fix it is reading boot output.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New(`kind "openai" needs a base_url`)
	}
	if cfg.Model == "" {
		return nil, errors.New(`kind "openai" needs a model`)
	}
	return &Kind{
		name:    cfg.Name,
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client: httpclient.New(httpclient.Config{
			ResponseHeaderTimeout: cfg.IdleTimeout,
			StreamIdleTimeout:     cfg.IdleTimeout,
		}),
	}, nil
}

// Name is the provider entry's own name.
func (k *Kind) Name() string { return k.name }

// Stream sends the request and folds the vendor's stream into this program's chunks.
//
// The error return covers everything that can go wrong before the first chunk: a request that
// cannot be built, a missing credential, an HTTP refusal. After that the chunks carry the news,
// including a failure, because the caller is already reading.
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
		// Deliberately without the key in the message: an error travels.
		return nil, fmt.Errorf("provider %q has no api key configured", k.name)
	}

	body, err := k.encodeRequest(req)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+endpointPath, bytes.NewReader(body))
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
	request := chatRequest{
		Model:           k.model,
		Messages:        make([]chatMessage, 0, len(req.Messages)),
		Tools:           make([]chatTool, 0, len(req.Tools)),
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		Stream:          true,
		StreamOpts:      &streamOptions{IncludeUsage: true},
		ReasoningEffort: string(req.Effort),
	}
	for _, message := range req.Messages {
		mapped := chatMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			Name:       message.Name,
		}
		for _, call := range message.ToolCalls {
			mapped.ToolCalls = append(mapped.ToolCalls, chatToolCall{
				ID:       call.ID,
				Type:     "function",
				Function: chatFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		request.Messages = append(request.Messages, mapped)
	}
	for _, tool := range req.Tools {
		request.Tools = append(request.Tools, chatTool{
			Type:     "function",
			Function: chatToolSpec{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters},
		})
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding the request: %w", err)
	}
	return body, nil
}

// folder accumulates one stream's deltas into this program's chunks.
//
// Tool calls arrive as fragments interleaved by index — the first fragment of the second call can
// arrive before the last fragment of the first — so they are held until the vendor says the turn
// is finished and then emitted whole. That is deliberate: a call is decided on as a whole by the
// permission engine, and a half-parsed argument string is not something to hand anybody.
type folder struct {
	calls map[int]*pendingCall
	// finishedReason is the vendor's own word from finish_reason, kept for the usage chunk.
	finishedReason string
	finished       bool
}

// pendingCall is one call being assembled.
type pendingCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func newFolder() *folder {
	return &folder{calls: make(map[int]*pendingCall, 4)}
}

// Fold folds one payload. It reports that the vendor said the stream is over at its marker, which
// is this protocol's own way of saying so and the reason the shared loop takes the signal rather
// than knowing the marker itself.
func (f *folder) Fold(payload string, emit func(provider.Chunk) bool) (bool, error) {
	if strings.TrimSpace(payload) == streamDone {
		return true, nil
	}
	var chunk chatChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return false, fmt.Errorf("decoding a stream chunk: %w", err)
	}
	if chunk.Error != nil {
		// Returned rather than emitted: the caller of this function emits a ChunkError and stops
		// without a Done, because a stream that failed did not finish.
		return true, fmt.Errorf("the provider rejected the request: %s", chunk.Error.Message)
	}

	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue // this program asks for one completion, so only the first choice is read
		}
		if choice.Delta.Content != "" {
			if !emit(provider.Chunk{Kind: provider.ChunkText, Text: choice.Delta.Content}) {
				return true, nil
			}
		}
		if choice.Delta.ReasoningContent != "" {
			if !emit(provider.Chunk{Kind: provider.ChunkReasoning, Text: choice.Delta.ReasoningContent}) {
				return true, nil
			}
		}
		for _, fragment := range choice.Delta.ToolCalls {
			f.accumulate(fragment)
		}
		if choice.FinishReason != "" {
			f.finished, f.finishedReason = true, choice.FinishReason
			f.emitCalls(emit)
		}
	}

	if chunk.Usage != nil {
		usage := chunk.Usage.toUsage(f.finishedReason)
		if !emit(provider.Chunk{Kind: provider.ChunkUsage, Usage: &usage}) {
			return true, nil
		}
	}
	return f.finished && chunk.Usage != nil, nil
}

// accumulate folds one fragment into the call its index names.
func (f *folder) accumulate(fragment chatToolCallDelta) {
	call, known := f.calls[fragment.Index]
	if !known {
		call = &pendingCall{}
		f.calls[fragment.Index] = call
	}
	// The first fragment carries the identity; later ones carry only arguments, and a vendor that
	// repeats the name is not doing anything wrong.
	if fragment.ID != "" {
		call.id = fragment.ID
	}
	if fragment.Function.Name != "" {
		call.name = fragment.Function.Name
	}
	call.arguments.WriteString(fragment.Function.Arguments)
}

// emitCalls hands over every assembled call, in the order the vendor indexed them.
func (f *folder) emitCalls(emit func(provider.Chunk) bool) {
	// Sorted rather than iterated by position: the vendor's indices are dense today, and a loop bound
	// derived from the map's length would silently drop a call if one ever were not.
	for _, index := range slices.Sorted(maps.Keys(f.calls)) {
		call, known := f.calls[index]
		if !known {
			continue
		}
		if !emit(provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: call.arguments.String(),
		}}) {
			return
		}
	}
	f.calls = make(map[int]*pendingCall, 4)
}

// toUsage renders the vendor's accounting in this program's terms.
func (u chatUsage) toUsage(finishReason string) provider.Usage {
	return provider.Usage{
		InputTokens:     u.PromptTokens,
		OutputTokens:    u.CompletionTokens,
		CacheHitTokens:  u.PromptCacheHitTokens,
		CacheMissTokens: u.PromptCacheMissTokens,
		ReasoningTokens: u.CompletionDetails.ReasoningTokens,
		FinishReason:    finishReason,
	}
}
