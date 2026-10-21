package model

import (
	"errors"
	"fmt"

	"pretty-reasonix/internal/contract/provider"
)

// ErrNoOutputRoom reports that the window has no room left for an answer.
var ErrNoOutputRoom = errors.New("the window has no room left for an answer")

// TurnUsage is what one turn has cost, summed across the requests it made.
//
// A turn is several requests — every tool call is another round trip — so the number worth showing a
// person is the total, not the last response's. FinishReason is kept from the *last* request, because
// that is the ending that decided how the turn ended.
type TurnUsage struct {
	Requests     int
	Input        int
	Output       int
	CacheHit     int
	CacheMiss    int
	CacheWrite   int
	Reasoning    int
	FinishReason string
}

// Add folds one response's accounting into the turn.
func (t *TurnUsage) Add(usage provider.Usage) {
	t.Requests++
	t.Input += usage.InputTokens
	t.Output += usage.OutputTokens
	t.CacheHit += usage.CacheHitTokens
	t.CacheMiss += usage.CacheMissTokens
	t.CacheWrite += usage.CacheWriteTokens
	t.Reasoning += usage.ReasoningTokens
	t.FinishReason = usage.FinishReason
}

// Truncated reports whether the turn stopped because an answer hit its output cap.
func (t *TurnUsage) Truncated() bool { return Truncation(t.FinishReason) }

// Truncation reports whether a vendor's finish reason means the answer was cut at its cap.
//
// Three vendors, three words for the same event: "length", "max_tokens" and "incomplete". The
// comparison lives here rather than at each provider's edge because Usage keeps the vendor's own word
// — which is what makes a log readable and a new vendor's reason visible — so recognising all three
// is the price of that fidelity, and it is paid once rather than three times.
//
// The distinction is not cosmetic: a truncated answer is a different outcome from a finished one, and
// a caller that cannot tell them apart will show someone half an answer as though it were whole.
func Truncation(finishReason string) bool {
	switch finishReason {
	case "length", "max_tokens", "incomplete":
		return true
	default:
		return false
	}
}

// OutputCeiling is the output cap to send for one request, clipped against the room left in the
// window.
//
// The clip happens at send time, and deliberately not in the compaction trigger. Compaction decides
// how much *history* to keep; this decides how much room the next *answer* may occupy. Folding the
// two together would make a request for a long answer silently also compact the conversation, so a
// truncation and a compaction would arrive as one event nobody asked for — and the compaction ratio
// would move as a side effect of a max_tokens setting.
//
// A configured cap of zero or less means the vendor's own cap applies, so there is nothing to clip.
func OutputCeiling(configured, window, used int) (int, error) {
	if configured <= 0 {
		return 0, nil
	}
	if window <= 0 {
		// No known window, so nothing to clip against. Guessing one would refuse answers that would
		// have been accepted.
		return configured, nil
	}
	room := window - used
	if room <= 0 {
		return 0, fmt.Errorf("%w: %d of %d already used", ErrNoOutputRoom, used, window)
	}
	return min(configured, room), nil
}
