// Package model resolves what a session actually asks of a model: which provider and model a
// reference names, how much context and output it can take, what a request costs, and which
// reasoning effort applies.
//
// It sits above the contract layer and below the loop, so it may read configuration and speak
// the provider vocabulary, but it holds no transport and no state.
package model

import (
	"errors"
	"fmt"
	"slices"

	"pretty-reasonix/internal/contract/provider"
)

// ErrUnsupportedEffort reports an effort the chosen model does not offer.
var ErrUnsupportedEffort = errors.New("the model does not support that reasoning effort")

// EffortFor decides which effort a request should carry.
//
// The three inputs are the three places it can come from, and the order between them is the
// interesting part: an explicit request wins, then the model's own default, then nothing at all —
// which leaves the choice to the vendor.
//
// A request the model does not support is an error rather than a quiet fall back to the default,
// and that is the whole point of this function. Falling back would let someone ask for maximum
// effort, get the ordinary level, and never learn: the answer would simply be less thorough than
// they paid for. Both the requested and the configured default are checked, because a default that
// names an unsupported level is a configuration defect that would otherwise only surface
// intermittently — on the sessions that do not override it.
func EffortFor(requested provider.Effort, supported []string, defaultEffort string) (provider.Effort, error) {
	offered, err := parseLevels(supported)
	if err != nil {
		return provider.EffortUnset, fmt.Errorf("supported_efforts: %w", err)
	}
	fallback, err := provider.ParseEffort(defaultEffort)
	if err != nil {
		return provider.EffortUnset, fmt.Errorf("default_effort: %w", err)
	}
	if fallback.IsSet() && !slices.Contains(offered, fallback) {
		return provider.EffortUnset, fmt.Errorf("%w: default_effort is %q and the model offers %s",
			ErrUnsupportedEffort, fallback, describe(offered))
	}

	if !requested.IsSet() {
		return fallback, nil
	}
	if len(offered) == 0 {
		return provider.EffortUnset, fmt.Errorf("%w: %q was asked for and this model has no effort setting",
			ErrUnsupportedEffort, requested)
	}
	if !slices.Contains(offered, requested) {
		return provider.EffortUnset, fmt.Errorf("%w: %q was asked for and the model offers %s",
			ErrUnsupportedEffort, requested, describe(offered))
	}
	return requested, nil
}

// parseLevels validates what a configuration file claims a model supports.
//
// A list is allowed to be empty, which means the vendor takes no effort knob at all. What it may
// not be is unreadable: a typo in supported_efforts would otherwise silently shrink the offered
// set and turn every request for that level into an error with no clue why.
func parseLevels(names []string) ([]provider.Effort, error) {
	levels := make([]provider.Effort, 0, len(names))
	for _, name := range names {
		level, err := provider.ParseEffort(name)
		if err != nil {
			return nil, err
		}
		if !level.IsSet() {
			continue
		}
		levels = append(levels, level)
	}
	return levels, nil
}

// describe renders a set of levels for an error message, in ladder order rather than as configured.
func describe(levels []provider.Effort) string {
	ordered := slices.Clone(levels)
	slices.SortFunc(ordered, func(a, b provider.Effort) int {
		return rank(a) - rank(b)
	})
	names := make([]string, 0, len(ordered))
	for _, level := range ordered {
		names = append(names, string(level))
	}
	return fmt.Sprint(names)
}

// rank positions a level on the ladder, and -1 for anything off it.
func rank(level provider.Effort) int {
	return slices.Index(provider.Efforts(), level)
}
