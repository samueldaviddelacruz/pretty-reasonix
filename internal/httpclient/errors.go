package httpclient

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"
)

// ErrUnrepeatable reports a request that cannot be sent again — a body with no way to recreate
// it. A sentinel because retrying that would ask a different question, not a transport blip.
var ErrUnrepeatable = errors.New("the request body cannot be re-sent")

// StatusError is a response the server returned instead of an answer.
//
// It carries facts — status, retryability, how long to wait — rather than a sentence, because the
// caller that retries is not the caller that explains.
type StatusError struct {
	// Method and URL identify the call, so every wrap site does not have to.
	Method string
	URL    string
	// Code is the HTTP status and Status its standard text.
	Code   int
	Status string
	// Retryable reports whether this status is one another attempt can plausibly fix.
	Retryable bool
	// RetryAfter is what the server asked for, clamped to the ceiling; zero when it asked for nothing.
	RetryAfter time.Duration
	// Body is a bounded prefix of the response: a vendor's explanation is usually in it, and an
	// unbounded read is its own failure mode.
	Body string
}

// Error renders the facts. It never includes a request header or a body we sent, since
// those are where a credential would be.
func (e *StatusError) Error() string {
	detail := ""
	if excerpt := strings.TrimSpace(e.Body); excerpt != "" {
		detail = ": " + excerpt
	}
	return fmt.Sprintf("%s %s: %s%s", e.Method, e.URL, e.Status, detail)
}

// Retryable reports whether an error is worth another attempt, so a caller can decide without
// parsing a message: a 5xx is the server saying "not now", a refused connection is the network
// saying the same, and a 401 is the credential saying no.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	if status, found := errors.AsType[*StatusError](err); found {
		return status.Retryable
	}
	if errors.Is(err, ErrUnrepeatable) {
		return false
	}
	return transientNetworkFailure(err)
}

// transientNetworkFailure reports whether a transport-level failure is one that asking
// again can plausibly fix.
//
// A name that does not resolve is deliberately in the "no" column: a DNS failure caused
// by a typo or a missing entry will not start working because we asked ten more times,
// and the retry ladder would turn a fast, obvious error into a slow one. A DNS *timeout*
// is the opposite — the resolver was busy, not wrong — and is retried.
func transientNetworkFailure(err error) bool {
	if dns, found := errors.AsType[*net.DNSError](err); found {
		return dns.IsTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, interrupted := range []error{
		io.ErrUnexpectedEOF,
		syscall.ECONNRESET,
		syscall.ECONNREFUSED,
		syscall.EPIPE,
	} {
		if errors.Is(err, interrupted) {
			return true
		}
	}
	return false
}
