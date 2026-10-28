// Package agent holds the engine: the controller a frontend drives, the turn loop it runs, and the
// context maintenance between rounds of it.
//
// The controller is the only thing a frontend calls. Everything a frontend needs to reach the engine
// is a port declared here, so the terminal UI and the headless runner hold the same object and can
// only do what these interfaces allow (docs/TECHNICAL_DESIGN.md §6.2).
package agent

import (
	"errors"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
)

// ErrNoTurn reports a decision delivered for a turn that is not waiting for one.
var ErrNoTurn = errors.New("no turn is waiting for a decision")

// TurnInput is what a frontend asks for when it starts a turn.
type TurnInput struct {
	// Text is what the person typed.
	Text string
	// Model is the resolved model to run against, so the controller never resolves a reference
	// itself: choosing is the assembly's job, and the turn only carries the answer.
	Model model.Model
	// Effort is the resolved reasoning effort, already checked against what the model offers.
	Effort provider.Effort
}

// TurnControl is a frontend's handle on the turn it started.
type TurnControl interface {
	// Done is closed when the turn ends, whatever ended it. A frontend ranges over events to render
	// the turn; this is how it learns there are no more.
	Done() <-chan struct{}
	// Cancel asks the turn to stop. More than one call is allowed and later ones do nothing.
	Cancel()
}

// Approvals is how a frontend answers a decision the engine cannot make alone.
type Approvals interface {
	// Answer delivers the decision for the call an ApprovalRequested event named. It reports
	// ErrNoTurn when nothing is waiting, so a stale card cannot decide a later turn.
	Answer(callID string, decision event.Decision) error
}

// History is the conversation, for a frontend to render and a person to resume.
type History interface {
	// Messages is the transcript in order, including entries a request never sees.
	Messages() []session.Message
}

// Capabilities is what this session can do, for a frontend to show rather than guess.
type Capabilities interface {
	// Models are the ones this session may use.
	Models() []model.Model
	// Tools are the tool names available to the model.
	Tools() []string
}
