package provider

import (
	"fmt"
	"slices"
	"strings"
)

// Effort is how hard a model should think before it answers.
//
// An ordered vocabulary rather than a number, so a caller can name a level without knowing what
// any one vendor does with it: the ladder belongs to this program, and each protocol maps it onto
// whatever it has — a named request field, a thinking-token budget, or nothing at all. A number
// would pretend the scales are comparable, and they are not.
type Effort string

const (
	// EffortUnset asks the vendor to decide, which is what an unconfigured session does.
	EffortUnset Effort = ""
	// EffortMinimal is the least a model can be asked to think, for a mechanical edit.
	EffortMinimal Effort = "minimal"
	// EffortLow suits a question whose answer is a line of code.
	EffortLow Effort = "low"
	// EffortMedium is the ordinary working level.
	EffortMedium Effort = "medium"
	// EffortHigh is for a change that has to be reasoned about rather than looked up.
	EffortHigh Effort = "high"
	// EffortMax is the most a vendor will do, and the most expensive.
	EffortMax Effort = "max"
)

// efforts is the ladder from least to most, excluding unset, and the order is the only thing that
// makes "least" and "most" mean anything.
var efforts = []Effort{EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortMax}

// Efforts returns the ladder, least to most, as a copy.
//
// It exists for the two places a caller needs the whole vocabulary rather than one value: the
// help text for --effort, and validating what a configuration file claims a model supports.
func Efforts() []Effort { return slices.Clone(efforts) }

// ParseEffort turns a name into an effort.
//
// Case and surrounding space are forgiven, because the name usually arrives from a shell or a
// hand-edited file and neither is a place to be strict about either. A name outside the ladder is
// an error rather than a silent unset: this is the difference between "the user asked for
// something this program does not have" and "the user asked for nothing", and collapsing the two
// hides a typo behind a request that quietly ran at a different effort.
func ParseEffort(name string) (Effort, error) {
	normalized := Effort(strings.ToLower(strings.TrimSpace(name)))
	if normalized == EffortUnset {
		return EffortUnset, nil
	}
	if !slices.Contains(efforts, normalized) {
		names := make([]string, 0, len(efforts))
		for _, effort := range efforts {
			names = append(names, string(effort))
		}
		return EffortUnset, fmt.Errorf("%q is not an effort: known levels are %s",
			name, strings.Join(names, ", "))
	}
	return normalized, nil
}

// IsSet reports whether an effort names a level, as opposed to asking the vendor to choose.
func (e Effort) IsSet() bool { return e != EffortUnset }
