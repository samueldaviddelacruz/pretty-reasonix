package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// ErrStreamIdle reports a stream that went silent for longer than the idle timeout.
var ErrStreamIdle = errors.New("the stream went silent")

// StreamInterruptedError is an event stream that ended before the server said it was done.
//
// It carries what a caller decides with: what went wrong, how much had already been delivered,
// and whether replaying is safe. A stream that delivered nothing can be replayed; one that
// delivered half an answer cannot, because the replay would show the same tokens twice.
type StreamInterruptedError struct {
	// Cause is what ended the stream.
	Cause error
	// Delivered is how many events arrived before the interruption.
	Delivered int
	// Retryable reports whether replaying the request is safe, which is true only when nothing
	// was delivered.
	Retryable bool
}

func (e *StreamInterruptedError) Error() string {
	return fmt.Sprintf("the event stream was interrupted after %d events: %v", e.Delivered, e.Cause)
}

// Unwrap keeps the cause matchable, so a caller can ask whether it was the watchdog.
func (e *StreamInterruptedError) Unwrap() error { return e.Cause }

// Stream opens an event stream.
//
// The connection and the headers are already retried by Do. What this adds is the body phase,
// which that policy cannot cover: once events have been handed to a caller, replaying the
// request would deliver them twice. So the stream is reopened at most once, and only after an
// idle drop that delivered nothing — the one interruption where a reconnect is both safe and
// exactly the fix.
func (c *Client) Stream(ctx context.Context, req *http.Request) (*Events, error) {
	stream := &Events{
		client:  c,
		request: req,
		ctx:     ctx,
		idleFor: c.streamIdle,
	}
	if err := stream.open(ctx); err != nil {
		return nil, err
	}
	return stream, nil
}

// Events is an open event stream. Next reads one event; Close releases it.
type Events struct {
	client  *Client
	request *http.Request
	//nolint:containedctx // this struct is a handle for one operation, not a long-lived object: the
	// context is the stream's lifetime, and threading it through every Next would let a caller
	// pass a different one mid-stream, which is the failure the rule exists to prevent.
	ctx     context.Context
	idleFor time.Duration

	body    io.ReadCloser
	scanner *eventScanner
	cancel  context.CancelFunc
	idle    *time.Timer
	// silent records that the watchdog fired, so a read failure can be attributed to silence
	// rather than reported as a broken connection. The watchdog writes it from its own
	// goroutine, so it is atomic: a plain bool here is a data race with the read below.
	silent    atomic.Bool
	delivered int
	reopened  bool
}

// open starts one attempt at the stream.
//
// Each attempt gets its own cancellable context so the watchdog can end it without ending the
// caller's: a reopen has to outlive the attempt that failed.
func (e *Events) open(ctx context.Context) error {
	attemptCtx, cancel := context.WithCancel(ctx)

	response, err := e.client.Do(attemptCtx, e.request)
	if err != nil {
		cancel()
		return err
	}
	if contentType := response.Header.Get("Content-Type"); !isEventStream(contentType) {
		_ = response.Body.Close()
		cancel()
		return fmt.Errorf("%s %s answered %q, which is not an event stream",
			e.request.Method, e.request.URL, contentType)
	}

	e.cancel = cancel
	e.silent.Store(false)
	e.body = &watchedBody{body: response.Body, touch: e.touch}
	e.scanner = newEventScanner(e.body)
	e.idle = time.AfterFunc(e.idleFor, func() {
		e.silent.Store(true)
		cancel()
	})
	return nil
}

// touch restarts the watchdog. It is called from the same goroutine that reads, so the timer
// needs no lock of its own.
func (e *Events) touch() {
	if e.idle != nil {
		e.idle.Reset(e.idleFor)
	}
}

// Next returns the next event, blocking until one arrives. It returns io.EOF when the server
// ended the stream itself, and a *StreamInterruptedError when the stream ended early.
func (e *Events) Next() (Event, error) {
	if e.scanner == nil {
		return Event{}, io.EOF // closed by the caller, so there is nothing left to read
	}
	for {
		event, err := e.scanner.Next()
		if err == nil {
			e.touch()
			e.delivered++
			return event, nil
		}
		if errors.Is(err, io.EOF) {
			return Event{}, io.EOF
		}

		interrupted := &StreamInterruptedError{
			Cause:     err,
			Delivered: e.delivered,
			Retryable: e.delivered == 0,
		}
		if e.silent.Load() {
			interrupted.Cause = fmt.Errorf("%w for %v: %w", ErrStreamIdle, e.idleFor, err)
		}
		// One reopen, only for silence before anything was delivered, and only while the caller
		// is still waiting.
		if e.silent.Load() && e.delivered == 0 && !e.reopened && e.ctx.Err() == nil {
			e.reopened = true
			_ = e.endAttempt()
			if reopenErr := e.open(e.ctx); reopenErr == nil {
				continue
			}
		}
		_ = e.endAttempt()
		return Event{}, interrupted
	}
}

// Close releases the stream. It is safe to call more than once.
func (e *Events) Close() error { return e.endAttempt() }

// endAttempt stops the watchdog, closes the body and cancels the context that governs it.
func (e *Events) endAttempt() error {
	var err error
	if e.idle != nil {
		e.idle.Stop()
	}
	if e.body != nil {
		err = e.body.Close()
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.idle, e.body, e.scanner, e.cancel = nil, nil, nil, nil
	return err
}

// watchedBody reports each successful read to the watchdog, which is what makes the timeout a
// measure of silence rather than of time: a stream delivering one byte per second is alive.
type watchedBody struct {
	body  io.ReadCloser
	touch func()
}

func (w *watchedBody) Read(buffer []byte) (int, error) {
	read, err := w.body.Read(buffer)
	if read > 0 {
		w.touch()
	}
	return read, err
}

func (w *watchedBody) Close() error { return w.body.Close() }

// isEventStream reports whether a Content-Type is the one this reader understands.
func isEventStream(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream")
}
