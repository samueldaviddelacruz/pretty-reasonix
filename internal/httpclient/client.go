package httpclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// The phases this package bounds. There is deliberately no overall http.Client.Timeout;
// see New for why, and docs/GO-STANDARDS.md §7 for the divergence that records it.
const (
	// dialTimeout bounds establishing the connection.
	dialTimeout = 30 * time.Second
	// tlsHandshakeTimeout bounds the handshake, which is a phase a slow or hostile
	// endpoint can stretch while sending nothing.
	tlsHandshakeTimeout = 15 * time.Second
	// keepAlive is the interval of TCP keep-alive probes on an idle connection.
	keepAlive = 30 * time.Second
	// managedResponseHeaderTimeout bounds the wait for response headers.
	//
	// The size is the point rather than a detail: a reasoning model may think for minutes
	// before its first byte, so the conventional thirty seconds would fail every hard
	// question. It matches the reference implementation's idle timeout, and a provider
	// entry can set idle_timeout_seconds (TDD §6.11) for a vendor that differs.
	managedResponseHeaderTimeout = 300 * time.Second
	// maxErrorBodyBytes bounds how much of a failed response is read for its message.
	maxErrorBodyBytes = 8 << 10
	// drainLimit bounds what is read from a response being discarded, so that a small one
	// can have its connection reused for the retry.
	drainLimit = 64 << 10
)

// Config is what a Client is built from.
type Config struct {
	// Transport, when set, is cloned and used as the base. Its dial and handshake
	// timeouts are then the caller's, since supplying a transport is how a caller says it
	// wants to own them.
	Transport *http.Transport
	// Proxy decides what counts as a proxied request. Nil keeps net/http's own
	// ProxyFromEnvironment, which is what makes HTTPS_PROXY, HTTP_PROXY and NO_PROXY work
	// without this package reimplementing them.
	Proxy func(*http.Request) (*url.URL, error)
	// ResponseHeaderTimeout overrides the managed wait for headers, which is where a
	// provider entry's idle_timeout_seconds lands.
	ResponseHeaderTimeout time.Duration
	// Retry is the backoff policy. Nil means DefaultRetryPolicy. It is a pointer so that
	// there is no partially-filled policy to interpret: a caller who wants three attempts
	// starts from DefaultRetryPolicy and changes the field.
	Retry *RetryPolicy
	// Sleep waits between attempts. Nil means a real timer; a test supplies its own so
	// that checking the ladder does not cost the ladder.
	Sleep func(ctx context.Context, delay time.Duration) error
}

// Client performs outbound HTTP with bounded retry.
type Client struct {
	http  *http.Client
	retry RetryPolicy
	sleep func(context.Context, time.Duration) error
}

// New builds a client.
//
// This is the only place in the program that configures outbound HTTP (rule [#81], TDD
// §6.1): provider kinds hold no retry logic and build no transport of their own, so retry,
// backoff, proxy and timeouts are decided once rather than re-decided per vendor.
func New(cfg Config) *Client {
	client := &Client{
		http: &http.Client{
			Transport: managedTransport(cfg),
			// No overall Timeout, and that is a deliberate divergence from rule [#81]
			// rather than an oversight. http.Client.Timeout covers reading the response
			// body, and this client's callers read a stream of tokens that can legitimately
			// take minutes, so an overall deadline would cut a good answer in half.
			// [#81]'s actual complaint — that no phase may wait forever — is met by the
			// transport's dial, handshake and header timeouts, by the stream idle watchdog
			// E3-T4 adds for the body, and by the caller's context. Recorded in
			// docs/GO-STANDARDS.md §7.
		},
		retry: DefaultRetryPolicy(),
		sleep: sleepUntil,
	}
	if cfg.Retry != nil {
		client.retry = *cfg.Retry
	}
	if cfg.Sleep != nil {
		client.sleep = cfg.Sleep
	}
	return client
}

// HTTPClient returns the underlying client.
//
// It exists for the caller that needs net/http directly — E3-T4's event-stream reader
// takes a response over and reads it — so that such a caller gets the configured client
// instead of building a second one. Everything else should go through Do, because Do is
// where the retry policy lives.
func (c *Client) HTTPClient() *http.Client { return c.http }

// Do performs the request, retrying the phases this package owns.
//
// Only the connection and the response headers are retried. Once a response is accepted
// its body belongs to the caller — for a completion that body is a stream of tokens — and
// re-sending after tokens have been delivered would duplicate what the user has already
// read. A body that fails mid-read is E3-T4's StreamInterruptedError, which the loop
// decides about with the user's context in hand.
//
// The caller owns the returned response's body and must close it.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	// The context the caller passed governs, rather than whatever the request was built
	// with: net/http reads the *request's* context, so a caller supplying one here while the
	// request carried another would be a cancellation that silently did nothing.
	req = req.WithContext(ctx)

	// Whether the request can be sent again is settled once, before the first attempt.
	// It cannot be settled later: net/http clears Request.Body once it has sent it, so a
	// request that had a body with no way to recreate it would look bodyless, and the
	// retry would arrive at the server asking a different question.
	repeatable := req.GetBody != nil
	hadBody := req.Body != nil && req.Body != http.NoBody

	// Both retry paths end the same way, so it is written once and named.
	waitThenRewind := func(attempt int, serverAsked time.Duration) error {
		if err := c.sleep(ctx, c.retry.Delay(attempt, serverAsked)); err != nil {
			return fmt.Errorf("waiting to retry %s %s: %w", req.Method, req.URL, err)
		}
		return rewind(req)
	}

	for attempt := 1; ; attempt++ {
		// G704 reads the request URL as tainted, which it is: this package's entire job is to
		// send what its caller asked for. The place to constrain a destination is where the
		// destination is read — a provider entry's base_url or an MCP server's url comes from
		// the user's own configuration, and a tool that fetches a model-supplied URL (E7's
		// web_fetch) is where an allowlist belongs. Putting a host check here would be the
		// wrong layer and would be trivially bypassed by the next caller.
		resp, err := c.http.Do(req) //nolint:gosec // G704: the destination is the caller's configuration, checked where it is read
		if err != nil {
			if attempt >= c.retry.MaxAttempts || !Retryable(err) {
				return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL, err)
			}
			if !repeatable && hadBody {
				return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL, ErrUnrepeatable)
			}
			if retryErr := waitThenRewind(attempt, 0); retryErr != nil {
				return nil, retryErr
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if !RetryableStatus(resp.StatusCode) || attempt >= c.retry.MaxAttempts {
			return nil, c.statusError(req, resp)
		}
		if !repeatable && hadBody {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL, ErrUnrepeatable)
		}

		// Read what the server asked for before discarding the response, then discard it
		// rather than read it: its message is of no use if the next attempt succeeds.
		serverAsked := retryAfter(resp)
		discard(resp.Body)
		_ = resp.Body.Close()
		if retryErr := waitThenRewind(attempt, serverAsked); retryErr != nil {
			return nil, retryErr
		}
	}
}

// statusError turns a response the caller cannot use into the typed error it decides with.
//
// A bounded prefix of the body is read for the message and the body is then closed: the
// status is the information and the excerpt is a courtesy. The read error is deliberately
// not reported, because failing to read an explanation does not change what the status
// means. The URL is redacted, since a request URL can carry a password.
func (c *Client) statusError(req *http.Request, resp *http.Response) *StatusError {
	excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	_ = resp.Body.Close()
	return &StatusError{
		Method:     req.Method,
		URL:        req.URL.Redacted(),
		Code:       resp.StatusCode,
		Status:     resp.Status,
		Retryable:  RetryableStatus(resp.StatusCode),
		RetryAfter: min(retryAfter(resp), c.retry.MaxRetryAfter),
		Body:       string(excerpt),
	}
}

// managedTransport builds the transport this package owns.
//
// A supplied transport is cloned rather than used directly, so that setting a proxy or a
// timeout here cannot reach back and change a transport its owner is still using.
func managedTransport(cfg Config) *http.Transport {
	var transport *http.Transport
	if cfg.Transport != nil {
		transport = cfg.Transport.Clone()
	} else {
		// Cloning net/http's default keeps the two things it knows that reimplementing
		// would get wrong: the proxy function that honours HTTPS_PROXY and NO_PROXY, and
		// the connection-pool sizes. Its timeouts are then replaced, which is the part of
		// it rule [#81] objects to.
		transport = defaultTransport().Clone()
		transport.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}).DialContext
		transport.TLSHandshakeTimeout = tlsHandshakeTimeout
		transport.ResponseHeaderTimeout = managedResponseHeaderTimeout
	}
	if cfg.ResponseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	}
	if cfg.Proxy != nil {
		transport.Proxy = cfg.Proxy
	}
	return transport
}

// defaultTransport returns net/http's default transport, or a fresh one if a program has
// replaced it with something that is not a *http.Transport.
//
// The check is not paranoia: a type assertion on a package-level variable is a panic
// waiting for the one program that assigns to it, and rule [#48] keeps panics out of
// library code — the panic a failed assertion causes included. The fresh transport names
// the proxy function explicitly, because a transport built without one silently ignores
// HTTPS_PROXY.
func defaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

// rewind restores a request body so it can be sent again.
//
// A GetBody of nil is a request with no body here: Do has already refused to retry a
// request that had one and cannot recreate it.
func rewind(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("rewinding the body of %s %s: %w", req.Method, req.URL, err)
	}
	req.Body = body
	return nil
}

// discard reads and throws away a bounded amount of a response body, so that a small one
// can have its connection reused for the next attempt. A body larger than the bound is
// left part-read, and closing a part-read body makes net/http close its connection rather
// than pool it — which is the right trade against transferring a large body for nothing.
func discard(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, drainLimit))
}

// sleepUntil waits for a delay or until the context ends, whichever comes first.
//
// A timer rather than time.Sleep, which this project forbids outright: a wait that cannot
// be interrupted would outlive the caller's cancellation, and the loop would wake to make
// a request nobody is waiting for.
func sleepUntil(ctx context.Context, delay time.Duration) error {
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}
