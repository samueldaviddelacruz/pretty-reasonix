package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pretty-reasonix/internal/httpclient"
)

// recorder counts the requests a server saw and can answer each one differently, which is
// what every retry test below is about.
type recorder struct {
	requests atomic.Int64
	// answer decides the status and headers for a given attempt, 1-based.
	answer func(attempt int64, w http.ResponseWriter)
}

func newRecorder(answer func(attempt int64, w http.ResponseWriter)) *recorder {
	return &recorder{answer: answer}
}

func (r *recorder) serve(w http.ResponseWriter, _ *http.Request) {
	r.answer(r.requests.Add(1), w)
}

// answering builds a recorder that returns the given statuses in order, repeating the
// last one once they run out.
func answering(statuses ...int) *recorder {
	return newRecorder(func(attempt int64, w http.ResponseWriter) {
		index := min(int(attempt)-1, len(statuses)-1)
		w.WriteHeader(statuses[index])
	})
}

// testPolicy is the default ladder with the jitter removed and few attempts, so a test can
// assert an exact delay without waiting for one.
func testPolicy(attempts int) *httpclient.RetryPolicy {
	policy := httpclient.DefaultRetryPolicy()
	policy.MaxAttempts = attempts
	policy.Jitter = nil
	return &policy
}

// waits records what the client waited, so the ladder can be asserted rather than endured.
type waits struct {
	delays []time.Duration
}

func (w *waits) sleep(_ context.Context, delay time.Duration) error {
	w.delays = append(w.delays, delay)
	return nil
}

// doErr runs a request that is expected to fail, closing any body the client handed back.
//
// There is nothing to read when Do reports an error, but the body still has to be closed:
// bodyclose is right that a response nobody closes leaves its connection unowned, and it
// cannot see that Do returns nil alongside an error.
//
//nolint:revive // context-as-argument: a test helper takes *testing.T first, as every Go test does; the rule is about threading a context predictably through production call chains, and this is a helper called from seven places in one file
func doErr(t *testing.T, client *httpclient.Client, ctx context.Context, req *http.Request) error {
	t.Helper()
	resp, err := client.Do(ctx, req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestRetryableStatusIsDeliberatelyNarrow(t *testing.T) {
	t.Parallel()
	documented := map[int]bool{
		200: false,
		400: false,
		401: false,
		403: false,
		404: false,
		409: false,
		422: false,
		408: true,
		429: true,
		500: true,
		502: true,
		503: true,
		504: true,
	}
	for code, want := range documented {
		if got := httpclient.RetryableStatus(code); got != want {
			t.Errorf("RetryableStatus(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestTheBackoffLadderDoublesAndStopsGrowing(t *testing.T) {
	t.Parallel()
	policy := httpclient.DefaultRetryPolicy()
	policy.Jitter = nil // the point of the test is the ladder, not the spread

	documented := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 8 * time.Second},
		{6, 15 * time.Second}, // 16s capped
		{20, 15 * time.Second},
	}
	for _, entry := range documented {
		if got := policy.Delay(entry.attempt, 0); got != entry.want {
			t.Errorf("attempt %d waits %v, want %v", entry.attempt, got, entry.want)
		}
	}
}

func TestWhatTheServerAsksForWinsButIsCapped(t *testing.T) {
	t.Parallel()
	policy := httpclient.DefaultRetryPolicy()
	policy.Jitter = nil

	// A server that knows when it will be ready outranks our guess.
	if got := policy.Delay(1, 7*time.Second); got != 7*time.Second {
		t.Errorf("a server asking for 7s got %v", got)
	}
	// And one that asks for a day gets the ceiling: the header is unauthenticated input,
	// and without a bound a broken or hostile server could hold the request for hours.
	if got := policy.Delay(1, 24*time.Hour); got != policy.MaxRetryAfter {
		t.Errorf("a server asking for a day got %v, want the %v ceiling", got, policy.MaxRetryAfter)
	}
	// Zero means the server said nothing, so the ladder applies.
	if got := policy.Delay(3, 0); got != 2*time.Second {
		t.Errorf("no Retry-After waited %v, want the ladder's 2s", got)
	}
}

func TestAFourHundredThatIsNotWorthRepeatingIsNotRepeated(t *testing.T) {
	t.Parallel()
	for _, code := range []int{400, 401, 403, 404, 422} {
		calls := answering(code)
		server := httptest.NewServer(http.HandlerFunc(calls.serve))

		client := httpclient.New(httpclient.Config{Retry: testPolicy(5)})
		err := doErr(t, client, context.Background(), mustRequest(t, http.MethodGet, server.URL))
		server.Close()

		var statusErr *httpclient.StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("status %d produced %v, want a StatusError", code, err)
		}
		if statusErr.Code != code {
			t.Errorf("the error reports %d, want %d", statusErr.Code, code)
		}
		if statusErr.Retryable {
			t.Errorf("status %d is marked retryable", code)
		}
		if got := calls.requests.Load(); got != 1 {
			t.Errorf("status %d was sent %d times; repeating a request that was rejected on its merits pays for it twice", code, got)
		}
	}
}

func TestARetryableStatusIsRepeatedAndThenReported(t *testing.T) {
	t.Parallel()
	calls := answering(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	client := httpclient.New(httpclient.Config{Retry: testPolicy(3), Sleep: (&waits{}).sleep})
	err := doErr(t, client, context.Background(), mustRequest(t, http.MethodGet, server.URL))

	var status *httpclient.StatusError
	if !errors.As(err, &status) {
		t.Fatalf("a server that never recovered produced %v", err)
	}
	if !status.Retryable {
		t.Error("the final error does not say the status was retryable, so a caller cannot offer a retry")
	}
	if got := calls.requests.Load(); got != 3 {
		t.Errorf("the server saw %d requests, want the policy's 3 attempts", got)
	}
}

func TestASuccessAfterARetryIsASuccess(t *testing.T) {
	t.Parallel()
	calls := answering(http.StatusServiceUnavailable, http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	client := httpclient.New(httpclient.Config{Retry: testPolicy(4), Sleep: (&waits{}).sleep})
	resp, err := client.Do(context.Background(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want the recovery to be handed back", resp.StatusCode)
	}
	if got := calls.requests.Load(); got != 2 {
		t.Errorf("the server saw %d requests, want 2", got)
	}
}

func TestTheClientWaitsWhatTheServerAsked(t *testing.T) {
	t.Parallel()
	calls := newRecorder(func(_ int64, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	recorded := &waits{}
	client := httpclient.New(httpclient.Config{Retry: testPolicy(2), Sleep: recorded.sleep})
	if err := doErr(t, client, context.Background(), mustRequest(t, http.MethodGet, server.URL)); err == nil {
		t.Fatal("a 429 that never clears was reported as success")
	}

	if len(recorded.delays) != 1 || recorded.delays[0] != 7*time.Second {
		t.Errorf("the client waited %v, want the single 7s the server asked for", recorded.delays)
	}
}

// TestAnHTTPDateIsMeasuredAgainstTheServersOwnClock pins the reason retryAfter reads the
// response's Date header instead of this machine's clock: the server is saying "not before
// this instant", and the instant it means is its own, so a client whose clock is wrong
// reads it exactly as intended — and a test can pin it without being able to move time.
func TestAnHTTPDateIsMeasuredAgainstTheServersOwnClock(t *testing.T) {
	t.Parallel()
	served := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	// Comfortably inside MaxRetryAfter: the ceiling has its own test above, and mixing the
	// two here would mean this one no longer says what it claims to.
	inFortyFiveSeconds := served.Add(45 * time.Second)

	calls := newRecorder(func(_ int64, w http.ResponseWriter) {
		w.Header().Set("Date", served.Format(http.TimeFormat))
		w.Header().Set("Retry-After", inFortyFiveSeconds.Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	recorded := &waits{}
	client := httpclient.New(httpclient.Config{Retry: testPolicy(2), Sleep: recorded.sleep})
	if err := doErr(t, client, context.Background(), mustRequest(t, http.MethodGet, server.URL)); err == nil {
		t.Fatal("a 503 that never clears was reported as success")
	}

	if len(recorded.delays) != 1 || recorded.delays[0] != 45*time.Second {
		t.Errorf("the client waited %v, want the 45s between the server's Date and its Retry-After", recorded.delays)
	}
}

// TestABodyThatCannotBeRecreatedIsNotRetried is the subtle one. net/http clears
// Request.Body once it has sent it, so a request that had a body with no GetBody would
// look bodyless after the first attempt — and the retry would arrive at the server asking a
// different question, silently.
func TestABodyThatCannotBeRecreatedIsNotRetried(t *testing.T) {
	t.Parallel()
	calls := answering(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	req := mustRequest(t, http.MethodPost, server.URL)
	req.Body = io.NopCloser(strings.NewReader(`{"prompt":"hello"}`))
	req.GetBody = nil // what a streamed body looks like: sendable once

	client := httpclient.New(httpclient.Config{Retry: testPolicy(4), Sleep: (&waits{}).sleep})
	if err := doErr(t, client, context.Background(), req); !errors.Is(err, httpclient.ErrUnrepeatable) {
		t.Errorf("Do returned %v, want ErrUnrepeatable rather than a retry with no body", err)
	}
	if got := calls.requests.Load(); got != 1 {
		t.Errorf("the server saw %d requests; a bodyless retry would have been a different request", got)
	}
}

// TestARetryCarriesTheBodyItCarriedTheFirstTime is the other half of the same property:
// when the body *can* be recreated, the retry must actually carry it.
func TestARetryCarriesTheBodyItCarriedTheFirstTime(t *testing.T) {
	t.Parallel()
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	req := mustRequestWithBody(t, http.MethodPost, server.URL, `{"prompt":"hello"}`)
	client := httpclient.New(httpclient.Config{Retry: testPolicy(3), Sleep: (&waits{}).sleep})
	if err := doErr(t, client, context.Background(), req); err == nil {
		t.Fatal("a 503 that never clears was reported as success")
	}

	if len(bodies) != 3 {
		t.Fatalf("the server saw %d requests, want 3", len(bodies))
	}
	for i, body := range bodies {
		if body != `{"prompt":"hello"}` {
			t.Errorf("attempt %d carried %q, want the body re-sent", i+1, body)
		}
	}
}

// TestTheConfiguredProxyIsUsed runs a real forward proxy: the request must arrive there,
// with an absolute URI, rather than at the origin.
func TestTheConfiguredProxyIsUsed(t *testing.T) {
	t.Parallel()
	var reached atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.IsAbs() {
			reached.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parsing the proxy URL: %v", err)
	}

	client := httpclient.New(httpclient.Config{Proxy: http.ProxyURL(proxyURL)})
	// The origin is deliberately unroutable: if the proxy is not used, nothing answers.
	resp, err := client.Do(context.Background(), mustRequest(t, http.MethodGet, "http://origin.invalid/thing"))
	if err != nil {
		t.Fatalf("the request did not go through the configured proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !reached.Load() {
		t.Error("the proxy was contacted but not as a proxy, so the setting did not take effect")
	}
}

// TestTheTransportIsOursAndNotTheSharedDefault is how "proxy settings honoured" is checked
// without depending on the process environment: net/http caches the environment on first
// use, so a test cannot change HTTPS_PROXY and observe it. What can be observed is that the
// client clones the default transport — which is what carries ProxyFromEnvironment — rather
// than sharing it or building a bare one.
func TestTheTransportIsOursAndNotTheSharedDefault(t *testing.T) {
	t.Parallel()
	client := httpclient.New(httpclient.Config{})

	transport, ok := client.HTTPClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the transport is %T, want a configured *http.Transport", client.HTTPClient().Transport)
	}
	if transport == http.DefaultTransport {
		t.Error("the shared default transport is in use; changing it here would reach every other client in the process")
	}
	if transport.Proxy == nil {
		t.Error("the transport has no proxy function, so HTTPS_PROXY and NO_PROXY are silently ignored")
	}
	if transport.ResponseHeaderTimeout == 0 {
		t.Error("no response-header timeout, so a silent endpoint can hold a request forever")
	}
}

func TestRetryableJudgesOurErrorsAndTheNetworks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a retryable status", &httpclient.StatusError{Code: 503, Retryable: true}, true},
		{"a status that is not", &httpclient.StatusError{Code: 401}, false},
		{"an unrepeatable request", httpclient.ErrUnrepeatable, false},
		{"a name that will not resolve", &net.DNSError{Err: "no such host"}, false},
		{"a resolver that timed out", &net.DNSError{IsTimeout: true}, true},
		{"a cancelled context", context.Canceled, false},
		{"nothing at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := httpclient.Retryable(tc.err); got != tc.want {
				t.Errorf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestRetryingStopsWhenTheCallerDoes: a wait that outlived the caller's cancellation would
// wake to make a request nobody is waiting for. The cancellation lands *during* the backoff,
// which is the case worth pinning — an already-cancelled context never reaches the server at
// all, so it would prove nothing about the retry loop.
func TestRetryingStopsWhenTheCallerDoes(t *testing.T) {
	t.Parallel()
	calls := answering(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(calls.serve))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sleep := func(ctx context.Context, _ time.Duration) error {
		cancel()         // the caller gives up while we are waiting to try again
		return ctx.Err() // which is what the wait now reports
	}

	client := httpclient.New(httpclient.Config{Retry: testPolicy(5), Sleep: sleep})
	err := doErr(t, client, ctx, mustRequest(t, http.MethodGet, server.URL))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Do returned %v, want the cancellation", err)
	}
	if got := calls.requests.Load(); got != 1 {
		t.Errorf("the server saw %d requests after cancellation, want the 1 that preceded it", got)
	}
}

func mustRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, target, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	return req
}

func mustRequestWithBody(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	return req
}
