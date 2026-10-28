package event_test

import (
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/event"
)

func TestEveryEventImplementsTheInterface(t *testing.T) {
	t.Parallel()
	// The field is `any`, not `event.Event`, on purpose: a slice of Event would already satisfy the
	// assertion below at compile time and prove nothing. This is the one thing the generated version
	// got wrong about its own test.
	events := []struct {
		name string
		e    any
	}{
		{"TurnStarted", event.TurnStarted{}},
		{"TextDelta", event.TextDelta{}},
		{"ReasoningDelta", event.ReasoningDelta{}},
		{"MessageCommitted", event.MessageCommitted{}},
		{"ToolDispatched", event.ToolDispatched{}},
		{"ToolProgress", event.ToolProgress{}},
		{"ToolResult", event.ToolResult{}},
		{"ApprovalRequested", event.ApprovalRequested{}},
		{"ApprovalDecided", event.ApprovalDecided{}},
		{"AskRequested", event.AskRequested{}},
		{"Notice", event.Notice{}},
		{"TodoStateChanged", event.TodoStateChanged{}},
		{"CompactionStarted", event.CompactionStarted{}},
		{"CompactionCommitted", event.CompactionCommitted{}},
		{"UsageReported", event.UsageReported{}},
		{"TurnDone", event.TurnDone{}},
		{"ToolCallStarted", event.ToolCallStarted{}},
	}

	for _, tc := range events {
		if _, ok := tc.e.(event.Event); !ok {
			t.Errorf("event %s does not implement Event interface", tc.name)
		}
	}
}

func TestEmitNeverBlocks(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()
	for range event.SinkBufferSize {
		sink.Emit(event.TurnStarted{})
	}

	done := make(chan struct{})
	go func() {
		sink.Emit(event.TurnStarted{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Emit blocked")
	}
}

// TestADroppedEventIsReportedWithItsCount is the claim worth asserting: one notice, naming how many
// events were lost. The generated version found the first notice and stopped, so duplicates passed a
// test named "Once".
func TestADroppedEventIsReportedWithItsCount(t *testing.T) {
	t.Parallel()
	sink := event.NewChannelSink()
	for range event.SinkBufferSize {
		sink.Emit(event.TurnStarted{})
	}
	for range 3 {
		sink.Emit(event.TurnStarted{})
	}
	if got := sink.Dropped(); got != 3 {
		t.Fatalf("Dropped() is %d while the buffer is full, want 3 counted and unreported", got)
	}

	events := drain(sink)
	// One event lands, and the notice rides with it.
	sink.Emit(event.TurnStarted{})
	events = append(events, drain(sink)...)

	var notices []event.Notice
	for _, e := range events {
		if notice, ok := e.(event.Notice); ok {
			notices = append(notices, notice)
		}
	}
	if len(notices) != 1 {
		t.Fatalf("found %d notices, want exactly one", len(notices))
	}
	if notices[0].Code != "event.dropped" || notices[0].Level != event.NoticeWarn {
		t.Errorf("notice is %+v, want a warning coded event.dropped", notices[0])
	}
	if !strings.Contains(notices[0].Text, "3") {
		t.Errorf("the notice text %q does not say how many events were lost", notices[0].Text)
	}
	if got := sink.Dropped(); got != 0 {
		t.Errorf("Dropped() is %d after the notice was delivered, want 0", got)
	}
}

// drain empties what is buffered without a sleep, so the test does not depend on how fast the machine
// is. The generated version used a 10ms timeout, which is the shape that turns flaky under load.
func drain(s *event.ChannelSink) []event.Event {
	var events []event.Event
	for {
		select {
		case e := <-s.Events():
			events = append(events, e)
		default:
			return events
		}
	}
}
