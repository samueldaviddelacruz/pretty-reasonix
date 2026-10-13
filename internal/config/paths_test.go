package config_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
)

// TestLayoutMatchesTheDocumentedPaths checks the table against the layout in
// docs/TECHNICAL_DESIGN.md §6.10, rather than against itself. A table that only
// agrees with its own test says nothing about the design.
func TestLayoutMatchesTheDocumentedPaths(t *testing.T) {
	t.Parallel()
	documented := map[string]string{
		"Home":         ".",
		"ConfigFile":   "config.toml",
		"EnvFile":      ".env",
		"TrustFile":    "trust.json",
		"CommandsDir":  "commands",
		"SkillsDir":    "skills",
		"SettingsFile": "settings.json",
		"SessionsDir":  "sessions",
		"LogsDir":      "logs",
	}
	got := make(map[string]string, len(config.Layout))
	for _, loc := range config.Layout {
		got[loc.Field] = loc.Rel
	}
	for field, want := range documented {
		if got[field] != want {
			t.Errorf("%s resolves to %q, want %q", field, got[field], want)
		}
	}
	for field := range got {
		if _, ok := documented[field]; !ok {
			t.Errorf("%s is in the layout but not in docs/TECHNICAL_DESIGN.md §6.10; either document it or remove it", field)
		}
	}
}

// TestResolveAgreesWithTheLayout is the drift guard: it fails both when a Paths
// field is missing from the table, and when Resolve builds a path the table does
// not describe. Without it the two could describe different layouts and every
// other test here would still pass.
func TestResolveAgreesWithTheLayout(t *testing.T) {
	t.Parallel()
	home := filepath.Join("/home", "tester", config.HomeDirName)
	paths := config.Resolve(home)

	rel := make(map[string]string, len(config.Layout))
	for _, loc := range config.Layout {
		rel[loc.Field] = loc.Rel
	}

	values := reflect.ValueOf(paths)
	types := values.Type()
	for i := range types.NumField() {
		name := types.Field(i).Name
		want, known := rel[name]
		if !known {
			t.Errorf("Paths.%s is not in config.Layout, so nothing asserts where it points", name)
			continue
		}
		if want == "." {
			want = home
		} else {
			want = filepath.Join(home, want)
		}
		if got := values.Field(i).String(); got != want {
			t.Errorf("Paths.%s = %q, want %q", name, got, want)
		}
	}
}

// TestLayoutModesAndPurposesAreSensible checks the invariants the modes are
// there to express, so that a new entry has to be deliberate about both.
func TestLayoutModesAndPurposesAreSensible(t *testing.T) {
	t.Parallel()
	for _, loc := range config.Layout {
		switch {
		case loc.Dir && loc.Mode.Perm() != 0o700:
			t.Errorf("%s is a directory with mode %#o: the tree holds credentials and transcripts, so only the owner may traverse it", loc.Field, loc.Mode.Perm())
		case !loc.Dir && loc.Mode.Perm()&0o111 != 0:
			t.Errorf("%s is a file with execute bits set (%#o)", loc.Field, loc.Mode.Perm())
		case loc.Purpose == "":
			t.Errorf("%s has no purpose; `prx doctor` and a layout failure both need one", loc.Field)
		case strings.Contains(loc.Rel, ".."):
			t.Errorf("%s resolves to %q, which escapes the home directory", loc.Field, loc.Rel)
		}
	}
}

// TestHomeHonoursTheEnvironmentVar uses a real temporary directory rather than a
// made-up path, so the assertion is about the value Home returns and not about
// whether the directory exists.
//
// It cannot be parallel: t.Setenv owns the process environment, and t.Parallel
// forbids the combination.
func TestHomeHonoursTheEnvironmentVar(t *testing.T) {
	relocated := t.TempDir()
	t.Setenv(config.HomeEnvVar, relocated)

	got, err := config.Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != relocated {
		t.Errorf("Home() = %q, want the %s value %q", got, config.HomeEnvVar, relocated)
	}
}

// TestHomeFallsBackToTheUserHomeDir checks the documented default: no override
// means the user's own home directory, not the working directory.
func TestHomeFallsBackToTheUserHomeDir(t *testing.T) {
	t.Setenv(config.HomeEnvVar, "")

	got, err := config.Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if filepath.Base(got) != config.HomeDirName {
		t.Errorf("Home() = %q, want it to end in %q", got, config.HomeDirName)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Home() = %q, want an absolute path", got)
	}
}

// TestHomeRefusesARelativeOverride pins the deliberate failure: a relocated home
// that depended on the working directory would point somewhere different in every
// session, so it is refused at startup instead.
func TestHomeRefusesARelativeOverride(t *testing.T) {
	t.Setenv(config.HomeEnvVar, "relative/place")

	if got, err := config.Home(); err == nil {
		t.Errorf("Home() = %q with no error, want a refusal", got)
	}
}

// TestProjectConfigPath checks that the project file is named by the constant and
// sits beside the directory it configures.
func TestProjectConfigPath(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("/work", "project")
	want := filepath.Join(dir, config.ProjectConfigFile)
	if got := config.ProjectConfigPath(dir); got != want {
		t.Errorf("ProjectConfigPath(%q) = %q, want %q", dir, got, want)
	}
}
