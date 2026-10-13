package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
)

// TestAMalformedUserConfigurationIsQuarantinedAndRecovered is the criterion's second
// half: the file is moved aside, the last good configuration is used, and one notice
// names both paths.
func TestAMalformedUserConfigurationIsQuarantinedAndRecovered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "default_model = \"vendor/good\"\n")
	if err := config.KeepGood(path); err != nil {
		t.Fatalf("KeepGood: %v", err)
	}
	malformed := "default_model = \"vendor/model\"\n[ui\nmystery = \n"
	writeTemp(t, dir, "config.toml", malformed)

	recovered, err := config.LoadUserRecovering(path)
	if err != nil {
		t.Fatalf("LoadUserRecovering: %v", err)
	}
	if !recovered.Recovered() {
		t.Fatal("a configuration that cannot be parsed was not moved aside")
	}
	if recovered.Config.DefaultModel != "vendor/good" {
		t.Errorf("default_model = %q, want the last good snapshot", recovered.Config.DefaultModel)
	}
	if recovered.Used != config.LastGoodPath(path) {
		t.Errorf("Used = %q, want the snapshot path", recovered.Used)
	}

	// What the user wrote is preserved, byte for byte: recovery that lost it would be
	// worse than the failure it is recovering from.
	quarantined, readErr := os.ReadFile(recovered.Quarantined)
	if readErr != nil {
		t.Fatalf("reading the quarantine: %v", readErr)
	}
	if string(quarantined) != malformed {
		t.Errorf("the quarantine holds %q, want what the user wrote", quarantined)
	}

	if len(recovered.Warnings) != 1 {
		t.Fatalf("want exactly one notice, got %d: %v", len(recovered.Warnings), recovered.Warnings)
	}
	notice := recovered.Warnings[0].String()
	for _, want := range []string{recovered.Quarantined, recovered.Used} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not name %s: %s", want, notice)
		}
	}
}

// TestTheDefaultsAreTheFallbackWithoutASnapshot: with nothing to fall back to, every
// setting is at its built-in default, and the notice says so rather than naming a file
// that does not exist.
func TestTheDefaultsAreTheFallbackWithoutASnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "not = valid = toml\n")

	recovered, err := config.LoadUserRecovering(path)
	if err != nil {
		t.Fatalf("LoadUserRecovering: %v", err)
	}
	if recovered.Config.DefaultModel != config.Default().DefaultModel {
		t.Errorf("default_model = %q, want the built-in default", recovered.Config.DefaultModel)
	}
	if recovered.Used != "" {
		t.Errorf("Used = %q, want it empty when the defaults were used", recovered.Used)
	}
	if len(recovered.Warnings) != 1 {
		t.Fatalf("want exactly one notice, got %v", recovered.Warnings)
	}
	if !strings.Contains(recovered.Warnings[0].Note, "built-in default") {
		t.Errorf("the notice says %q, want it to say the defaults were used", recovered.Warnings[0].Note)
	}
	if !strings.Contains(recovered.Warnings[0].Note, recovered.Quarantined) {
		t.Error("the notice does not name where the file was moved to")
	}
}

// TestAHealthyConfigurationIsUsedUnchanged is the control in the other direction: a
// file that parses must never be quarantined, whatever it contains.
func TestAHealthyConfigurationIsUsedUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "default_model = \"vendor/model\"\n[ui]\nmystery = 1\n")

	recovered, err := config.LoadUserRecovering(path)
	if err != nil {
		t.Fatalf("LoadUserRecovering: %v", err)
	}
	if recovered.Recovered() {
		t.Error("a configuration that parsed was moved aside")
	}
	if recovered.Config.DefaultModel != "vendor/model" {
		t.Errorf("default_model = %q, want the file's value", recovered.Config.DefaultModel)
	}
	if len(recovered.Warnings) != 1 || recovered.Warnings[0].Key != "ui.mystery" {
		t.Errorf("warnings = %v, want the unknown key reported and nothing else", recovered.Warnings)
	}
	if _, statErr := os.Stat(config.QuarantinePath(path)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("a quarantine was written for a file that parsed")
	}
}

// TestAMissingUserConfigurationIsOrdinary: a first run has no file, and that is not a
// recovery.
func TestAMissingUserConfigurationIsOrdinary(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", t.TempDir() + "/absent.toml"} {
		recovered, err := config.LoadUserRecovering(path)
		if err != nil {
			t.Fatalf("LoadUserRecovering(%q): %v", path, err)
		}
		if recovered.Recovered() || len(recovered.Warnings) != 0 {
			t.Errorf("LoadUserRecovering(%q) recovered something: %+v", path, recovered)
		}
	}
}

// TestAQuarantineThatCannotBeWrittenRefusesToRecover pins the property that matters
// most here: recovery must never destroy the only copy of what the user wrote.
func TestAQuarantineThatCannotBeWrittenRefusesToRecover(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "not = valid = toml\n")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("making the directory read-only: %v", err)
	}
	// The cleanup restores the mode, because t.TempDir cannot remove what it cannot
	// write into.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := config.LoadUserRecovering(path); err == nil {
		t.Fatal("recovery proceeded without preserving the file it could not move")
	} else if !errors.Is(err, config.ErrMalformed) {
		t.Errorf("the error does not carry ErrMalformed, so a caller cannot tell what happened: %v", err)
	}
	// And the original is still exactly where it was.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("the user's file is gone: %v", statErr)
	}
}

// TestAProjectFileIsNeverQuarantined is the deliberate asymmetry: a committed file is
// its author's to fix, and writing a .bak into somebody's repository is not ours.
func TestAProjectFileIsNeverQuarantined(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	project := writeTemp(t, dir, config.ProjectConfigFile, "not = valid = toml\n")

	if _, err := config.Load(project, "", config.Flags{}); err == nil {
		t.Fatal("a malformed project configuration was accepted")
	} else if !errors.Is(err, config.ErrMalformed) {
		t.Errorf("the error does not carry ErrMalformed: %v", err)
	}
	if _, statErr := os.Stat(config.QuarantinePath(project)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("the loader wrote a quarantine into a project directory")
	}
}

// TestKeepGoodIsWhatRecoveryFallsBackTo pairs the two halves of the mechanism, so that
// the writer and the reader cannot drift into disagreeing about the suffix.
func TestKeepGoodIsWhatRecoveryFallsBackTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "default_model = \"vendor/good\"\n")
	if err := config.KeepGood(path); err != nil {
		t.Fatalf("KeepGood: %v", err)
	}
	if info, err := os.Stat(config.LastGoodPath(path)); err != nil {
		t.Fatalf("KeepGood wrote nothing: %v", err)
	} else if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the snapshot is %#o, want it owner-only like the file it copies", mode)
	}

	writeTemp(t, dir, "config.toml", "default_model = broken\n")
	recovered, err := config.LoadUserRecovering(path)
	if err != nil {
		t.Fatalf("LoadUserRecovering: %v", err)
	}
	if recovered.Config.DefaultModel != "vendor/good" {
		t.Errorf("default_model = %q, want the snapshot KeepGood wrote", recovered.Config.DefaultModel)
	}
}

// TestRenderNotices checks the one renderer the surfaces share.
func TestRenderNotices(t *testing.T) {
	t.Parallel()
	// Both paths are built from the identity constants rather than written out: the
	// scanner in internal/architecture refuses a literal, and it is right to — these
	// are exactly the strings that would go stale in a rename.
	userPath := filepath.Join("/home", "tester", config.HomeDirName, "config.toml")
	projectPath := filepath.Join("/work", config.ProjectConfigFile)

	warnings := []config.Warning{{Source: userPath, Key: "ui.mystery", Note: "unknown here"}}
	ignored := config.IgnoredProjectSettings{{
		Source: projectPath,
		Key:    "sandbox.network",
		Note:   "a project may not turn network access on; the user's value stands",
	}}

	var out strings.Builder
	if err := config.RenderNotices(&out, warnings, ignored); err != nil {
		t.Fatalf("RenderNotices: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"not applied: " + projectPath + ": sandbox.network",
		"warning: " + userPath + ": ui.mystery",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the notices do not contain %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "not applied") > strings.Index(text, "warning") {
		t.Error("a compatibility warning was printed before a refusal")
	}

	// A surface that has gone away — a closed terminal, a broken pipe — must produce
	// an error rather than lose the notice silently.
	if err := config.RenderNotices(failingWriter{}, warnings, ignored); err == nil {
		t.Error("a write failure was swallowed")
	}
	if err := config.RenderNotices(failingWriter{}, nil, ignored); err == nil {
		t.Error("a write failure was swallowed on the refusal path")
	}
}

// failingWriter stands in for a surface that is no longer there.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("the surface is gone")
}
