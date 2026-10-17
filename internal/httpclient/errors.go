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

// ErrUnrepeatable reports a request that cannot be sent again, which is a request with
// a body and no way to recreate it. It is a sentinel because the loop has to tell this
// apart from a transport failure: one means "the network is having a moment", and the
// other means "retrying would ask a different question".
var ErrUnrepeatable = errors.New("the request body cannot be re-sent")

// StatusError is a response the server returned instead of an answer.
//
// It carries the facts a caller decides with — the status, whether another attempt is
// worth making, and how long the server asked us to wait — rather than a finished
// sentence, because the caller that retries is not the caller that explains. TDD §6.4
// keeps the loop's policy and the frontend's wording apart, and this type is where that
// split is enforced: everything here is a fact, and every message is built from them at
// the point somebody reads it.
type StatusError struct {
	// Method and URL say what was requested, so a caller does not have to annotate every
	// wrap site to get a message that identifies the call.
	Method string
	URL    string
	// Code is the HTTP status and Status its standard text.
	Code   int
	Status string
	// Retryable reports whether this status is one another attempt can plausibly fix.
	Retryable bool
	// RetryAfter is what the server asked for, already clamped by the policy's ceiling;
	// zero when it asked for nothing.
	RetryAfter time.Duration
	// Body is a bounded prefix of the response, because a vendor's explanation is
	// usually in it and because reading an error page without a limit is its own
	// failure mode.
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

// Retryable reports whether an error is worth another attempt, so that a caller — the
// turn loop, offering the user a retry rather than a stack trace — can decide without
// parsing a message.
//
// It covers this package's own errors and the transport failures net/http reports. The
// judgement is about what repeating the request would change: a 5xx is the server saying
// "not now", a refused or reset connection is the network saying the same, and a 401 is
// the credential saying no, which asking again will not alter.
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
