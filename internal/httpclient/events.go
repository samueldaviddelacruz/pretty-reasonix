package httpclient

import (
	"bufio"
	"io"
	"strings"
)

const (
	// eventTypeMessage is the name the format gives an event that does not name itself.
	eventTypeMessage = "message"
	// maxEventLineBytes bounds one line of an event stream. The scanner's default is generous
	// for prose and small for an inline payload, and a silently truncated line would look like a
	// protocol error two layers up.
	maxEventLineBytes = 4 << 20
)

// Event is one server-sent event.
type Event struct {
	// Type is the event's name, from the event field; "message" when the stream names none,
	// which is what the format specifies.
	Type string
	// Data is the payload, with several data fields joined by newlines.
	Data string
	// ID is the id field, when the stream sent one.
	ID string
}

// eventScanner reads events from an event-stream body.
//
// Parsed by hand rather than with a decoder, because every interesting case is at the boundary:
// a comment line that must be ignored, a field with no colon, two data lines belonging to one
// event, and a stream that ends without the blank line that would have terminated the last one.
type eventScanner struct {
	lines *bufio.Scanner
}

func newEventScanner(body io.Reader) *eventScanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxEventLineBytes)
	return &eventScanner{lines: scanner}
}

// Next returns the next event, or io.EOF once the stream has ended.
func (s *eventScanner) Next() (Event, error) {
	event := Event{}
	var sawField, sawData bool

	for s.lines.Scan() {
		line := s.lines.Text()
		if line == "" {
			if !sawField {
				continue // the blank line that ends each event is not an event of its own
			}
			return withDefaultType(event), nil
		}
		if strings.HasPrefix(line, ":") {
			continue // a comment, which is how a server keeps a quiet stream alive
		}
		field, value, hasValue := strings.Cut(line, ":")
		if !hasValue {
			continue // a field with no colon carries no value
		}
		// One space after the colon is part of the format and is not data.
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			event.Type = value
			sawField = true
		case "data":
			if sawData {
				event.Data += "\n"
			}
			event.Data += value
			sawData, sawField = true, true
		case "id":
			event.ID = value
			sawField = true
		case "retry":
			// Deliberately unread: a reconnect delay is a decision for the caller, which knows
			// what the person waiting has already been shown.
		}
	}
	if err := s.lines.Err(); err != nil {
		return Event{}, err
	}
	if sawField {
		// The stream ended without its terminating blank line. Deliver what arrived rather than
		// losing it; the call after this one reports the end.
		return withDefaultType(event), nil
	}
	return Event{}, io.EOF
}

// withDefaultType applies the format's default to an event that never named its type.
func withDefaultType(event Event) Event {
	if event.Type == "" {
		event.Type = eventTypeMessage
	}
	return event
}
