package httpclient

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy is the bounded backoff this package applies.
//
// It is a value with the delay as a method rather than arithmetic inside the retry
// loop, for a reason that has nothing to do with tidiness: a ladder computed inline can
// only be tested by making real requests and waiting for real seconds, and a test that
// costs ten seconds is a test somebody deletes. As a value, the ladder is checked
// directly and the loop is checked for obeying it.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries, not the number of retries: 1 means no
	// retry at all, and 0 is read as the default rather than as "never try".
	MaxAttempts int
	// Base is the first delay, and each further attempt doubles it.
	Base time.Duration
	// Cap bounds the doubling.
	Cap time.Duration
	// MaxRetryAfter bounds what a server's Retry-After may impose. See Delay.
	MaxRetryAfter time.Duration
	// Jitter, when set, turns the computed delay into the delay actually waited. Nil
	// means no jitter, which is what a test that needs a known delay passes.
	Jitter func(time.Duration) time.Duration
}

// DefaultRetryPolicy is the policy a client uses unless it is given another.
//
// The numbers are the reference implementation's, which were tuned against real vendors
// rather than derived: ten attempts is roughly what a bad minute looks like, half a
// second to fifteen seconds covers a provider restart without turning a fast failure
// into a slow one, and a minute is as long as a server's own instruction is allowed to
// hold us.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:   10,
		Base:          500 * time.Millisecond,
		Cap:           15 * time.Second,
		MaxRetryAfter: 60 * time.Second,
		Jitter:        fullJitter,
	}
}

// Delay returns how long to wait before the attempt following the given 1-based attempt
// number.
//
// A server's Retry-After wins over the ladder when it sends one, because the server
// knows when it will be ready and the ladder is only our guess — and it is clamped to
// MaxRetryAfter, because that header is unauthenticated input, and without a ceiling a
// broken or hostile server could hold the request for hours by asking for a day.
func (p RetryPolicy) Delay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, p.MaxRetryAfter)
	}
	delay := min(p.Base, p.Cap)
	for i := 1; i < attempt && delay < p.Cap; i++ {
		delay = min(delay*2, p.Cap)
	}
	if p.Jitter != nil {
		delay = p.Jitter(delay)
	}
	return delay
}

// fullJitter spreads a delay over the whole interval rather than adding a small amount
// to a fixed one.
//
// That is what stops a fleet of clients which failed together from returning together.
// The herd is the reason backoff carries randomness at all, and spreading over the whole
// interval scatters it as widely as the interval allows.
func fullJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	// G404 is right that this is not a cryptographic source, and it does not need to be: the
	// value is a retry delay, where the requirement is that two clients which failed together
	// do not return together, not that the interval be unpredictable. A crypto read per retry
	// would be cost for a property nobody depends on.
	return time.Duration(rand.Int64N(int64(delay))) //nolint:gosec // G404: a retry delay is not a secret
}

// RetryableStatus reports whether a status is one that another attempt can plausibly
// fix.
//
// The set is deliberately narrow. 408 is the server having timed out waiting for us, 429
// is it asking us to slow down, and 5xx is it having failed. Every 4xx that means "your
// request or your credential is wrong" is excluded, because repeating it produces the
// same answer and costs the same tokens — and on a metered provider, retrying a request
// that was rejected on its merits is a way to pay repeatedly for one mistake.
func RetryableStatus(code int) bool {
	switch {
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests:
		return true
	case code >= 500 && code <= 599:
		return true
	default:
		return false
	}
}

// retryAfter reads what a server asked us to wait.
//
// The seconds form is used as given. The HTTP-date form is measured against the
// response's own Date header rather than this machine's clock, which is both more
// correct and simpler: the server is saying "not before this instant", and the instant it
// means is its own, so a client whose clock is wrong reads it exactly as intended — and
// a test can pin it without being able to move time.
//
// A response with no Date header, or one that cannot be read, yields zero: we did not
// understand the instruction, and inventing one is worse than falling back to the
// ladder.
func retryAfter(resp *http.Response) time.Duration {
	value := resp.Header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	served, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return 0
	}
	if wait := when.Sub(served); wait > 0 {
		return wait
	}
	return 0
}
