package session

import "fmt"

// NewPersistent makes a transcript that writes every message to a session's log as well as keeping it.
//
// This is the whole of persistence: the controller already appends to a transcript, so a transcript that also writes
// means the turn loop needs no knowledge that disks exist. The same shape as the sink boundary — one port, two
// implementations — applied to storage.
//
// It refuses an id already in use rather than appending into another session's log: a collision in a name is a bug at
// the call site, and the alternative is silently mixing two conversations together. Reading an existing session is
// LoadPersistent's job, and the two are separate so the mistake cannot be made by choosing the wrong function.
func NewPersistent(layout Layout) (*Transcript, error) {
	if fileExists(layout.Events) {
		return nil, fmt.Errorf("%w: %s", ErrSessionExists, layout.Events)
	}
	if err := Create(layout); err != nil {
		return nil, err
	}
	log, err := CreateLog(layout.Events, layout.Canonical)
	if err != nil {
		return nil, fmt.Errorf("opening the session: %w", err)
	}
	return &Transcript{log: log}, nil
}

// LoadPersistent opens an existing session and fills the transcript from its log, salvaging a torn tail.
//
// It is separate from NewPersistent because the two differ in one decision that matters: a new session must not read a
// damaged file, and a resumed one must not overwrite one. Recovering here rather than in the caller means the notice
// about what was set aside travels with the transcript that lost it.
func LoadPersistent(layout Layout) (*Transcript, Recovered, error) {
	recovered, err := Recover(layout)
	if err != nil {
		return nil, Recovered{}, err
	}
	log, err := CreateLog(layout.Events, layout.Canonical)
	if err != nil {
		return nil, Recovered{}, fmt.Errorf("opening the session: %w", err)
	}
	return &Transcript{messages: recovered.Messages, log: log}, recovered, nil
}
