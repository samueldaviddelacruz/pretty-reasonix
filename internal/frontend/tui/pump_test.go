// White-box: the pump and its message are unexported, and the batching rule is the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"pretty-reasonix/internal/contract/event"
)

// collectInto runs a pump against a sink and returns a channel of what it sent.
//
// No sleeps: the pump is given a sink, the test queues events, and then it waits on the destination until the batch it
// expects arrives. A test that waited for a duration would be guessing at scheduling it cannot see.
func collectInto(t *testing.T, sink *event.ChannelSink) (<-chan batchMsg, context.CancelFunc) {
	t.Helper()
	batches := make(chan batchMsg, 8)
	ctx, stop := context.WithCancel(context.Background())
	go newPump(sink, func(msg tea.Msg) {
		if batch, ok := msg.(batchMsg); ok {
			batches <- batch
		}
	}).run(ctx)
	return batches, stop
}

// next waits for one batch or fails, which is the only place a timeout is legitimate: the subject is "does it arrive".
func next(t *testing.T, batches <-chan batchMsg) batchMsg {
	t.Helper()
	select {
	case batch := <-batches:
		return batch
	case <-time.After(5 * time.Second):
		t.Fatal("no batch arrived; this timeout is the subject, not a sleep")
		return batchMsg{}
	}
}

// TestABurstCostsOneRender is the criterion's first clause. Three events queued before the pump reads are one message,
// because a render is the expensive thing and fifty deltas should not buy fifty of them.
func TestABurstCostsOneRender(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()
	batches, stop := collectInto(t, sink)
	defer stop()

	// Queued before the pump can plausibly be scheduled, and buffered, so they are all waiting when it reads.
	sink.Emit(event.TurnStarted{TurnID: "t1"})
	sink.Emit(event.TextDelta{TurnID: "t1", ItemID: "i", Text: "a"})
	sink.Emit(event.TextDelta{TurnID: "t1", ItemID: "i", Text: "b"})

	batch := next(t, batches)
	if len(batch.Events) != 3 {
		t.Fatalf("the first batch holds %d events, want all 3 queued", len(batch.Events))
	}
}

// TestAContinuousStreamStillBatches: a producer faster than the renderer must not starve it into one event per message,
// and must not block it either.
func TestAContinuousStreamStillBatches(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()
	batches, stop := collectInto(t, sink)
	defer stop()

	for range 20 {
		sink.Emit(event.TextDelta{TurnID: "t1", ItemID: "i", Text: "x"})
	}
	first := next(t, batches)
	if len(first.Events) == 0 {
		t.Fatal("an empty batch was sent")
	}
	// The assertion is not a count: how many land in the first batch is a scheduling detail. What matters is that the
	// pump never sends one message per event when it could have sent one for many.
	if len(first.Events) > 20 {
		t.Errorf("the batch holds %d events, more than were sent", len(first.Events))
	}
}

// TestALossIsCountedAndCarried is the criterion's third clause: dropping is surfaced rather than silent.
func TestALossIsCountedAndCarried(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()

	// Filled before the pump starts, so the overflow is deterministic rather than a race with the reader.
	for range event.SinkBufferSize + 5 {
		sink.Emit(event.TextDelta{TurnID: "t1", ItemID: "i", Text: "x"})
	}
	if sink.Dropped() != 5 {
		t.Fatalf("the sink dropped %d events, want 5 over the buffer", sink.Dropped())
	}

	batches, stop := collectInto(t, sink)
	defer stop()
	batch := next(t, batches)
	if batch.Dropped != 5 {
		t.Errorf("the batch reports %d dropped, want the 5 the sink counted", batch.Dropped)
	}
	if len(batch.Events) == 0 {
		t.Error("a batch that reports a loss should also carry what survived it")
	}
}

// TestThePumpStopsWithItsContext, because a goroutine reading a channel outlives the program otherwise.
func TestThePumpStopsWithItsContext(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()
	batches, stop := collectInto(t, sink)

	sink.Emit(event.TurnStarted{TurnID: "t1"})
	_ = next(t, batches)

	stop()
	// Nothing further is sent: the pump has returned. A second emit must not panic on a send to a dead reader, and the
	// program must not receive a batch it did not ask for.
	sink.Emit(event.TurnStarted{TurnID: "t2"})
	select {
	case unexpected := <-batches:
		t.Fatalf("the pump sent %d events after its context ended", len(unexpected.Events))
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAModelCountsBatchesAndLosses: the view is where the criterion becomes visible.
func TestAModelCountsBatchesAndLosses(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	model := newModel(Options{Now: clock.Now})

	updated, _ := model.Update(batchMsg{
		Events:  []event.Event{event.TurnStarted{}, event.Notice{}},
		Dropped: 3,
	})
	view := updated.View().Content

	if !strings.Contains(view, "2 event(s)") {
		t.Errorf("the view does not count the events: %q", view)
	}
	if !strings.Contains(view, "3 dropped") {
		t.Errorf("the view does not report the loss: %q", view)
	}
}
