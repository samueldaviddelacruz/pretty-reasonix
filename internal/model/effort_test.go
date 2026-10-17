package model_test

import (
	"errors"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
)

// TestWhichEffortARequestEndsUpWith is the whole resolution chain in one table: an explicit
// request wins, then the model's default, then nothing at all.
func TestWhichEffortARequestEndsUpWith(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		requested provider.Effort
		supported []string
		fallback  string
		want      provider.Effort
	}{
		"an explicit level the model offers": {
			requested: provider.EffortHigh,
			supported: []string{"low", "medium", "high"},
			want:      provider.EffortHigh,
		},
		"nothing asked, so the model's default applies": {
			supported: []string{"low", "medium", "high"},
			fallback:  "medium",
			want:      provider.EffortMedium,
		},
		"nothing asked and no default, so the vendor decides": {
			supported: []string{"low", "medium", "high"},
			want:      provider.EffortUnset,
		},
		"a model with no effort setting and nothing asked": {
			want: provider.EffortUnset,
		},
		"the request overrides the default": {
			requested: provider.EffortMax,
			supported: []string{"low", "max"},
			fallback:  "low",
			want:      provider.EffortMax,
		},
		"the configured levels are forgiven their case": {
			requested: provider.EffortMedium,
			supported: []string{"LOW", "Medium"},
			want:      provider.EffortMedium,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := model.EffortFor(tc.requested, tc.supported, tc.fallback)
			if err != nil {
				t.Fatalf("EffortFor: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolved to %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAnUnsupportedEffortIsReportedNotDropped is the criterion's second half. A quiet fall back to
// the default would let someone ask for maximum effort, receive the ordinary level, and never
// learn: the answer is simply less thorough than they paid for.
func TestAnUnsupportedEffortIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		requested provider.Effort
		supported []string
		fallback  string
		mentions  []string
	}{
		"a level the model does not offer": {
			requested: provider.EffortMax,
			supported: []string{"low", "medium", "high"},
			mentions:  []string{"max", "low", "high"},
		},
		"a level asked for from a model with no effort setting": {
			requested: provider.EffortHigh,
			mentions:  []string{"high", "no effort setting"},
		},
		"a default naming a level the model does not offer": {
			supported: []string{"low", "high"},
			fallback:  "medium",
			mentions:  []string{"default_effort", "medium"},
		},
		"a default naming a level when the model has no knob at all": {
			fallback: "high",
			mentions: []string{"default_effort", "high"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := model.EffortFor(tc.requested, tc.supported, tc.fallback)
			if err == nil {
				t.Fatalf("resolved to %q with no error", got)
			}
			if !errors.Is(err, model.ErrUnsupportedEffort) {
				t.Errorf("the error does not match ErrUnsupportedEffort: %v", err)
			}
			for _, want := range tc.mentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the message does not mention %q, so it does not say what to change: %v", want, err)
				}
			}
		})
	}
}

// TestAnUnreadableConfigurationIsItsOwnFailure: a typo in supported_efforts must not look like a
// request for an unsupported level, because the fix is in a different file.
func TestAnUnreadableConfigurationIsItsOwnFailure(t *testing.T) {
	t.Parallel()
	_, err := model.EffortFor(provider.EffortHigh, []string{"high", "hgh"}, "")
	if err == nil {
		t.Fatal("an unreadable level was accepted")
	}
	if errors.Is(err, model.ErrUnsupportedEffort) {
		t.Errorf("a configuration typo was reported as an unsupported request: %v", err)
	}
	if !strings.Contains(err.Error(), "supported_efforts") {
		t.Errorf("the error does not say which setting is wrong: %v", err)
	}

	_, err = model.EffortFor(provider.EffortUnset, []string{"low"}, "extreme")
	if err == nil {
		t.Fatal("an unreadable default was accepted")
	}
	if !strings.Contains(err.Error(), "default_effort") {
		t.Errorf("the error does not say which setting is wrong: %v", err)
	}
}

// TestTheOfferedLevelsAreListedInLadderOrder: the message is what someone reads to fix their
// configuration, so it should not depend on the order they happened to write.
func TestTheOfferedLevelsAreListedInLadderOrder(t *testing.T) {
	t.Parallel()
	_, err := model.EffortFor(provider.EffortMax, []string{"high", "low", "medium"}, "")
	if err == nil {
		t.Fatal("max was accepted by a model offering only low, medium and high")
	}
	if !strings.Contains(err.Error(), "[low medium high]") {
		t.Errorf("the levels are not listed in ladder order: %v", err)
	}
}
