package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrNoSession reports a query that names nothing.
	ErrNoSession = errors.New("no session matches that")
	// ErrAmbiguousSession reports a query that names more than one, which is refused rather than guessed at: opening
	// the wrong conversation is worse than being asked again.
	ErrAmbiguousSession = errors.New("more than one session matches that")
	// ErrSessionExists reports a copy onto a name already in use, which would otherwise overwrite a conversation.
	ErrSessionExists = errors.New("a session with that id already exists")
)

// NewID names a session after when it started, with a suffix so two starts in the same second cannot collide.
//
// The time is a parameter rather than read here, for the same reason every timestamp in this package is: a function
// that reads the clock cannot be tested and a name that depends on one is a name that cannot be predicted. The suffix
// comes from the operating system's random source rather than a counter, because two processes starting at the same
// instant is exactly the case a counter would get wrong.
func NewID(now time.Time) (string, error) {
	suffix := make([]byte, 2)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("naming the session: %w", err)
	}
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(suffix), nil
}

// Newest reports the most recently written session, or false when there is none.
//
// "Most recently written" is the log's modification time rather than its name, because a session resumed today was
// started in the past: ordering by name would treat yesterday's resumed conversation as older than today's empty one.
func Newest(sessionsDir string) (Layout, bool, error) {
	entries, err := os.ReadDir(sessionsDir)
	if errors.Is(err, os.ErrNotExist) {
		return Layout{}, false, nil
	}
	if err != nil {
		return Layout{}, false, fmt.Errorf("listing sessions: %w", err)
	}

	var newest Layout
	var newestAt time.Time
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".events.jsonl") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		if !found || info.ModTime().After(newestAt) {
			id := strings.TrimSuffix(entry.Name(), ".events.jsonl")
			newest = NewLayout(sessionsDir, id)
			newestAt = info.ModTime()
			found = true
		}
	}
	return newest, found, nil
}

// Candidate is one session a query could mean, with what a person needs to choose between them.
type Candidate struct {
	Layout Layout
	Title  string
}

// Resolve finds the session a query names.
//
// Three forms, in the order a person is likely to mean them: an exact id, a path to a session's file, then a
// substring of the id, the title or the preview. Missing and ambiguous are distinct errors because the next action
// differs — one means "start a new session", the other means "you were more specific than the list, be specific
// again" — and a message that said only "not found" for both would be wrong half the time.
func Resolve(sessionsDir, query string, now time.Time) (Layout, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return Layout{}, ErrNoSession
	}

	// A path names a file the person can see, so it wins: if they typed one, they meant that one.
	if looksLikePath(trimmed) {
		if layout, ok := layoutForPath(sessionsDir, trimmed); ok {
			return layout, nil
		}
		return Layout{}, fmt.Errorf("%w: %s is not a session in this directory", ErrNoSession, trimmed)
	}

	if layout := NewLayout(sessionsDir, trimmed); fileExists(layout.Events) {
		return layout, nil
	}

	matches, err := matchByText(sessionsDir, strings.ToLower(trimmed), now)
	if err != nil {
		return Layout{}, err
	}
	switch len(matches) {
	case 0:
		return Layout{}, fmt.Errorf("%w: nothing has %q in its id, title or preview", ErrNoSession, trimmed)
	case 1:
		return matches[0].Layout, nil
	default:
		// Named, not just counted: the caller can show the list, and a person can pick.
		titles := make([]string, 0, len(matches))
		for _, match := range matches {
			titles = append(titles, match.Title)
		}
		return Layout{}, fmt.Errorf("%w: %q matches %s", ErrAmbiguousSession, trimmed, strings.Join(titles, ", "))
	}
}

// looksLikePath reports whether a query was meant as a path rather than an id. A separator is the signal; an id never
// contains one, and a bare filename could be either, so only an explicit path is treated as one.
func looksLikePath(query string) bool { return strings.ContainsRune(query, filepath.Separator) }

// layoutForPath accepts the path to any of a session's files — the log, the transcript, the index, or the directory —
// because a person copying a path from a file listing will copy whichever one they were looking at.
func layoutForPath(sessionsDir, query string) (Layout, bool) {
	absolute, err := filepath.Abs(query)
	if err != nil {
		return Layout{}, false
	}
	base := filepath.Base(absolute)
	for _, suffix := range []string{".events.jsonl", ".jsonl", ".index.json", ".lock"} {
		if id, found := strings.CutSuffix(base, suffix); found {
			layout := NewLayout(filepath.Dir(absolute), id)
			if fileExists(layout.Events) {
				return layout, true
			}
			return Layout{}, false
		}
	}
	return Layout{}, false
}

// matchByText reads the indexes and matches a substring, repairing any that are stale so a search cannot miss a
// session because its cache was wrong.
func matchByText(sessionsDir, needle string, now time.Time) ([]Candidate, error) {
	entries, err := os.ReadDir(sessionsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	var matches []Candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".events.jsonl") {
			continue
		}
		layout := NewLayout(sessionsDir, strings.TrimSuffix(entry.Name(), ".events.jsonl"))
		index, _, indexErr := LoadOrRebuild(layout, "", now)
		if indexErr != nil {
			// A session whose log cannot be read is skipped rather than failing the whole search: a picker that
			// refuses to open because one conversation is damaged is worse than one that lists the others.
			continue
		}
		if strings.Contains(strings.ToLower(layout.Events), needle) ||
			strings.Contains(strings.ToLower(index.Title), needle) ||
			strings.Contains(strings.ToLower(index.Preview), needle) {
			matches = append(matches, Candidate{Layout: layout, Title: index.Title})
		}
	}
	return matches, nil
}

// Copy duplicates a session under a new id, leaving the original untouched.
//
// The copy is made by replaying the log rather than by copying bytes, so the new session is a session rather than a
// file that happens to look like one: fresh revision numbers, a chain that starts at zero, and an index that describes
// its own log. The original is opened read-only and never written.
func Copy(from Layout, toID string, now time.Time) (Layout, error) {
	messages, err := ReadTranscript(from.Events, from.Canonical)
	if err != nil {
		return Layout{}, fmt.Errorf("reading the session to copy: %w", err)
	}

	to := NewLayout(from.Dir, toID)
	if fileExists(to.Events) {
		return Layout{}, fmt.Errorf("%w: %s", ErrSessionExists, toID)
	}
	if createErr := Create(to); createErr != nil {
		return Layout{}, createErr
	}

	log, err := CreateLog(to.Events, to.Canonical)
	if err != nil {
		return Layout{}, err
	}
	defer func() { _ = log.Close() }()

	for _, message := range messages {
		if message.Content == "" {
			// Host-authored entries may be empty; they are still part of the record, so they are written with a
			// placeholder rather than dropped. A copy that quietly lost entries would be worse than a noisy one.
			message.Content = " "
		}
		if err := log.Append(message); err != nil {
			return Layout{}, fmt.Errorf("copying a message: %w", err)
		}
	}
	if _, err := Rebuild(to, "", now); err != nil {
		return Layout{}, err
	}
	return to, nil
}

// fileExists is the whole of "is there anything here", which is all the callers need.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
