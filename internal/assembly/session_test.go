// White-box: openSession is unexported and is the decision these tests are about. Driving it through Build would need a
// configured provider and a real model.
//
//nolint:testpackage // the subject is unexported
package assembly

import (
	"errors"
	"os"
	"testing"
	"time"

	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// sessions builds a configuration pointing at a temporary sessions directory.
func sessions(t *testing.T) config.Paths {
	t.Helper()
	home := config.Resolve(t.TempDir())
	// The directory only: a seeded session here would make "nothing to continue" impossible to test.
	if err := os.MkdirAll(home.SessionsDir, 0o700); err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	return home
}

// seed writes one session with one message and returns its layout.
func seed(t *testing.T, paths config.Paths, id, said string) session.Layout {
	t.Helper()
	layout := session.NewLayout(paths.SessionsDir, id)
	transcript, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	transcript.Append(session.Message{Role: provider.RoleUser, Content: said})
	if closeErr := transcript.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	return layout
}

// TestWhichSessionARunOpens is the test whose absence let both review findings through: the flags were parsed and never
// forwarded, and a copy handed a just-created session to NewPersistent - neither visible without exercising this.
func TestWhichSessionARunOpens(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	t.Run("no flags starts a fresh session", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		old := seed(t, paths, "old", "an earlier question")

		layout, resumed, _, err := openSession(paths, Options{Now: now}, "new-id")
		if err != nil {
			t.Fatalf("openSession: %v", err)
		}
		if resumed {
			t.Error("a run with no flags resumed something")
		}
		if layout.Events == old.Events {
			t.Error("a run with no flags opened the existing session")
		}
	})

	t.Run("continue takes the newest", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		seed(t, paths, "older", "first")
		newest := seed(t, paths, "newer", "second")

		layout, resumed, _, err := openSession(paths, Options{Now: now, Continue: true}, "fresh-id")
		if err != nil {
			t.Fatalf("openSession: %v", err)
		}
		if !resumed || layout.Events != newest.Events {
			t.Errorf("--continue opened %s (resumed=%v), want the newest %s", layout.Events, resumed, newest.Events)
		}
	})

	t.Run("continue with nothing to continue is a fresh session", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		layout, resumed, _, err := openSession(paths, Options{Now: now, Continue: true}, "the-new-one")
		if err != nil {
			t.Fatalf("an empty directory was an error: %v", err)
		}
		if resumed {
			t.Error("a resume with nothing to resume claimed it resumed")
		}
		if layout.Events == "" {
			t.Error("no session was named")
		}
	})

	t.Run("resume takes the named session", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		seed(t, paths, "chosen", "the parser question")
		seed(t, paths, "other", "something else")

		layout, resumed, _, err := openSession(paths, Options{Now: now, Resume: "chosen"}, "fresh-id")
		if err != nil {
			t.Fatalf("openSession: %v", err)
		}
		if !resumed || layout.Events != session.NewLayout(paths.SessionsDir, "chosen").Events {
			t.Errorf("--resume opened %s", layout.Events)
		}
	})

	t.Run("an ambiguous resume is reported", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		seed(t, paths, "a", "the parser question")
		seed(t, paths, "b", "the parser rewrite")

		_, _, _, err := openSession(paths, Options{Now: now, Resume: "parser"}, "fresh-id")
		if !errors.Is(err, session.ErrAmbiguousSession) {
			t.Fatalf("an ambiguous resume returned %v", err)
		}
	})

	t.Run("copy opens the copy, not the original", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		original := seed(t, paths, "original", "the conversation worth keeping")

		layout, resumed, _, err := openSession(paths, Options{Now: now, Continue: true, Copy: true}, "copied-id")
		if err != nil {
			t.Fatalf("--copy failed: %v", err)
		}
		// Resumed, because the copy carries the messages: reporting otherwise would leave the transcript empty and send
		// NewPersistent at a file Copy has just written.
		if !resumed {
			t.Error("a copy did not report itself as the session to continue")
		}
		if layout.Events == original.Events {
			t.Error("a copy opened the original")
		}
		// And the copy is readable, which is what a later turn needs.
		if _, readErr := session.ReadTranscript(layout.Events, layout.Canonical); readErr != nil {
			t.Errorf("the copy is not readable: %v", readErr)
		}
	})

	t.Run("copy without a session to copy is refused", func(t *testing.T) {
		t.Parallel()
		paths := sessions(t)
		if _, _, _, err := openSession(paths, Options{Now: now, Copy: true}, "fresh-id"); err == nil {
			t.Error("--copy on its own was accepted")
		}
	})
}

// TestCloseReleasesTheWorkspaceHandle is the review finding: the directory descriptor a run is confined to was opened and
// never closed, so every session leaked one.
func TestCloseReleasesTheWorkspaceHandle(t *testing.T) {
	t.Parallel()
	paths := sessions(t)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	layout, _, _, err := openSession(paths, Options{Now: now}, "sess-close")
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	// NewPersistent creates the layout, so calling Create first would have it refuse an id already in use.
	root, err := os.OpenRoot(paths.SessionsDir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	transcript, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}

	run := &Run{Transcript: transcript, root: root}
	if closeErr := run.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if run.root != nil {
		t.Error("Close kept the workspace handle")
	}
	// And a second Close is harmless, which is what lets the caller close explicitly and defer a safety net — the same
	// arrangement the session transcript already supports.
	if closeErr := run.Close(); closeErr != nil {
		t.Errorf("a second Close returned %v", closeErr)
	}
}
