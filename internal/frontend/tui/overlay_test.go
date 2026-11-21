// White-box: the stack is unexported state, and the ownership rule is the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// modal is an overlay a test configures.
type modal struct {
	name      string
	ownership KeyOwnership
	composer  bool
}

func (m modal) Title() string { return m.name }

func (m modal) OwnsKeys() KeyOwnership          { return m.ownership }
func (m modal) ComposerVisible() bool           { return m.composer }
func (modal) Update(tea.Msg) (Overlay, tea.Cmd) { return nil, nil }
func (modal) View(int) string                   { return "" }

// TestNoOverlayLeavesTheComposerVisible is the baseline the rest differ from.
func TestNoOverlayLeavesTheComposerVisible(t *testing.T) {
	t.Parallel()
	stack := &Stack{}

	if !stack.ComposerVisible() {
		t.Error("no overlay hid the composer")
	}
	if stack.Swallows() {
		t.Error("an empty stack claimed the keyboard")
	}
	// The status line is counted even with nothing open: it is always drawn.
	if got := stack.BottomHeight(3, 1); got != 4 {
		t.Errorf("the bottom region is %d rows, want composer 3 + status 1", got)
	}
}

// TestAModalHidesTheComposerAndKeepsTheStatus is the criterion's second clause, and the status line is the half the first
// version got wrong: it returned the composer alone while the status was still drawn, so a layout reserving that height
// overdrew the transcript.
func TestAModalHidesTheComposerAndKeepsTheStatus(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "decision", ownership: Modal, composer: false})

	if stack.ComposerVisible() {
		t.Error("a modal left the composer visible")
	}
	if !stack.Swallows() {
		t.Error("a modal did not claim the keyboard")
	}
	if got := stack.BottomHeight(3, 1); got != 1 {
		t.Errorf("the bottom region is %d rows under a modal, want the status line's 1", got)
	}
}

// TestAnInputOwnedOverlayKeepsTheComposer: a picker over a half-typed question must not throw the question away.
func TestAnInputOwnedOverlayKeepsTheComposer(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "picker", ownership: InputOwned, composer: true})

	if !stack.ComposerVisible() {
		t.Error("a picker hid the composer")
	}
	if stack.Swallows() {
		t.Error("a picker claimed the whole keyboard")
	}
	if got := stack.BottomHeight(4, 1); got != 5 {
		t.Errorf("the bottom region is %d rows, want 5", got)
	}
}

// TestANestedOverlayCannotResurrectTheComposer is the review's fourth finding: top-wins visibility let a picker that keeps
// the composer bring back an input box the decision card below had deliberately removed.
func TestANestedOverlayCannotResurrectTheComposer(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "decision", ownership: Modal, composer: false})
	stack.Push(modal{name: "picker", ownership: InputOwned, composer: true})

	if stack.ComposerVisible() {
		t.Error("a picker resurrected the composer a modal had hidden")
	}
	if got := stack.BottomHeight(3, 1); got != 1 {
		t.Errorf("the bottom region is %d rows, want the status line only", got)
	}
}

// TestTheTopIsWhatAKeyReaches, and the keyboard still belongs to an overlay when a hidden composer leaves nowhere else for
// it to go.
//
// This test used to assert the opposite — that a non-modal top meant "not swallowed" — and that assertion was the bug: the
// modal underneath had already hidden the composer, so keys routed "to the composer" were delivered to an input box nobody
// could see. The top is still what a keystroke reaches; it is no longer what decides whether the composer gets it.
func TestTheTopIsWhatAKeyReaches(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "below", ownership: Modal, composer: false})
	stack.Push(modal{name: "above", ownership: InputOwned, composer: true})

	if stack.Top().Title() != "above" {
		t.Errorf("the top is %q", stack.Top().Title())
	}
	if !stack.Swallows() {
		t.Error("keys would be routed to the composer the modal below has hidden")
	}
}

// TestAPopRevealsTheRulesBelow, since closing a picker must restore the card's ownership.
func TestAPopRevealsTheRulesBelow(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "decision", ownership: Modal, composer: false})
	stack.Push(modal{name: "picker", ownership: InputOwned, composer: true})

	// Before the pop the picker is on top but the card has hidden the composer, so keys are already swallowed; what the
	// pop changes is which overlay's declaration governs next.
	stack.Pop()
	if stack.Depth() != 1 || stack.Top().Title() != "decision" {
		t.Fatalf("after popping, %d overlays with %q on top", stack.Depth(), stack.Top().Title())
	}
	if !stack.Swallows() {
		t.Error("the decision card did not resume owning the keyboard")
	}
	if stack.ComposerVisible() {
		t.Error("the composer came back with the decision card in front")
	}
}

// TestPoppingAnEmptyStackIsHarmless, so a handler need not check before closing something.
func TestPoppingAnEmptyStackIsHarmless(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Pop()
	stack.Pop()

	if stack.Depth() != 0 || stack.Top() != nil {
		t.Errorf("popping an empty stack left %d overlays", stack.Depth())
	}
}

// TestANilOverlayIsRefused: a stored nil would give Top a non-nil interface holding nothing, and the first method call
// on it would panic.
func TestANilOverlayIsRefused(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(nil)

	if stack.Depth() != 0 || stack.Top() != nil {
		t.Error("a nil overlay was stored")
	}
	if stack.Swallows() {
		t.Error("a nil overlay claimed the keyboard")
	}
}

// TestTheBaseDefaultsAreTheHarmlessOnes, which is what lets a toast embed it and answer nothing.
func TestTheBaseDefaultsAreTheHarmlessOnes(t *testing.T) {
	t.Parallel()
	base := Base{Name: "toast"}

	if base.Title() != "toast" {
		t.Errorf("the title is %q", base.Title())
	}
	if !base.ComposerVisible() {
		t.Error("a bare overlay hid the composer")
	}
	if base.OwnsKeys() != InputOwned {
		t.Error("a bare overlay claimed the keyboard")
	}
}

// TestNegativeHeightsAreNotReserved: a caller with nothing to draw must not produce negative room for the transcript.
func TestNegativeHeightsAreNotReserved(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	if got := stack.BottomHeight(-2, 1); got != 1 {
		t.Errorf("a negative composer height reserved %d rows", got)
	}
	if got := stack.BottomHeight(2, -1); got != 2 {
		t.Errorf("a negative status height reserved %d rows", got)
	}
}

// keyboardOnly is a modal that declares nothing but its ownership, which is the shape a real card will have.
type keyboardOnly struct{ Base }

func (keyboardOnly) OwnsKeys() KeyOwnership            { return Modal }
func (keyboardOnly) Update(tea.Msg) (Overlay, tea.Cmd) { return nil, nil }
func (keyboardOnly) View(int) string                   { return "" }

// TestAModalEmbeddingBaseStillHidesTheComposer is Gemini's first finding, and it is the U2 bug itself: the defaults say the
// composer is visible, so a modal that overrode only OwnsKeys claimed the keyboard and left an input box on screen.
func TestAModalEmbeddingBaseStillHidesTheComposer(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(keyboardOnly{Base{Name: "card"}})

	if stack.ComposerVisible() {
		t.Fatal("a modal embedding the defaults left the composer visible")
	}
	if !stack.Swallows() {
		t.Fatal("the modal did not claim the keyboard")
	}
	if got := stack.BottomHeight(3, 1); got != 1 {
		t.Errorf("the bottom region is %d rows, want the status line only", got)
	}
}

// TestAHiddenComposerSwallowsKeys is Gemini's second finding: a picker over a decision card is not itself modal, so the
// stack reported "not swallowed" while the composer was hidden, and every keystroke went to an input box nobody could see.
func TestAHiddenComposerSwallowsKeys(t *testing.T) {
	t.Parallel()
	stack := &Stack{}
	stack.Push(modal{name: "decision", ownership: Modal, composer: false})
	stack.Push(modal{name: "picker", ownership: InputOwned, composer: true})

	if stack.Top().OwnsKeys() == Modal {
		t.Fatal("the picker is meant to be input-owned here; the point is that the stack still swallows")
	}
	if !stack.Swallows() {
		t.Error("keys would be routed to a hidden composer")
	}
}

// TestAnInvalidOwnershipDoesNotPanicWhileReporting, which is Gemini's third finding: a panic inside String would fire while
// something was already reporting a failure.
func TestAnInvalidOwnershipDoesNotPanicWhileReporting(t *testing.T) {
	t.Parallel()
	if got := KeyOwnership(-7).String(); got != "keyownership(-7)" {
		t.Errorf("an invalid ownership formats as %q", got)
	}
}
