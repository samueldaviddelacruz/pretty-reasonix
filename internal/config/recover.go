package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// The suffixes this package uses beside a configuration file: the copy a malformed
// file is moved to, and the snapshot of the last configuration that parsed.
const (
	quarantineSuffix = ".bak"
	lastGoodSuffix   = ".good"
)

// QuarantinePath is where a configuration file that cannot be parsed is moved.
//
// One file rather than a series: a second bad edit replaces the first quarantine.
// That is the honest trade — keeping every attempt would fill the directory with
// copies of a file that has never once been valid — and the notice always names the
// path that now holds whatever was there.
func QuarantinePath(path string) string { return path + quarantineSuffix }

// LastGoodPath is the snapshot of the last configuration that parsed cleanly, and
// the first thing recovery tries.
func LastGoodPath(path string) string { return path + lastGoodSuffix }

// Recovery describes what happened when the user's own configuration could not be
// read: which file was moved aside, and what was used instead.
type Recovery struct {
	// Config is the effective user configuration — the last good snapshot, or the
	// built-in defaults.
	Config *Config
	// Warnings are the keys the file that was used does not define as this version
	// reads them. After a quarantine it also carries the one notice naming the two
	// paths, which is what a surface shows the user.
	Warnings []Warning
	// Quarantined is the .bak file an unparseable configuration was moved to, empty
	// when there was nothing to move.
	Quarantined string
	// Used is the file whose contents were used after quarantining, empty when the
	// built-in defaults were used. Both are also in the notice, because a caller
	// that wants to branch on the outcome and a caller that wants to print it need
	// different shapes of the same fact.
	Used string
}

// Recovered reports whether a file had to be moved aside.
func (r Recovery) Recovered() bool { return r.Quarantined != "" }

// KeepGood records path as the last configuration that worked, which is what
// LoadUserRecovering falls back to.
//
// The writer is the repair path — `prx doctor repair` and the setup flow, both E19.
// This package will not write a snapshot as a side effect of reading, because a read
// that writes is a read that fails on a read-only home.
func KeepGood(path string) error {
	body, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the configuration file
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return writeOwnerOnly(LastGoodPath(path), body)
}

// LoadUserRecovering reads the user's own configuration, recovering rather than failing.
//
// A malformed file is copied to QuarantinePath, the last good snapshot is used if there is
// one and the defaults otherwise, and one notice names both paths. A failing copy returns
// the parse error instead: recovering without preserving what the user wrote destroys it.
// Project files are never treated this way — they are committed, and quarantining one would
// write into somebody's working tree.
func LoadUserRecovering(path string) (*Recovery, error) {
	if path == "" {
		return &Recovery{Config: Default()}, nil
	}
	body, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the user's own configuration
	if errors.Is(err, fs.ErrNotExist) {
		return &Recovery{Config: Default()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading configuration %s: %w", path, err)
	}

	cfg, warnings, parseErr := Parse(body)
	if parseErr == nil {
		return &Recovery{Config: cfg, Warnings: warnings}, nil
	}
	if !errors.Is(parseErr, ErrMalformed) {
		return nil, parseErr
	}

	// Move the file aside before anything else, so a failure here loses nothing. If
	// the move fails the file is left exactly where it is, and the error says so
	// while still carrying ErrMalformed — a caller needs to know both that the
	// configuration was unreadable and that its only copy is untouched.
	quarantined := QuarantinePath(path)
	if moveErr := writeOwnerOnly(quarantined, body); moveErr != nil {
		return nil, errors.Join(parseErr, fmt.Errorf(
			"moving %s aside failed, so it has been left where it is: %w", path, moveErr))
	}

	recovered := &Recovery{Config: Default(), Quarantined: quarantined}
	notice := fmt.Sprintf("it could not be parsed; it was moved to %s and every setting is back at its built-in default", quarantined)
	if goodBody, readErr := os.ReadFile(LastGoodPath(path)); readErr == nil {
		if lastGood, _, restoreErr := Parse(goodBody); restoreErr == nil {
			recovered.Config = lastGood
			recovered.Used = LastGoodPath(path)
			notice = fmt.Sprintf("it could not be parsed; it was moved to %s and the last good configuration at %s was used instead", quarantined, recovered.Used)
		}
	}
	recovered.Warnings = []Warning{{Source: path, Key: "configuration", Note: notice}}
	return recovered, nil
}

// RenderNotices writes every notice a load produced, one per line, in the form every surface shows
// it. One renderer rather than three, because a notice a user meets in two places should not read
// differently in each. Refusals come first: a stale key is a compatibility matter, while a project
// asking for a permission it may not have is the line somebody should notice.
func RenderNotices(w io.Writer, warnings []Warning, ignored IgnoredProjectSettings) error {
	for _, refused := range ignored {
		if _, err := fmt.Fprintf(w, "not applied: %s\n", refused); err != nil {
			return fmt.Errorf("writing a refusal: %w", err)
		}
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", warning); err != nil {
			return fmt.Errorf("writing a warning: %w", err)
		}
	}
	return nil
}

// writeOwnerOnly writes a copy of a configuration beside another, owner-only since the original may
// name an api_key_env.
//
// G703 reads the derived path as traversal because it cannot see where it came from: the path is the
// caller's own configuration file plus a fixed suffix, never anything a project or a model supplies.
func writeOwnerOnly(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // G703: the caller's own configuration file plus a fixed suffix
		return fmt.Errorf("writing %s: %w", path, err)
	}
	// The mode a write is given is subject to the umask, so it is set explicitly.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restricting %s: %w", path, err)
	}
	return nil
}
