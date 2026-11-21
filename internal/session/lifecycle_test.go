package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// writeSession creates a session with one message and returns its layout.
func writeSession(t *testing.T, dir, id, said string) session.Layout {
	t.Helper()
	layout := session.NewLayout(dir, id)
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
	log, err := session.CreateLog(layout.Events, layout.Canonical)
	if err != nil {
		t.Fatalf("CreateLog %s: %v", id, err)
	}
	if err := log.Append(session.Message{Role: provider.RoleUser, Content: said}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return layout
}

// TestResolveAcceptsAnID: the plainest form, and the one a script uses because it is unambiguous by construction.
func TestResolveAcceptsAnID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := writeSession(t, dir, "20261121-120000-abcd", "first question")

	got, err := session.Resolve(dir, "20261121-120000-abcd", time.Now())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Events != want.Events {
		t.Errorf("resolved %s, want %s", got.Events, want.Events)
	}
}

// TestResolveAcceptsAnyOfTheSessionsFiles: a person copying a path from a listing copies whichever file they were
// looking at, so all of them have to work.
func TestResolveAcceptsAnyOfTheSessionsFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := writeSession(t, dir, "sess-a", "anything")

	for _, path := range []string{layout.Events, layout.Canonical, layout.Index} {
		got, err := session.Resolve(dir, path, time.Now())
		if err != nil {
			t.Errorf("Resolve(%s): %v", filepath.Base(path), err)
			continue
		}
		if got.Events != layout.Events {
			t.Errorf("Resolve(%s) gave %s", filepath.Base(path), got.Events)
		}
	}

	// The sessions directory is not itself a session: 6.10 keeps them as flat files inside it, so a directory
	// path is a mistake worth reporting rather than resolving to something that does not exist.
	if _, err := session.Resolve(dir, layout.Dir, time.Now()); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("the sessions directory resolved as a session: %v", err)
	}
}

// TestResolveMatchesTitleAndPreview: a substring is what a person remembers, and the index exists so that finding it
// costs one small read.
func TestResolveMatchesTitleAndPreview(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := writeSession(t, dir, "sess-a", "why is the sky blue")
	writeSession(t, dir, "sess-b", "unrelated question")

	for _, query := range []string{"sky", "SKY", "blue"} {
		got, err := session.Resolve(dir, query, time.Now())
		if err != nil {
			t.Errorf("Resolve(%q): %v", query, err)
			continue
		}
		if got.Events != layout.Events {
			t.Errorf("Resolve(%q) gave %s", query, got.Events)
		}
	}
}

// TestMissingAndAmbiguousAreDistinguishable is the criterion's sharpest clause: the two failures call for different
// next actions, and a single "not found" would be wrong half the time.
func TestMissingAndAmbiguousAreDistinguishable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSession(t, dir, "sess-a", "the parser question")
	writeSession(t, dir, "sess-b", "the parser rewrite")

	_, missing := session.Resolve(dir, "nothing like this", time.Now())
	if !errors.Is(missing, session.ErrNoSession) {
		t.Errorf("a missing query returned %v, want ErrNoSession", missing)
	}

	_, ambiguous := session.Resolve(dir, "parser", time.Now())
	if !errors.Is(ambiguous, session.ErrAmbiguousSession) {
		t.Errorf("an ambiguous query returned %v, want ErrAmbiguousSession", ambiguous)
	}
	if errors.Is(ambiguous, session.ErrNoSession) {
		t.Error("an ambiguous query was also reported as missing")
	}
	// The message names the candidates, so the caller can show them rather than asking blindly again.
	if !strings.Contains(ambiguous.Error(), "parser question") {
		t.Errorf("the ambiguity does not name what matched: %v", ambiguous)
	}
}

// TestNewestIsByWriteTimeNotByName, because a session resumed today was started in the past: ordering by name would
// treat yesterday's resumed conversation as older than today's empty one.
func TestNewestIsByWriteTimeNotByName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	older := writeSession(t, dir, "20260101-090000-aaaa", "started long ago")
	newer := writeSession(t, dir, "20251231-090000-bbbb", "started later")

	// A name that sorts first but was written last.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(newer.Events, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got, found, err := session.Newest(dir)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if !found {
		t.Fatal("no session was found")
	}
	if got.Events != newer.Events {
		t.Errorf("Newest gave %s, want the most recently written %s", got.Events, newer.Events)
	}
	if got.Events == older.Events {
		t.Error("the name ordering won over the write time")
	}
}

// TestNoSessionsIsNotAnError: a first run is ordinary, and continue has to be able to say so.
func TestNoSessionsIsNotAnError(t *testing.T) {
	t.Parallel()
	if _, found, err := session.Newest(filepath.Join(t.TempDir(), "absent")); err != nil || found {
		t.Fatalf("Newest on an empty directory: found=%v err=%v", found, err)
	}
}

// TestCopyLeavesTheOriginalUntouched is the criterion's third clause, asserted by comparing the original's bytes before
// and after rather than by trusting the copy path.
func TestCopyLeavesTheOriginalUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	original := writeSession(t, dir, "sess-a", "the important question")

	before, err := os.ReadFile(original.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	copied, err := session.Copy(original, "sess-copy", time.Now())
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}

	after, err := os.ReadFile(original.Events)
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the original's log changed during a copy")
	}

	// And the copy is a session in its own right: it replays, and its chain starts at zero.
	messages, err := session.ReadTranscript(copied.Events, copied.Canonical)
	if err != nil {
		t.Fatalf("reading the copy: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "the important question" {
		t.Fatalf("the copy holds %+v", messages)
	}
	index, err := session.LoadIndex(copied)
	if err != nil {
		t.Fatalf("the copy has no usable index: %v", err)
	}
	if index.Messages != 1 {
		t.Errorf("the copy's index says %d messages", index.Messages)
	}
}

// TestCopyRefusesToOverwrite: a copy onto a name in use would destroy a conversation.
func TestCopyRefusesToOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	original := writeSession(t, dir, "sess-a", "keep me")
	writeSession(t, dir, "sess-taken", "already here")

	if _, err := session.Copy(original, "sess-taken", time.Now()); !errors.Is(err, session.ErrSessionExists) {
		t.Fatalf("a copy over an existing session returned %v", err)
	}
}

// TestNewIDsDoNotCollideWithinASecond is why the suffix is random rather than a counter: two processes starting at the
// same instant is exactly the case a counter would get wrong.
func TestNewIDsDoNotCollideWithinASecond(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]bool, 50)
	for range 50 {
		id, err := session.NewID(now)
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if seen[id] {
			t.Fatalf("NewID produced %s twice in the same second", id)
		}
		seen[id] = true
	}
}
