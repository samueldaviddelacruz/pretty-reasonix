package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"pretty-reasonix/internal/contract/event"
)

// batchMsg is every event the engine produced since the last update, delivered as one message.
//
// One message rather than one per event, because a render is the most expensive thing a terminal frontend does: a
// burst of fifty deltas would otherwise cost fifty renders, and each would be thrown away by the next. The pump does
// the same work either way; only the redraws collapse.
type batchMsg struct {
	Events []event.Event
	// Dropped is how many events the sink lost and has not yet reported. It travels with the batch rather than on its
	// own, because a loss is only meaningful next to what survived it.
	Dropped int64
}

// pump drains a sink and hands the program one message per burst.
//
// It runs on its own goroutine, which is what keeps the engine from ever waiting on a renderer: the sink drops rather
// than blocks, and this side catches up as fast as the program will take messages.
type pump struct {
	sink *event.ChannelSink
	// send is the way in. A function rather than a *tea.Program so a test can collect what would have been sent
	// without running a terminal program to do it.
	send func(tea.Msg)
}

// newPump wires a pump to a sink and a destination.
func newPump(sink *event.ChannelSink, send func(tea.Msg)) *pump {
	return &pump{sink: sink, send: send}
}

// run forwards events until the context ends. It never returns an error: a frontend that cannot be told what happened
// has nothing to do with the failure, and the caller's cancellation is the only way out.
func (p *pump) run(ctx context.Context) {
	events := p.sink.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case first, ok := <-events:
			if !ok {
				return
			}
			// Everything already queued goes in the same message. The select's default is what makes this a drain
			// rather than a wait: if the channel is empty the batch ships now, and the next burst starts a new one.
			batch := collect(first, events)
			p.send(batchMsg{Events: batch, Dropped: p.sink.Dropped()})
		}
	}
}

// collect takes the event it was given and everything else already waiting, for as long as nothing blocks.
//
// A closed channel ends the batch rather than producing a zero event: the pump's own exit follows on the next
// iteration, and inventing an event here would put a message in front of the model that the engine never sent.
func collect(first event.Event, events <-chan event.Event) []event.Event {
	batch := []event.Event{first}
	for {
		select {
		case next, ok := <-events:
			if !ok {
				return batch
			}
			batch = append(batch, next)
		default:
			return batch
		}
	}
}
