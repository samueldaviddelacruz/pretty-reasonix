package tui

// Overlay is something drawn above the transcript that takes some of the keystrokes.
//
// Two declarations, and both exist to remove a special case rather than add one:
//
//   - OwnsKeys says whether a key is this overlay's. Every input handler asks the stack rather than each overlay, so
//     adding a fourth modal cannot forget to stop the composer from also acting on Enter.
//   - ComposerVisible says whether the composer is drawn and active underneath. A modal that owns the keyboard while the
//     composer still renders below it is the bug U2 names: the person types into something that is not listening.
type Overlay interface {
	// ID names the overlay, for a stack trace and for a test failure worth reading.
	ID() string
	// OwnsKeys reports whether this overlay claims a key.
	OwnsKeys(key string) bool
	// ComposerVisible reports whether the composer is drawn while this overlay is on top.
	ComposerVisible() bool
}

// Stack is the open overlays, topmost last.
//
// A stack rather than a single overlay because one modal can open another — a picker inside a decision card — and the
// rules below are stated about *the top*, which is the only one a keystroke can reach.
type Stack struct {
	overlays []Overlay
}

// Push puts an overlay on top.
func (s *Stack) Push(overlay Overlay) { s.overlays = append(s.overlays, overlay) }

// Pop removes the topmost overlay. It is a no-op on an empty stack, so a handler does not have to check.
func (s *Stack) Pop() {
	if len(s.overlays) > 0 {
		s.overlays = s.overlays[:len(s.overlays)-1]
	}
}

// Top is the overlay a keystroke reaches first, or nil when nothing is open.
func (s *Stack) Top() Overlay {
	if len(s.overlays) == 0 {
		return nil
	}
	return s.overlays[len(s.overlays)-1]
}

// Depth is how many overlays are open, which is what a test asserts when the question is "did it pop".
func (s *Stack) Depth() int { return len(s.overlays) }

// Handles reports whether the top overlay claims a key.
//
// Only the top, deliberately. A key falls through to the composer or nowhere — it never reaches an overlay *below* the
// top, because a modal exists precisely to stop the thing underneath from reacting.
func (s *Stack) Handles(key string) bool {
	top := s.Top()
	return top != nil && top.OwnsKeys(key)
}

// ComposerVisible reports whether the composer is drawn and active.
//
// The stack answers rather than the layout, so the rule is stated once: with no overlay the composer is visible, and with
// one it is visible only if that overlay says so. A layout that asked each overlay itself would grow a special case per
// modal, which is the thing this ticket is for.
func (s *Stack) ComposerVisible() bool {
	top := s.Top()
	return top == nil || top.ComposerVisible()
}

// BottomHeight derives the height of the pinned bottom region.
//
// The composer's own height is subtracted when it is hidden, which is what makes an inactive composer impossible to
// render under a modal: the layout is told how much room to reserve, and with the composer hidden it reserves none.
// The status line is not part of this: it is always drawn, because a person needs to know what mode they are in most
// while a modal has taken the screen.
func (s *Stack) BottomHeight(composerHeight int) int {
	if !s.ComposerVisible() {
		return 0
	}
	return max(composerHeight, 0)
}
