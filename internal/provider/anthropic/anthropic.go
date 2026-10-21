// Package anthropic implements the provider kind that speaks the Messages API over an event
// stream.
//
// It is the second wire format behind the same contract, and it differs structurally rather than
// cosmetically: the system prompt is a top-level field, content is a list of typed blocks, tool
// results are a block in a user turn, and a reasoning effort is a token budget instead of a name.
// Absorbing that is what the contract was for.
//
// It is registered as kind = "anthropic" by internal/assembly and never by itself (rule [#3]).
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
)

const (
	// apiVersion is the protocol revision this kind speaks. The vendor requires the header on every
	// request and version-gates its shapes on it, so it is a constant rather than a setting.
	apiVersion = "2023-06-01"
	// messagesPath is appended to the resolved root.
	messagesPath = "/v1/messages"
	// versionSegment is what a configured base URL often ends with, and what this kind strips
	// before deriving the path itself.
	versionSegment = "/v1"
	// defaultMaxTokens is used when a request names no cap, because this protocol requires the
	// field. The cap ladder itself belongs to E3-T10.
	defaultMaxTokens = 4096
	// minThinkingBudget is the smallest budget this protocol accepts. A request with less room than
	// this simply does not get thinking, rather than being sent and rejected.
	minThinkingBudget = 1024
	// ephemeral is the only cache-control type the protocol defines.
	ephemeral = "ephemeral"
)

// thinkingBudgets is how this protocol expresses an effort level: a token budget rather than a
// name. The ladder is ours, to be tuned against what a vendor actually does with the budget; what
// matters here is that the mapping is explicit and in one place.
var thinkingBudgets = map[provider.Effort]int{
	provider.EffortMinimal: 1024,
	provider.EffortLow:     2048,
	provider.EffortMedium:  4096,
	provider.EffortHigh:    8192,
	provider.EffortMax:     16384,
}

// Kind is one configured provider speaking the Messages API.
type Kind struct {
	name        string
	root        string
	model       string
	apiKey      func() string
	promptCache bool
	client      *httpclient.Client
}

// New is the factory internal/assembly registers for kind = "anthropic".
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New(`kind "anthropic" needs a base_url`)
	}
	if cfg.Model == "" {
		return nil, errors.New(`kind "anthropic" needs a model`)
	}
	return &Kind{
		name:        cfg.Name,
		root:        resolveRoot(cfg.BaseURL),
		model:       cfg.Model,
		apiKey:      cfg.APIKey,
		promptCache: cfg.PromptCache,
		client: httpclient.New(httpclient.Config{
			ResponseHeaderTimeout: cfg.IdleTimeout,
			StreamIdleTimeout:     cfg.IdleTimeout,
		}),
	}, nil
}

// resolveRoot turns a configured base URL into the root this kind derives paths from.
//
// A configured URL often ends with the version segment — https://api.anthropic.com/v1 — because
// that is how the chat-completions kind wants it. Here it is stripped and re-derived, so the same
// configuration works for either kind and a user who pastes the wrong shape does not get a request
// to /v1/v1/messages.
func resolveRoot(baseURL string) string {
	root := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), versionSegment)
	return strings.TrimRight(root, "/")
}

// Name is the provider entry's own name.
func (k *Kind) Name() string { return k.name }

// endpoint is where this kind sends its requests, which is the base URL resolved through
// resolveRoot.
func (k *Kind) endpoint() string { return k.root + messagesPath }

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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, k.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building the request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	// Canonical case, which is what go puts on the wire anyway: the vendor documents these
	// lowercase, but http header names are case-insensitive.
	request.Header.Set("Anthropic-Version", apiVersion)
	request.Header.Set("X-Api-Key", key)

	events, err := k.client.Stream(ctx, request)
	if err != nil {
		return nil, err
	}

	chunks := make(chan provider.Chunk)
	go func() {
		defer close(chunks)
		defer func() { _ = events.Close() }()
		k.fold(ctx, events, chunks)
	}()
	return chunks, nil
}

// encodeRequest renders a request in the vendor's shape.
func (k *Kind) encodeRequest(req provider.Request) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	request := messagesRequest{
		Model:       k.model,
		MaxTokens:   maxTokens,
		Messages:    make([]turn, 0, len(req.Messages)),
		Tools:       make([]tool, 0, len(req.Tools)),
		Temperature: req.Temperature,
		Thinking:    thinkingFor(req.Effort, maxTokens),
		Stream:      true,
	}

	// The system prompt becomes a top-level field, joined rather than concatenated blindly: several
	// system messages are ordinary here and the vendor has one field for all of them.
	system := make([]string, 0, 2)
	for _, message := range req.Messages {
		if message.Role == provider.RoleSystem {
			if message.Content != "" {
				system = append(system, message.Content)
			}
			continue
		}
		rendered, err := renderTurn(message)
		if err != nil {
			return nil, err
		}
		request.Messages = append(request.Messages, rendered)
	}
	if len(system) > 0 {
		prompt := systemBlock{Type: "text", Text: strings.Join(system, "\n\n")}
		if k.promptCache {
			prompt.CacheControl = &cacheControl{Type: ephemeral}
		}
		request.System = []systemBlock{prompt}
	}

	for _, schema := range req.Tools {
		mapped := tool{Name: schema.Name, Description: schema.Description, InputSchema: schema.Parameters}
		request.Tools = append(request.Tools, mapped)
	}
	if k.promptCache && len(request.Tools) > 0 {
		// The tools and the system prompt are the prefix that repeats on every turn of a session,
		// which is where a cache breakpoint earns its keep. This protocol allows four; two suffice.
		request.Tools[len(request.Tools)-1].CacheControl = &cacheControl{Type: ephemeral}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding the request: %w", err)
	}
	return body, nil
}

// renderTurn maps one contract message onto the vendor's blocks.
func renderTurn(message provider.Message) (turn, error) {
	rendered := turn{Role: string(message.Role)}

	if message.Role == provider.RoleTool {
		// A tool result is not its own role here: it is a block inside a user turn.
		rendered.Role = string(provider.RoleUser)
		rendered.Content = []block{{
			Type:      "tool_result",
			ToolUseID: message.ToolCallID,
			Content:   message.Content,
		}}
		return rendered, nil
	}
	if message.Content != "" {
		rendered.Content = append(rendered.Content, block{Type: "text", Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		// This protocol wants the arguments as an object, where chat completions wanted the raw
		// string. A call whose arguments never became valid JSON cannot be sent, and saying so here
		// is better than sending an empty object the model would act on.
		input := json.RawMessage(call.Arguments)
		if len(input) == 0 || !json.Valid(input) {
			return turn{}, fmt.Errorf("tool call %q has arguments that are not JSON: %q", call.Name, call.Arguments)
		}
		rendered.Content = append(rendered.Content, block{
			Type: "tool_use", ID: call.ID, Name: call.Name, Input: input,
		})
	}
	if len(rendered.Content) == 0 {
		// A turn with nothing in it is not sendable, and an empty content list is rejected.
		rendered.Content = []block{{Type: "text", Text: ""}}
	}
	return rendered, nil
}

// thinkingFor turns an effort level into this protocol's budget, or nothing at all.
//
// Two clamps matter. The protocol requires a budget below max_tokens, so a small cap shrinks the
// budget; and below the protocol's minimum there is no room to think at all, so the request is
// sent without rather than rejected.
func thinkingFor(effort provider.Effort, maxTokens int) *thinking {
	budget, known := thinkingBudgets[effort]
	if !known {
		return nil // unset, or a level this kind has no budget for
	}
	budget = min(budget, maxTokens-1)
	if budget < minThinkingBudget {
		return nil
	}
	return &thinking{Type: "enabled", BudgetTokens: budget}
}

// fold reads the vendor's events and emits chunks until the stream ends or the caller gives up.
func (*Kind) fold(ctx context.Context, events *httpclient.Events, chunks chan<- provider.Chunk) {
	emit := func(chunk provider.Chunk) bool {
		select {
		case chunks <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}

	folding := newFolder()
	done := false
	emitDone := func() {
		if !done {
			done = true
			emit(provider.Chunk{Kind: provider.ChunkDone})
		}
	}

	for {
		event, err := events.Next()
		if errors.Is(err, io.EOF) {
			emitDone()
			return
		}
		if err != nil {
			emit(provider.Chunk{Kind: provider.ChunkError, Err: err})
			return
		}
		over, foldErr := folding.event(event.Data, emit)
		if foldErr != nil {
			emit(provider.Chunk{Kind: provider.ChunkError, Err: foldErr})
			return
		}
		if over {
			emitDone()
			return
		}
	}
}

// folder accumulates one stream into this program's chunks.
//
// Tool calls are identified by their content-block index, and a block is complete when the vendor
// says so with content_block_stop — which is a firmer signal than a finish reason, because it names
// the block rather than the turn.
type folder struct {
	blocks map[int]*pendingBlock
	usage  provider.Usage
}

type pendingBlock struct {
	id        string
	name      string
	arguments strings.Builder
}

func newFolder() *folder { return &folder{blocks: make(map[int]*pendingBlock, 2)} }

// event folds one payload and reports whether the vendor said the stream is over.
func (f *folder) event(payload string, emit func(provider.Chunk) bool) (bool, error) {
	var event streamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false, fmt.Errorf("decoding a stream event: %w", err)
	}

	switch event.Type {
	case "message_start":
		if event.Message != nil {
			f.usage.InputTokens = event.Message.Usage.InputTokens
			f.usage.CacheHitTokens = event.Message.Usage.CacheReadInputTokens
		}
	case "content_block_start":
		if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			f.blocks[event.Index] = &pendingBlock{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
		}
	case "content_block_delta":
		if event.Delta == nil {
			break
		}
		switch event.Delta.Type {
		case "text_delta":
			if !emit(provider.Chunk{Kind: provider.ChunkText, Text: event.Delta.Text}) {
				return true, nil
			}
		case "thinking_delta":
			if !emit(provider.Chunk{Kind: provider.ChunkReasoning, Text: event.Delta.Thinking}) {
				return true, nil
			}
		case "input_json_delta":
			if pending, known := f.blocks[event.Index]; known {
				pending.arguments.WriteString(event.Delta.PartialJSON)
			}
		}
	case "content_block_stop":
		if pending, known := f.blocks[event.Index]; known {
			delete(f.blocks, event.Index)
			if !emit(provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: pending.id, Name: pending.name, Arguments: pending.arguments.String(),
			}}) {
				return true, nil
			}
		}
	case "message_delta":
		if event.Delta != nil {
			f.usage.FinishReason = event.Delta.StopReason
		}
		if event.Usage != nil {
			f.usage.OutputTokens = event.Usage.OutputTokens
			f.usage.CacheHitTokens = max(f.usage.CacheHitTokens, event.Usage.CacheReadInputTokens)
		}
		if !emit(provider.Chunk{Kind: provider.ChunkUsage, Usage: &f.usage}) {
			return true, nil
		}
	case "message_stop":
		return true, nil
	case "error":
		if event.Error != nil {
			return true, fmt.Errorf("the provider rejected the request: %s", event.Error.Message)
		}
		return true, errors.New("the provider ended the stream with an error")
	case "ping":
		// A keep-alive, which is what the idle watchdog exists to be fed by.
	}
	return false, nil
}
