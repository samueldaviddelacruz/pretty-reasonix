package agent

import (
	"fmt"
	"strings"
)

// Bounds on the recovery record.
//
// Bounded because the record is folded into the next turn, which is the one thing a person did not ask to
// pay for. Small enough to be cheap, large enough to say what was being attempted — and a constant rather
// than a literal so raising it is a decision rather than an edit.
const (
	// RecoveryInputBytes is how much of the interrupted request is repeated back.
	RecoveryInputBytes = 400
	// RecoveryProgressLines is how many steps of what had happened are kept.
	RecoveryProgressLines = 8
	// RecoveryProgressBytes is how much of each step is kept.
	RecoveryProgressBytes = 200
)

// Recovery is what the next turn is told about a turn that did not finish.
//
// A cancelled turn leaves work half done — a file written, a search run, a decision reached and not acted
// on. Saying so is the difference between the next message starting from where the last one stopped and
// starting from amnesia, and it is bounded so that the memory does not become the next turn's cost.
type Recovery struct {
	// TurnID is which turn this is about, so a person reading the transcript can match the two.
	TurnID string
	// Input is what was asked, bounded.
	Input string
	// Progress is what had happened, in order, bounded in both count and size.
	Progress []string
	// Cancelled says the turn was stopped rather than broken: the difference decides whether the next turn
	// should resume the work or start over.
	Cancelled bool
	// Err is why a turn failed, when it failed rather than being stopped.
	Err error
}

// Render turns the record into the block a host appends to the next turn.
func (r Recovery) Render() string {
	var b strings.Builder
	reason := "was interrupted"
	if !r.Cancelled && r.Err != nil {
		reason = "failed"
	}
	fmt.Fprintf(&b, "The previous turn %s. What it was working on:\n", reason)
	if r.Input != "" {
		fmt.Fprintf(&b, "- asked: %s\n", bound(r.Input, RecoveryInputBytes))
	}
	if len(r.Progress) > 0 {
		kept := r.Progress
		omitted := 0
		if len(kept) > RecoveryProgressLines {
			omitted = len(kept) - RecoveryProgressLines
			// The last steps are the ones worth keeping: what it was doing when it stopped is more useful
			// than what it did first.
			kept = kept[omitted:]
		}
		b.WriteString("- had got as far as:\n")
		for _, step := range kept {
			fmt.Fprintf(&b, "  - %s\n", bound(step, RecoveryProgressBytes))
		}
		if omitted > 0 {
			fmt.Fprintf(&b, "  - (%d earlier step(s) not shown)\n", omitted)
		}
	}
	if !r.Cancelled && r.Err != nil {
		fmt.Fprintf(&b, "- stopped with: %s\n", bound(r.Err.Error(), RecoveryProgressBytes))
	}
	b.WriteString("Continue from there rather than starting again, and say so if the interrupt left something inconsistent.\n")
	return b.String()
}

// bound shortens a string to at most limit bytes, keeping the start, and marks that it did.
func bound(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Trimmed on a rune boundary rather than mid-rune: a block of half a character is the kind of thing
	// that makes a transcript look corrupted rather than truncated.
	cut := limit
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
