package event

import (
	"strconv"
	"sync"
)

// SinkBufferSize is how many events a ChannelSink holds before it starts dropping.
//
// It is one of the two buffers sanctioned by GO-STANDARDS.md §7: the engine must never block on a slow
// frontend, so back-pressure in this direction is the wrong trade. 256 is well over one turn's worth
// of events and under a megabyte of memory, so a frontend that has genuinely stopped is detectable
// while a merely slow one is not punished.
const SinkBufferSize = 256

// Sink is a consumer of engine events.
//
// Emit must not block. A frontend that cannot keep up loses events rather than pushing back-pressure
// onto the model, because a stalled loop is worse than a missing row.
type Sink interface {
	Emit(Event)
}

// ChannelSink is a Sink over a buffered channel, for a frontend that drains on its own goroutine.
type ChannelSink struct {
	ch chan Event
	// mu guards dropped, and is held across the notice's own send so that two emitters cannot both
	// decide to report the same losses.
	mu      sync.Mutex
	dropped int64
	// total is never reset. A frontend that wants to say "N events were lost" needs a number it can trust; dropped is
	// the pending count, which the notice resets the moment it lands, so summing it counts the same loss twice or
	// misses it entirely depending on when the read lands. Both exist because they answer different questions.
	total int64
}

// NewChannelSink makes a sink whose buffer a frontend drains from its own goroutine.
func NewChannelSink() *ChannelSink {
	return &ChannelSink{ch: make(chan Event, SinkBufferSize)}
}

// Emit never blocks: a full buffer counts a loss, and the next event that lands carries a notice
// saying how many were lost in the meantime.
func (s *ChannelSink) Emit(e Event) {
	select {
	case s.ch <- e:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.dropped == 0 {
			return
		}
		notice := Notice{
			Level: NoticeWarn,
			Code:  "event.dropped",
			Text:  "Dropped " + strconv.FormatInt(s.dropped, 10) + " events",
		}
		select {
		case s.ch <- notice:
			// Zeroed only when the notice actually landed, so a funnel that is still full keeps the
			// count rather than losing the news along with the events.
			s.dropped = 0
		default:
		}
	default:
		s.mu.Lock()
		s.dropped++
		s.total++
		s.mu.Unlock()
	}
}

// Events is the channel a frontend drains. It is the same channel on every call.
func (s *ChannelSink) Events() <-chan Event { return s.ch }

// TotalDropped is every event this sink has ever lost, which is the only number a frontend may accumulate.
func (s *ChannelSink) TotalDropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// Dropped is how many events are lost and not yet reported.
func (s *ChannelSink) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}
