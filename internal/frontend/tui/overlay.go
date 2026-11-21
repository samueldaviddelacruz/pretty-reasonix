package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// KeyOwnership is what an overlay claims about the keyboard, declared once.
//
// The design of record (§6.12.5) makes this an enum rather than a predicate for a reason a per-key check cannot answer:
// a modal that owns the keyboard wants to *swallow everything* — every rune, Tab, paste and mouse event — and a
// `OwnsKeys(key) bool` would make it enumerate them and forget one. Global bindings are handled by the caller before the
// stack is consulted, which is where Escape and Ctrl-C belong: a modal that also swallowed those would be unescapable.
type KeyOwnership int

const (
	// InputOwned leaves the composer live: a picker over a half-typed question must not throw the question away.
	InputOwned KeyOwnership = iota
	// Modal takes the keyboard and hides the composer.
	Modal
)

// String names a kind for a test failure.
//
// The default arm formats the number rather than repeating a rune by it: a negative count makes strings.Repeat panic, and
// a panic inside String would fire while something was already reporting a failure.
func (k KeyOwnership) String() string {
	switch k {
	case InputOwned:
		return "input-owned"
	case Modal:
		return "modal"
	default:
		return "keyownership(" + strconv.Itoa(int(k)) + ")"
	}
}

// Overlay is something drawn above the transcript.
//
// Every method is a declaration, and the layout reads the declarations rather than trusting each panel to behave:
// "an inactive input box rendered under a modal" becomes structurally impossible instead of a review item.
type Overlay interface {
	// Title is what the overlay is, for a stack trace and for the screen's own header.
	Title() string
	// Update handles one message and returns the overlay that replaces it, so a card can change its own state without
	// the stack knowing what a card is.
	Update(tea.Msg) (Overlay, tea.Cmd)
	// View draws it at the given width.
	View(width int) string
	// OwnsKeys declares what it does with the keyboard.
	OwnsKeys() KeyOwnership
	// ComposerVisible declares whether the composer stays drawn and live underneath.
	ComposerVisible() bool
}

// Base is embedded by an overlay that only cares about one of the two declarations.
//
// It exists because the interface would otherwise force every notification toast and help panel to answer a question
// about the composer. The defaults are the least surprising pair: a title, and a composer that stays live.
type Base struct {
	// Name is what Title returns.
	Name string
}

// Title returns the name.
func (b Base) Title() string { return b.Name }

// ComposerVisible keeps the composer live, which is right for anything that is not taking the keyboard.
func (Base) ComposerVisible() bool { return true }

// OwnsKeys leaves the composer in charge by default.
func (Base) OwnsKeys() KeyOwnership { return InputOwned }

// Stack is the open overlays, topmost last.
//
// A stack rather than a single overlay because one overlay can open another — a picker inside a decision card — and the
// rules are stated about *the top*, which is the only one a keystroke can reach.
type Stack struct {
	overlays []Overlay
}

// Push puts an overlay on top. A nil overlay is refused rather than stored, because a nil in the stack would make Top
// return a non-nil interface holding nothing, and the first method call on it would panic.
func (s *Stack) Push(overlay Overlay) {
	if overlay == nil {
		return
	}
	s.overlays = append(s.overlays, overlay)
}

// Pop removes the topmost overlay. It is a no-op on an empty stack, so a handler need not check first.
func (s *Stack) Pop() {
	if len(s.overlays) == 0 {
		return
	}
	last := len(s.overlays) - 1
	// Cleared rather than truncated past, so the popped overlay is not kept alive by the backing array.
	s.overlays[last] = nil
	s.overlays = s.overlays[:last]
}

// Top is the overlay a keystroke reaches first, or nil when nothing is open.
func (s *Stack) Top() Overlay {
	if len(s.overlays) == 0 {
		return nil
	}
	return s.overlays[len(s.overlays)-1]
}

// Depth is how many overlays are open.
func (s *Stack) Depth() int { return len(s.overlays) }

// Swallows reports whether the keyboard belongs to an overlay rather than the composer.
//
// Two ways to be true, and the second is not obvious: an overlay that declares Modal takes it, and so does a stack whose
// composer is hidden — because if no composer is drawn, keys routed "to the composer" go nowhere at all. The first version
// checked only the top, so a picker over a decision card reported "not swallowed" while the composer was hidden, and every
// keystroke was delivered to an input box nobody could see.
//
// Global bindings — Escape, Ctrl-C, page keys — are the caller's to handle *before* asking this, which is the one thing an
// overlay must not be able to take away.
func (s *Stack) Swallows() bool {
	return s.Modal() || !s.ComposerVisible()
}

// Modal reports whether any open overlay declared itself modal.
func (s *Stack) Modal() bool {
	for _, overlay := range s.overlays {
		if overlay.OwnsKeys() == Modal {
			return true
		}
	}
	return false
}

// ComposerVisible reports whether the composer is drawn and live.
//
// Three conditions, and the middle one is the one that matters: no open overlay may be modal, none may say the composer is
// hidden, and therefore the composer is visible only when every overlay agrees. A modal hides it by *declaring itself
// modal* — not by separately remembering to say so — which is what makes the two declarations impossible to leave
// disagreeing. The first version required both, so a modal that embedded the defaults claimed the keyboard and **left the
// composer on screen**: exactly the state U2 exists to make unreachable.
func (s *Stack) ComposerVisible() bool {
	if s.Modal() {
		return false
	}
	for _, overlay := range s.overlays {
		if !overlay.ComposerVisible() {
			return false
		}
	}
	return true
}

// BottomHeight is how many rows the pinned bottom region needs.
//
// Both halves are counted: the composer when it is visible, and the status line always. An earlier version returned the
// composer alone and called it the bottom region, which under a modal returned zero while the status line was still
// drawn — and a layout that reserved zero then overdrew the transcript by one row.
func (s *Stack) BottomHeight(composerHeight, statusHeight int) int {
	height := max(statusHeight, 0)
	if s.ComposerVisible() {
		height += max(composerHeight, 0)
	}
	return height
}
