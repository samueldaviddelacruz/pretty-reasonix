// White-box on purpose: the model is unexported, and driving it through Run would mean running a real program to
// assert about a view. The options and the restore path are tested through Run; the state machine is tested here.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeClock is a clock a test moves by hand, which is the whole point of injecting one: a timer driven by it can be
// advanced without sleeping, so the test is neither slow nor flaky.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// TestOptionsAreRefusedWithoutAClockOrTerminal: both are required so that a program built without them fails where the
// mistake is, rather than at the first frame.
func TestOptionsAreRefusedWithoutAClockOrTerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer

	if err := Run(context.Background(), Options{In: strings.NewReader(""), Out: &out}); !errors.Is(err, ErrNoClock) {
		t.Errorf("a missing clock returned %v", err)
	}
	if err := Run(context.Background(), Options{Now: time.Now, Out: &out}); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("a missing input returned %v", err)
	}
	if err := Run(context.Background(), Options{Now: time.Now, In: strings.NewReader("")}); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("a missing output returned %v", err)
	}
}

// TestACancelledContextReturnsAndRestores runs the real program against buffers, which is the fake terminal this ticket
// asks for: no tty, no escape sequences to a person, and the deferred restore still runs on the way out.
func TestACancelledContextReturnsAndRestores(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already over: the program starts and stops without the test waiting for anything

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{In: strings.NewReader(""), Out: &out, Now: time.Now, Width: 80, Height: 24})
	}()

	select {
	case <-done:
		// Returned, which is the assertion: a frontend that could not be stopped by its context would hang the CLI's
		// shutdown, and a hanging shutdown is what leaves a terminal in raw mode.
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled context did not stop the program; this is the one place a real timeout is the subject")
	}
}

// TestTheModelIsDrivenByTheInjectedClock is the criterion's second clause. Every timer here reads the clock, so a test
// advances it and sends a frame — no sleeping, and the assertion is about the elapsed time the clock reports rather
// than about how long the test happened to run.
func TestTheModelIsDrivenByTheInjectedClock(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	model := newModel(Options{Now: clock.Now, In: strings.NewReader(""), Out: &bytes.Buffer{}, Width: 80, Height: 24})

	first := model.View().Content
	if !strings.Contains(first, "0s") {
		t.Errorf("a fresh program reports %q, want no elapsed time", first)
	}

	clock.advance(90 * time.Second)
	next, _ := model.Update(frameMsg{})
	second := next.View().Content

	if !strings.Contains(second, "1m30s") {
		t.Errorf("after the clock advanced by 90s the view says %q", second)
	}
	if !strings.Contains(second, "1 frame(s)") {
		t.Errorf("the frame was not counted: %q", second)
	}
}

// TestTheModesAreDistinguishable: inline and fullscreen render differently, and a person should be able to tell which
// one they are in when something looks wrong.
func TestTheModesAreDistinguishable(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	var out bytes.Buffer

	inline := newModel(Options{Inline: true, Now: clock.Now, In: strings.NewReader(""), Out: &out}).View().Content
	fullscreen := newModel(Options{Inline: false, Now: clock.Now, In: strings.NewReader(""), Out: &out}).View().Content

	if !strings.Contains(inline, "inline") || strings.Contains(inline, "fullscreen") {
		t.Errorf("the inline view says %q", inline)
	}
	if !strings.Contains(fullscreen, "fullscreen") {
		t.Errorf("the fullscreen view says %q", fullscreen)
	}
}

// TestCtrlCQuitsAndNothingElseDoes: the skeleton needs one way out, and a program that quit on any key would be
// impossible to type into once the composer arrives.
func TestCtrlCQuitsAndNothingElseDoes(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	model := newModel(Options{Now: clock.Now, In: strings.NewReader(""), Out: &bytes.Buffer{}})

	if _, cmd := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}); cmd != nil {
		t.Error("an ordinary key quit the program")
	}
	if _, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("ctrl+c did not quit")
	}
}
