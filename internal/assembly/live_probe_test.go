//go:build integration

package assembly_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/assembly"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
)

// liveProvider is the first thing in this repository that talks to a real endpoint.
//
// Everything else is verified against scripted providers and recorded fixtures, which proves the shapes and
// proves nothing about whether a vendor accepts them. This test closes that gap for one request: it builds a
// provider from environment variables, runs a turn through the controller, and asserts an answer came back
// and reached the transcript.
//
// It skips unless PRX_PROBE_BASE_URL is set, so the normal suite never reaches the network. To run it:
//
//	PRX_PROBE_BASE_URL=https://api.deepseek.com \
//	PRX_PROBE_MODEL=deepseek-chat \
//	PRX_PROBE_API_KEY=sk-... \
//	go test -tags integration -run TestALiveProviderAnswers ./internal/assembly/ -v
//
// PRX_PROBE_KIND defaults to openai; use anthropic or responses for the others.
// rate limit into a flaky test, and this is the one test whose cost is somebody else's money.
//
//nolint:paralleltest // one live request against a real endpoint: running it beside others would turn a
func TestALiveProviderAnswers(t *testing.T) {
	base := os.Getenv("PRX_PROBE_BASE_URL")
	if base == "" {
		t.Skip("PRX_PROBE_BASE_URL not set: this test talks to a real provider and never runs by accident")
	}
	kind := os.Getenv("PRX_PROBE_KIND")
	if kind == "" {
		kind = "openai"
	}
	modelName := os.Getenv("PRX_PROBE_MODEL")
	if modelName == "" {
		t.Fatalf("PRX_PROBE_BASE_URL is set but PRX_PROBE_MODEL is not")
	}
	// A local endpoint needs no key; a hosted one will refuse without it, and that refusal is a result
	// rather than a reason to fail earlier.
	key := os.Getenv("PRX_PROBE_API_KEY")

	// The kinds are registered by internal/assembly and nowhere else, so a probe that a person runs by hand
	// needs the same wiring the frontend will: without this the registry is empty and provider.New says so.
	if err := assembly.RegisterProviders(); err != nil {
		t.Fatalf("registering the provider kinds: %v", err)
	}

	live, err := provider.New(kind, provider.Config{
		Name:        "probe",
		BaseURL:     base,
		Model:       modelName,
		APIKey:      func() string { return key },
		IdleTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("building a %q provider: %v", kind, err)
	}

	sink := &liveSink{t: t}
	transcript := &session.Transcript{}
	c, err := agent.NewController(agent.Deps{
		Provider:   live,
		Transcript: transcript,
		Sink:       sink,
		Policy: agent.Policy{
			// A cap, so this test costs what it says it costs rather than whatever the vendor's
			// default output budget happens to be. Without it the probe is cheap by luck.
			Model:  model.Model{Provider: kind, Name: modelName, MaxOutputTokens: 64},
			Effort: provider.EffortUnset,
			Preset: agent.PresetBalanced,
		},
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	control, err := c.Run(ctx, agent.TurnInput{Text: "Reply with exactly one line: PROBE-OK"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-control.Done():
	case <-ctx.Done():
		t.Fatalf("the turn did not finish within the deadline; the events so far are above")
	}

	var answered bool
	for _, message := range c.Messages() {
		if message.Role == provider.RoleAssistant && strings.Contains(message.Content, "PROBE-OK") {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("the turn completed without a usable answer; transcript: %+v", c.Messages())
	}
	if sink.failure != "" {
		t.Errorf("the provider reported a failure: %s", sink.failure)
	}
}

// liveSink reports what the engine says, so a failure has a story rather than a message.
type liveSink struct {
	t       *testing.T
	failure string
}

func (s *liveSink) Emit(e event.Event) {
	switch typed := e.(type) {
	case event.Notice:
		s.t.Logf("notice [%s] %s", typed.Code, typed.Text)
		if typed.Level == event.NoticeError {
			s.failure = typed.Text
		}
	case event.TurnStarted:
		s.t.Logf("turn %s started", typed.TurnID)
	case event.MessageCommitted:
		s.t.Logf("committed %d bytes of answer", len(typed.Message.Content))
	case event.UsageReported:
		s.t.Logf("usage: in=%d out=%d cached=%d", typed.Usage.InputTokens, typed.Usage.OutputTokens, typed.Usage.CacheHitTokens)
	case event.TurnDone:
		s.t.Logf("turn %s ended %s", typed.TurnID, typed.Status)
	}
}
