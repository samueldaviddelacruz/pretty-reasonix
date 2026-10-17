package provider_test

import (
	"slices"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
)

func TestTheEffortLadderIsOrderedLeastToMost(t *testing.T) {
	t.Parallel()
	want := []provider.Effort{
		provider.EffortMinimal,
		provider.EffortLow,
		provider.EffortMedium,
		provider.EffortHigh,
		provider.EffortMax,
	}
	got := provider.Efforts()
	if !slices.Equal(got, want) {
		t.Errorf("Efforts() = %v, want %v in that order", got, want)
	}

	// A copy, because a caller sorting or truncating this would otherwise rearrange the ladder
	// for everybody else in the process.
	got[0] = "clobbered"
	if again := provider.Efforts(); !slices.Equal(again, want) {
		t.Error("Efforts() handed out the ladder itself")
	}
}

func TestParsingAnEffortForgivesCaseAndSpace(t *testing.T) {
	t.Parallel()
	cases := map[string]provider.Effort{
		"high":     provider.EffortHigh,
		"HIGH":     provider.EffortHigh,
		"  High  ": provider.EffortHigh,
		"max":      provider.EffortMax,
		"minimal":  provider.EffortMinimal,
		"":         provider.EffortUnset,
		"   ":      provider.EffortUnset,
	}
	for name, want := range cases {
		got, err := provider.ParseEffort(name)
		if err != nil {
			t.Errorf("ParseEffort(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("ParseEffort(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestAnEffortOutsideTheLadderIsReported: this is the difference between "the user asked for
// something we do not have" and "the user asked for nothing", and collapsing them would hide a
// typo behind a request that quietly ran at a different effort.
func TestAnEffortOutsideTheLadderIsReported(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"highish", "highest", "extreme", "2"} {
		got, err := provider.ParseEffort(name)
		if err == nil {
			t.Errorf("ParseEffort(%q) = %q and no error", name, got)
			continue
		}
		if !strings.Contains(err.Error(), "high") {
			t.Errorf("the error for %q does not list the levels, so it does not help: %v", name, err)
		}
	}
}

func TestAnUnsetEffortSaysSo(t *testing.T) {
	t.Parallel()
	if provider.EffortUnset.IsSet() {
		t.Error("EffortUnset reports itself as set")
	}
	for _, effort := range provider.Efforts() {
		if !effort.IsSet() {
			t.Errorf("%q reports itself as unset", effort)
		}
	}
}
