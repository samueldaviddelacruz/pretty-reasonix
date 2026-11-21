package session

import "pretty-reasonix/internal/contract/provider"

// Message is one entry in the transcript, which is deliberately wider than
// provider.Message: the record keeps what the wire has no field for (an id, the
// vendor's unedited payload, whether the entry is only kept) so that the request
// shape never has to grow a field just to remember something.
type Message struct {
	ID   string
	Role provider.Role
	// Content is the text as the model and the person should read it.
	Content string
	// RawContent is the vendor's payload before it was turned into Content, kept
	// so a redisplay or a replay after a format change is still possible. It never
	// reaches a request.
	RawContent string
	// Hidden marks an entry that stays in the record but not in the request: the
	// ugliness of a compaction or an injected note is a display problem, not a
	// reason to lose the history that produced it.
	Hidden bool
	// HostAuthored marks an entry the tool wrote rather than the person or the model — an execution
	// policy block, an environment summary. It is kept so a reader can tell the program's words from
	// the participants', and so the UI can style them differently.
	HostAuthored bool
	ToolCallID   string
	ToolCalls    []provider.ToolCall
}

// Transcript is the append-only record of a conversation, in the order the turns
// happened. Appending is the only way in, so a caller that has only a *Transcript
// cannot rewrite what was already said.
type Transcript struct {
	messages []Message
	// log is optional: a transcript built without one is the in-memory record the tests and the
	// first draft of a turn use, and a transcript built with one is the same record plus a copy on
	// disk. Nothing else in this type changes, which is why persistence arrives without a second
	// code path through the controller.
	log *Log
	// writeErr is the first failure to persist, kept rather than returned.
	//
	// Append takes no error because the turn has already produced an answer worth showing: a session
	// that cannot be written is a loss of history, and failing the turn over it would throw away the
	// answer too. The caller reports this once, at the end, rather than the loop deciding per message.
	writeErr error
}

// Append records m at the end of the transcript. It does nothing else: every
// other decision (whether the entry is hidden, what it looks like on the wire) is
// made by the caller that knows the reason.
func (t *Transcript) Append(m Message) {
	t.messages = append(t.messages, m)
	if t.log != nil && t.writeErr == nil {
		t.writeErr = t.log.Append(m)
	}
}

// WriteError reports the first failure to persist, or nil when every message reached the log.
//
// It exists so that losing history is *reported* rather than silent: a transcript that quietly stopped writing would
// look exactly like a transcript that worked, which is the failure mode this whole store exists to prevent.
func (t *Transcript) WriteError() error { return t.writeErr }

// Close finishes the session's log. It is safe on a transcript that has none.
func (t *Transcript) Close() error {
	if t.log == nil {
		return nil
	}
	// A write that already failed is the more useful of the two errors: it names what went wrong with the conversation.
	if t.writeErr != nil {
		_ = t.log.Close()
		return t.writeErr
	}
	return t.log.Close()
}

// All returns a copy of the record, so a caller can read the whole history
// without being able to append to or overwrite it. The copy is shallow: a
// ToolCalls slice is shared with the record.
func (t *Transcript) All() []Message {
	out := make([]Message, len(t.messages))
	copy(out, t.messages)
	return out
}

// Fold projects the record onto the messages a provider may receive: hidden
// entries are left behind and the bookkeeping the wire has no field for is
// dropped. It reads nothing but the receiver, so the same transcript always folds
// to the same request.
func (t *Transcript) Fold() []provider.Message {
	out := make([]provider.Message, 0, len(t.messages))
	for _, m := range t.messages {
		if m.Hidden {
			continue
		}
		out = append(out, provider.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		})
	}
	return out
}
