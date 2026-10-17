package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pretty-reasonix/internal/httpclient"
)

// idleFor is short enough that a watchdog test costs milliseconds and long enough that the
// scheduler cannot fire it before the bytes are read.
const idleFor = 80 * time.Millisecond

// streaming serves a canned body as an event stream, which is how the parser is tested: the
// reader is what these cases are about, and a scanner unit test on an unexported function would
// prove less than one that goes through a response.
func streaming(t *testing.T, body string) *httpclient.Events {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: idleFor}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	t.Cleanup(func() { _ = events.Close() })
	return events
}

// collect reads events until the stream ends and returns them.
func collect(t *testing.T, events *httpclient.Events) []httpclient.Event {
	t.Helper()
	var read []httpclient.Event
	for {
		event, err := events.Next()
		if errors.Is(err, io.EOF) {
			return read
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		read = append(read, event)
	}
}

func TestTheEventFormatIsReadAsSpecified(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want []httpclient.Event
	}{
		"a bare data line takes the default type": {
			body: "data: hello\n\n",
			want: []httpclient.Event{{Type: "message", Data: "hello"}},
		},
		"an event names its own type": {
			body: "event: delta\ndata: hi\n\n",
			want: []httpclient.Event{{Type: "delta", Data: "hi"}},
		},
		"two data lines are one event": {
			body: "data: one\ndata: two\n\n",
			want: []httpclient.Event{{Type: "message", Data: "one\ntwo"}},
		},
		"the id field is kept, because a resume needs it": {
			body: "id: 42\ndata: hi\n\n",
			want: []httpclient.Event{{Type: "message", Data: "hi", ID: "42"}},
		},
		"a comment keeps a quiet stream alive without being data": {
			body: ": keep-alive\ndata: real\n\n",
			want: []httpclient.Event{{Type: "message", Data: "real"}},
		},
		"a field with no colon carries no value": {
			body: "junk\ndata: real\n\n",
			want: []httpclient.Event{{Type: "message", Data: "real"}},
		},
		"exactly one space after the colon belongs to the format": {
			body: "data:  two spaces\n\n",
			want: []httpclient.Event{{Type: "message", Data: " two spaces"}},
		},
		"only one space is stripped": {
			body: "data:no space\n\n",
			want: []httpclient.Event{{Type: "message", Data: "no space"}},
		},
		"several events arrive in order": {
			body: "data: first\n\ndata: second\n\n",
			want: []httpclient.Event{{Type: "message", Data: "first"}, {Type: "message", Data: "second"}},
		},
		"an empty data field is a value, not an absence": {
			body: "data:\n\n",
			want: []httpclient.Event{{Type: "message", Data: ""}},
		},
		"a stream that ends without its blank line still delivers": {
			body: "data: truncated",
			want: []httpclient.Event{{Type: "message", Data: "truncated"}},
		},
		"a retry field is ignored rather than misread as data": {
			body: "retry: 5000\ndata: real\n\n",
			want: []httpclient.Event{{Type: "message", Data: "real"}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := collect(t, streaming(t, tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("read %d events, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("event %d is %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

// silentUntilCancelled writes the headers, flushes, and then says nothing until the client gives
// up — which is exactly how a stalled vendor looks.
func silentUntilCancelled(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if err := http.NewResponseController(w).Flush(); err != nil {
		return
	}
	<-r.Context().Done()
}

// TestASilentStreamIsReadAsDropped is the watchdog half of the criterion. The event that arrived
// is delivered, and the silence after it surfaces typed rather than as a hang or a bare error.
func TestASilentStreamIsReadAsDropped(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		_, _ = io.WriteString(w, "data: only\n\n")
		_ = controller.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: idleFor}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = events.Close() }()

	event, err := events.Next()
	if err != nil {
		t.Fatalf("the first event: %v", err)
	}
	if event.Data != "only" {
		t.Errorf("the first event is %+v, want the one that arrived", event)
	}

	_, err = events.Next()
	interrupted, found := errors.AsType[*httpclient.StreamInterruptedError](err)
	if !found {
		t.Fatalf("Next returned %v, want a StreamInterruptedError", err)
	}
	if !errors.Is(err, httpclient.ErrStreamIdle) {
		t.Errorf("the interruption does not say it was silence: %v", err)
	}
	if interrupted.Delivered != 1 {
		t.Errorf("Delivered is %d, want the one event that arrived", interrupted.Delivered)
	}
	if interrupted.Retryable {
		t.Error("the interruption is marked retryable after delivering an event, so a replay would show it twice")
	}
}

// TestASilentStreamIsReopenedOnce is the other half: silence *before* anything was delivered is
// the one case where a reconnect is both safe and the fix, so it happens once and invisibly.
func TestASilentStreamIsReopenedOnce(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			silentUntilCancelled(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		_, _ = io.WriteString(w, "data: after the reopen\n\n")
		_ = controller.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: idleFor}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = events.Close() }()

	event, err := events.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.Data != "after the reopen" {
		t.Errorf("the event is %+v, want the one from the reopened stream", event)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("the server saw %d requests, want the reopen to have made exactly 2", got)
	}
}

// TestASecondSilenceIsSurfacedRatherThanRetried bounds the reopen: "at most once" means a
// stream that stalls again is reported, not retried for ever.
func TestASecondSilenceIsSurfacedRatherThanRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		silentUntilCancelled(w, r)
	}))
	defer server.Close()

	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: idleFor}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = events.Close() }()

	_, err = events.Next()
	if _, found := errors.AsType[*httpclient.StreamInterruptedError](err); !found {
		t.Fatalf("Next returned %v, want a StreamInterruptedError", err)
	}
	if !errors.Is(err, httpclient.ErrStreamIdle) {
		t.Errorf("the interruption does not say it was silence: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("the server saw %d requests; the reopen happens once and no more", got)
	}
}

// TestAMidBodyCutIsTyped is the criterion's second half, and the case that must NOT be retried:
// the connection died rather than stalled, and what the server had already sent is all there is.
func TestAMidBodyCutIsTyped(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		_, _ = io.WriteString(w, "data: first\n\n")
		_ = controller.Flush()
		//nolint:forbidigo // the documented way to abort a response mid-body; this test exists to
		// prove that a cut surfaces typed rather than being mistaken for the server finishing.
		panic(http.ErrAbortHandler)
	}))
	defer server.Close()

	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: time.Second}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = events.Close() }()

	first, firstErr := events.Next()
	if firstErr != nil || first.Data != "first" {
		t.Fatalf("the event before the cut: %+v, %v", first, firstErr)
	}

	_, err = events.Next()
	interrupted, found := errors.AsType[*httpclient.StreamInterruptedError](err)
	if !found {
		t.Fatalf("Next returned %v, want a StreamInterruptedError", err)
	}
	if errors.Is(err, httpclient.ErrStreamIdle) {
		t.Error("a cut was reported as silence, so a caller cannot tell a stall from a death")
	}
	if interrupted.Delivered != 1 {
		t.Errorf("Delivered is %d, want 1", interrupted.Delivered)
	}
	if interrupted.Retryable {
		t.Error("a cut after delivering an event is marked retryable")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("the server saw %d requests; a cut is surfaced, not replayed", got)
	}
}

func TestAResponseThatIsNotAnEventStreamIsRefused(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"not a stream"}`)
	}))
	defer server.Close()

	_, err := httpclient.New(httpclient.Config{}).Stream(
		t.Context(), mustRequest(t, http.MethodGet, server.URL))
	if err == nil {
		t.Fatal("a JSON response was accepted as an event stream")
	}
	if !strings.Contains(err.Error(), "application/json") {
		t.Errorf("the error does not say what arrived instead: %v", err)
	}
}

// TestAClosedStreamReadsAsEnded: Next after Close must answer rather than dereference nothing.
func TestAClosedStreamReadsAsEnded(t *testing.T) {
	t.Parallel()
	events := streaming(t, "data: one\n\n")
	if err := events.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := events.Close(); err != nil {
		t.Errorf("Close is not idempotent: %v", err)
	}
	if _, err := events.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next after Close returned %v, want EOF", err)
	}
}

func TestStreamStopsWhenTheCallerDoes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(silentUntilCancelled))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	events, err := httpclient.New(httpclient.Config{StreamIdleTimeout: time.Minute}).Stream(
		ctx, mustRequest(t, http.MethodGet, server.URL))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = events.Close() }()

	cancel()
	if _, err := events.Next(); err == nil {
		t.Fatal("a cancelled stream produced an event and no error")
	}
}
