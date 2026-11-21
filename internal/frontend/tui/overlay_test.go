// White-box: the stack is unexported state, and the ownership rule is the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"slices"
	"testing"
)

// modal is an overlay a test configures.
type modal struct {
	id       string
	keys     []string
	composer bool
}

func (m modal) ID() string { return m.id }

func (m modal) OwnsKeys(key string) bool { return slices.Contains(m.keys, key) }

func (m modal) ComposerVisible() bool { return m.composer }

// TestNoOverlayLeavesTheComposerVisible: the ordinary case, stated so the rule has a baseline to differ from.
func TestNoOverlayLeavesTheComposerVisible(t *testing.T) {
	t.Parallel()
	stack := &Stack{}

	if !stack.ComposerVisible() {
		t.Error("no overlay hid the composer")
	}
	if stack.Handles("enter") {
		t.Error("an empty stack claimed a key")
	}
	if got := stack.BottomHeight(3); got != 3 {
		t.Errorf("the bottom region is %d rows, want the composer's 3", got)
	}
}

// TestAModalWithoutTheComposerReservesNoRoom is the criterion's second clause: an inactive composer cannot render under a
// modal, because the layout is never told to leave room for it.
func TestAModalWithoutTheComposerReservesNoRoom(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{id: "decision", keys: []string{"y", "n"}, composer: false})

	if stack.ComposerVisible() {
		t.Error("a modal that owns the screen left the composer visible")
	}
	if got := stack.BottomHeight(3); got != 0 {
		t.Errorf("the bottom region reserves %d rows under a modal, want 0", got)
	}
}

// TestAModalMayKeepTheComposer, because a picker over a half-typed question should not throw the question away.
func TestAModalMayKeepTheComposer(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{id: "picker", keys: []string{"up"}, composer: true})

	if !stack.ComposerVisible() {
		t.Error("a modal that keeps the composer hid it")
	}
	if got := stack.BottomHeight(4); got != 4 {
		t.Errorf("the bottom region is %d rows, want the composer's 4", got)
	}
}

// TestOnlyTheTopOwnsAKey: a keystroke reaches the top overlay or nothing. It never reaches the overlay underneath, because
// a modal exists to stop what is below it from reacting.
func TestOnlyTheTopOwnsAKey(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{id: "below", keys: []string{"y"}, composer: true})
	stack.Push(modal{id: "above", keys: []string{"n"}, composer: true})

	if stack.Handles("y") {
		t.Error("a key reached the overlay below the top")
	}
	if !stack.Handles("n") {
		t.Error("the top overlay did not claim its own key")
	}
	// A key nobody claims falls through, which is what lets Escape reach a global handler.
	if stack.Handles("escape") {
		t.Error("an unclaimed key was claimed")
	}
}

// TestAPopRevealsWhatWasUnderneath, since a nested modal returning must restore the rules of the one below it.
func TestAPopRevealsWhatWasUnderneath(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{id: "decision", keys: []string{"y"}, composer: false})
	stack.Push(modal{id: "picker", keys: []string{"up"}, composer: true})

	if !stack.ComposerVisible() {
		t.Fatal("the picker declares the composer visible, so it should be; the setup is what makes the pop meaningful")
	}

	stack.Pop()
	if stack.Depth() != 1 {
		t.Fatalf("depth is %d after popping one", stack.Depth())
	}
	if stack.ComposerVisible() {
		t.Error("popping to the decision card left the composer visible")
	}
	if stack.Top().ID() != "decision" {
		t.Errorf("the top is %q", stack.Top().ID())
	}
}

// TestPoppingAnEmptyStackIsHarmless, so a handler does not have to check before closing something.
func TestPoppingAnEmptyStackIsHarmless(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Pop()
	stack.Pop()

	if stack.Depth() != 0 || stack.Top() != nil {
		t.Errorf("popping an empty stack left %d overlays", stack.Depth())
	}
}

// TestANegativeComposerHeightIsNotReserved: a caller with nothing to draw must not produce negative room for the
// transcript.
func TestANegativeComposerHeightIsNotReserved(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	if got := stack.BottomHeight(-2); got != 0 {
		t.Errorf("a negative composer height reserved %d rows", got)
	}
}
