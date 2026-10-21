package model_test

import (
	"errors"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
)

// TestATurnSumsItsRequests: a turn is several round trips, so the number worth reporting is the
// total rather than the last response's.
func TestATurnSumsItsRequests(t *testing.T) {
	t.Parallel()
	var turn model.TurnUsage

	turn.Add(provider.Usage{
		InputTokens: 100, OutputTokens: 20, CacheHitTokens: 60,
		CacheMissTokens: 40, CacheWriteTokens: 10, ReasoningTokens: 5, FinishReason: "tool_calls",
	})
	turn.Add(provider.Usage{
		InputTokens: 150, OutputTokens: 30, CacheHitTokens: 140,
		CacheMissTokens: 10, FinishReason: "stop",
	})

	if turn.Requests != 2 {
		t.Errorf("Requests is %d, want 2", turn.Requests)
	}
	if turn.Input != 250 || turn.Output != 50 {
		t.Errorf("input %d output %d, want 250 and 50", turn.Input, turn.Output)
	}
	if turn.CacheHit != 200 || turn.CacheMiss != 50 {
		t.Errorf("cache hit %d miss %d, want 200 and 50", turn.CacheHit, turn.CacheMiss)
	}
	if turn.CacheWrite != 10 || turn.Reasoning != 5 {
		t.Errorf("cache write %d reasoning %d, want 10 and 5", turn.CacheWrite, turn.Reasoning)
	}
	// The last request's ending is the one that decided how the turn ended.
	if turn.FinishReason != "stop" {
		t.Errorf("FinishReason is %q, want the last request's", turn.FinishReason)
	}
	if turn.Truncated() {
		t.Error("a turn that stopped normally was reported as truncated")
	}
}

// TestTruncationRecognisesEveryVendorsWord: three vendors, three words for the same event, and one
// place that knows all of them.
func TestTruncationRecognisesEveryVendorsWord(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"length", "max_tokens", "incomplete"} {
		if !model.Truncation(reason) {
			t.Errorf("%q was not recognised as a truncation", reason)
		}
	}
	for _, reason := range []string{"stop", "end_turn", "tool_calls", "tool_use", "completed", ""} {
		if model.Truncation(reason) {
			t.Errorf("%q was reported as a truncation", reason)
		}
	}
}

// TestATruncatedTurnSaysSo is the half that matters: a caller that cannot tell a truncated answer from
// a finished one will show somebody half an answer as though it were whole.
func TestATruncatedTurnSaysSo(t *testing.T) {
	t.Parallel()
	var turn model.TurnUsage
	turn.Add(provider.Usage{FinishReason: "max_tokens"})
	if !turn.Truncated() {
		t.Error("a turn cut at its cap did not report a truncation")
	}
}

// TestTheCeilingIsClippedAgainstTheRoomLeft is E3-T9's second criterion: the clip happens at send
// time, against the window, and is a separate decision from when to compact.
func TestTheCeilingIsClippedAgainstTheRoomLeft(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		configured, window, used int
		want                     int
		wantErr                  bool
	}{
		"no configured cap leaves the choice to the vendor": {configured: 0, window: 100000, used: 1000, want: 0},
		"an unknown window cannot clip anything":            {configured: 8192, window: 0, used: 1000, want: 8192},
		"plenty of room, so the configured cap stands":      {configured: 8192, window: 100000, used: 1000, want: 8192},
		"the window is the tighter limit":                   {configured: 32768, window: 100000, used: 95000, want: 5000},
		"exactly at the edge":                               {configured: 8192, window: 100000, used: 91808, want: 8192},
		"nothing left at all":                               {configured: 8192, window: 100000, used: 100000, wantErr: true},
		"more used than the window holds":                   {configured: 8192, window: 100000, used: 120000, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := model.OutputCeiling(tc.configured, tc.window, tc.used)
			if tc.wantErr {
				if !errors.Is(err, model.ErrNoOutputRoom) {
					t.Fatalf("error is %v, want ErrNoOutputRoom", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("OutputCeiling: %v", err)
			}
			if got != tc.want {
				t.Errorf("ceiling is %d, want %d", got, tc.want)
			}
		})
	}
}
