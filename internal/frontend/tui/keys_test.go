// White-box: the model, its composer and its stack are unexported, and the key path is the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ready makes a model sized for a terminal, which is the state key handling assumes.
func ready(t *testing.T, send func(string) error) *model {
	t.Helper()
	m := newModel(Options{Now: time.Now, Send: send, Height: 24, Width: 80})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// typeInto sends each rune as a key press, the way a terminal does.
func typeInto(m *model, text string) {
	for _, r := range text {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestTypingReachesTheComposer, the clause nothing tested before: the rules existed and no key reached them.
func TestTypingReachesTheComposer(t *testing.T) {
	t.Parallel()
	m := ready(t, nil)
	typeInto(m, "hi there")

	if got := m.composer.Raw(); got != "hi there" {
		t.Errorf("the composer holds %q", got)
	}
}

// TestEnterSubmitsAndClears, and the text goes to the caller rather than nowhere.
func TestEnterSubmitsAndClears(t *testing.T) {
	t.Parallel()
	var sent []string
	m := ready(t, func(text string) error { sent = append(sent, text); return nil })
	typeInto(m, "what does go.mod declare")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(sent) != 1 || sent[0] != "what does go.mod declare" {
		t.Fatalf("Send received %v", sent)
	}
	if !m.composer.Empty() {
		t.Errorf("the composer still holds %q after a send", m.composer.Raw())
	}
}

// TestAModifiedEnterDoesNotSubmit is the rule this package was fixed for, asserted through a real key press rather than a
// hand-built Key: Bubble Tea renders it as "shift+enter", which is why an equality check failed to match.
func TestAModifiedEnterDoesNotSubmit(t *testing.T) {
	t.Parallel()
	var sent []string
	m := ready(t, func(text string) error { sent = append(sent, text); return nil })
	typeInto(m, "first line")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	typeInto(m, "second line")

	if len(sent) != 0 {
		t.Fatalf("shift+enter submitted %v", sent)
	}
	if got := m.composer.Raw(); got != "first line\nsecond line" {
		t.Errorf("the composer holds %q", got)
	}
}

// TestAPasteBecomesOneTokenThroughTheProgram, which is the reason a paste is a message rather than a stream of keys: by the
// time the lines arrived separately, folding would be impossible.
func TestAPasteBecomesOneTokenThroughTheProgram(t *testing.T) {
	t.Parallel()
	m := ready(t, nil)
	m.Update(tea.PasteMsg{Content: "one\ntwo\nthree"})

	if strings.Contains(m.composer.Raw(), "two") {
		t.Errorf("the paste was inserted literally: %q", m.composer.Raw())
	}
	if m.composer.Value() != "one\ntwo\nthree" {
		t.Errorf("Value() = %q", m.composer.Value())
	}
}

// TestAnOverlayTakesTheKeys: a modal that owns the keyboard must stop the composer from acting, and Escape must still work
// because it is handled before the stack.
func TestAnOverlayTakesTheKeys(t *testing.T) {
	t.Parallel()
	m := ready(t, nil)
	m.stack.Push(modal{name: "card", ownership: Modal, composer: false})

	typeInto(m, "ignored")
	if m.composer.Raw() != "" {
		t.Errorf("a modal let the composer receive %q", m.composer.Raw())
	}

	m.stack.Pop()
	typeInto(m, "now")
	if m.composer.Raw() != "now" {
		t.Errorf("after the pop the composer holds %q", m.composer.Raw())
	}
}

// TestEscapeClosesAnOverlayWithoutReachingTheComposer, since a global binding handled after the overlay would be one the
// overlay could take away.
func TestEscapeClosesAnOverlayWithoutReachingTheComposer(t *testing.T) {
	t.Parallel()
	m := ready(t, nil)
	// Pushed through an update so the layout is derived the way the program derives it.
	m.stack.Push(modal{name: "card", ownership: Modal, composer: false})
	m.syncViewport()
	before := m.viewport.Height()

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.stack.Depth() != 0 {
		t.Errorf("escape left %d overlays open", m.stack.Depth())
	}
	// Closing the overlay gives the composer a row back, so the viewport has to shrink by one or the frame overruns.
	if got := m.viewport.Height(); got != before-1 {
		t.Errorf("the viewport is %d rows after the pop, was %d; the frame would overflow", got, before)
	}
}

// TestTheComposerResizesTheTranscript: the bottom region grows with the composer, so the viewport has to shrink by the same
// amount or the frame overruns the terminal.
func TestTheComposerResizesTheTranscript(t *testing.T) {
	t.Parallel()
	m := ready(t, nil)
	before := m.viewport.Height()

	m.composer.Newline()
	m.composer.Newline()
	m.syncViewport()

	if got := m.viewport.Height(); got != before-2 {
		t.Errorf("the viewport is %d rows after two newlines, was %d", got, before)
	}
}

// TestSendingNothingIsNotASend, so a stray Enter on an empty composer does not ask the model a blank question.
func TestSendingNothingIsNotASend(t *testing.T) {
	t.Parallel()
	var sent []string
	m := ready(t, func(text string) error { sent = append(sent, text); return nil })

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: teaspace, Text: " "})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(sent) != 0 {
		t.Errorf("an empty composer sent %v", sent)
	}
}

// teaspace is the key code for a plain space, named so the test reads as what it types.
const teaspace = ' '
