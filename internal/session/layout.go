package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Layout is where one session keeps its files, as documented in TDD §6.10.
//
// Names carry the session id so that a directory listing is self-describing: a `.events.jsonl` with no id in front
// of it in a shared directory is a file nobody can attribute, and a damaged one cannot be matched back to the
// session it belonged to.
type Layout struct {
	Dir       string
	Canonical string
	Events    string
	Index     string
	Lock      string
	Outputs   string
}

// NewLayout names a session's files without touching the filesystem.
func NewLayout(sessionsDir, id string) Layout {
	file := func(suffix string) string { return filepath.Join(sessionsDir, id+suffix) }
	return Layout{
		Dir:       sessionsDir,
		Canonical: file(".jsonl"),
		Events:    file(".events.jsonl"),
		Index:     file(".index.json"),
		Lock:      file(".lock"),
		Outputs:   file(".outputs"),
	}
}

// Modes are the permissions the layout is created with, and they differ for a reason.
//
// A transcript is as sensitive as the conversation in it, so it is owner-only. An index holds a title, a preview
// and counts — enough for a picker, nothing a reader could reconstruct a prompt from — so 0644 is deliberate rather
// than an oversight: it is the one file a same-machine tool may read.
const (
	// ModeTranscript is the mode for the canonical and event files.
	ModeTranscript os.FileMode = 0o600
	// ModeIndex is the mode for the picker's cache.
	ModeIndex os.FileMode = 0o644
	// ModeDir is the mode for the session's own directory.
	ModeDir os.FileMode = 0o700
)

// ErrLayoutIncomplete reports a directory that exists but is missing a file it must have.
var ErrLayoutIncomplete = errors.New("the session directory is incomplete")

// Create makes the session directory and everything in it, with the documented modes.
//
// The files are created empty rather than on first write, so that a session's existence is a fact about the
// filesystem rather than an inference from whether anything has been said yet. A picker that had to guess would
// either hide new sessions or invent them.
func Create(layout Layout) error {
	if err := os.MkdirAll(layout.Dir, ModeDir); err != nil {
		return fmt.Errorf("creating the session directory: %w", err)
	}
	// MkdirAll applies the mode only to directories it creates, and umask can still narrow it; setting it again is
	// the only way to know what the mode actually is.
	if err := os.Chmod(layout.Dir, ModeDir); err != nil {
		return fmt.Errorf("setting the session directory mode: %w", err)
	}
	if err := os.MkdirAll(layout.Outputs, ModeDir); err != nil {
		return fmt.Errorf("creating the output directory: %w", err)
	}

	for path, mode := range map[string]os.FileMode{
		layout.Canonical: ModeTranscript,
		layout.Events:    ModeTranscript,
		layout.Index:     ModeIndex,
	} {
		if err := touch(path, mode); err != nil {
			return err
		}
	}
	return nil
}

// touch creates a file with a mode if it is not there, and leaves an existing one alone.
//
// An existing file keeps its mode on purpose: a user who tightened or loosened a permission made a decision, and
// re-applying the default on every start would silently undo it.
func touch(path string, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		// Not opened at all: opening for writing would fail on a transcript a user had made read-only, and
		// re-applying the default mode would silently undo a permission they chose.
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", filepath.Base(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode) //nolint:gosec // G304: the path comes from Layout, which names the session's own files
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Base(path), err)
	}
	return file.Close()
}

// Verify checks that the layout exists and is complete, which is what a resume does before trusting a directory.
func Verify(layout Layout) error {
	if _, err := os.Stat(layout.Dir); err != nil {
		return fmt.Errorf("the session directory: %w", err)
	}
	for _, path := range []string{layout.Canonical, layout.Events, layout.Index, layout.Outputs} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%w: %s", ErrLayoutIncomplete, filepath.Base(path))
		}
	}
	return nil
}
