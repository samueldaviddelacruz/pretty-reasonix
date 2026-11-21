package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pretty-reasonix/internal/session"
)

// openLayout creates a session and returns its layout for inspection.
func openLayout(t *testing.T) session.Layout {
	t.Helper()
	layout := session.NewLayout(t.TempDir(), "sess-0001")
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return layout
}

// TestTheDocumentedLayoutIsCreated is E9-T9's first clause: the names and the files, per TDD §6.10.
func TestTheDocumentedLayoutIsCreated(t *testing.T) {
	t.Parallel()
	layout := openLayout(t)

	for _, path := range []string{layout.Canonical, layout.Events, layout.Index, layout.Outputs} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not created: %v", filepath.Base(path), err)
		}
	}
	if !strings.HasSuffix(layout.Events, ".events.jsonl") || !strings.HasSuffix(layout.Index, ".index.json") {
		t.Errorf("the names do not carry the documented suffixes: %s, %s", layout.Events, layout.Index)
	}
	// The id is in front of every name, so a file in a shared directory can be attributed.
	for _, name := range []string{layout.Canonical, layout.Events, layout.Index, layout.Lock, layout.Outputs} {
		if !strings.HasPrefix(filepath.Base(name), "sess-0001") {
			t.Errorf("%s does not begin with the session id", filepath.Base(name))
		}
	}
}

// TestTheModesDifferOnPurpose is E9-T9's second clause, and the 0644 is the interesting one: the index is the file a
// same-machine tool may read, so its mode is a decision rather than an oversight.
func TestTheModesDifferOnPurpose(t *testing.T) {
	t.Parallel()
	layout := openLayout(t)

	cases := map[string]struct {
		path string
		want os.FileMode
	}{
		"canonical transcript": {layout.Canonical, session.ModeTranscript},
		"event log":            {layout.Events, session.ModeTranscript},
		"index":                {layout.Index, session.ModeIndex},
		"outputs directory":    {layout.Outputs, session.ModeDir},
	}
	for name, tc := range cases {
		info, err := os.Stat(tc.path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != tc.want {
			t.Errorf("%s is %04o, want %04o", name, got, tc.want)
		}
	}
	if session.ModeIndex == session.ModeTranscript {
		t.Fatal("the index and the transcript have the same mode, so the distinction is not real")
	}
}

// TestAnExistingModeIsNotReset: a user who tightened or loosened a permission made a decision, and a restart must
// not silently undo it.
func TestAnExistingModeIsNotReset(t *testing.T) {
	t.Parallel()
	layout := openLayout(t)
	if err := os.Chmod(layout.Events, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create again: %v", err)
	}
	info, err := os.Stat(layout.Events)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Errorf("the mode was reset to %04o", got)
	}
}

// TestVerifyCatchesAnIncompleteLayout: a resume has to refuse a directory rather than invent what is missing.
func TestVerifyCatchesAnIncompleteLayout(t *testing.T) {
	t.Parallel()
	layout := openLayout(t)
	if err := session.Verify(layout); err != nil {
		t.Fatalf("a fresh layout did not verify: %v", err)
	}

	if err := os.Remove(layout.Events); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := session.Verify(layout); !errors.Is(err, session.ErrLayoutIncomplete) {
		t.Fatalf("an incomplete layout verified: %v", err)
	}
}

// TestSpillOutputLivesUnderTheSession: spilled tool output is the biggest thing a session writes, so it must land in
// its own directory rather than beside the transcripts.
func TestSpillOutputLivesUnderTheSession(t *testing.T) {
	t.Parallel()
	layout := openLayout(t)
	if filepath.Dir(layout.Outputs) != layout.Dir {
		t.Errorf("the output directory is not inside the session's own: %s", layout.Outputs)
	}
	info, err := os.Stat(layout.Outputs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("the output path is not a directory")
	}
}
