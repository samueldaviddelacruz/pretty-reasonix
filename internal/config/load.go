package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Flags are the settings a command line can override, and nothing more.
//
// The pointer fields are deliberate: a flag that was not given must not
// overwrite what a file said, and a value type cannot distinguish "unset" from
// "set to the zero value" — rule [#11]'s zero-value trap, in the one place it
// really bites. The flags a session actually parses are E14's and E19's; this is
// only the overlay they need.
type Flags struct {
	// Model is --model: a provider name, or an explicit provider/model.
	Model *string
	// PermissionMode is --permission-mode.
	PermissionMode *string
	// Allow is --allowed-tools: session-only allow rules.
	//
	// It appends rather than replaces. A command line should not be able to
	// silently discard the deny rules already in play, and appending makes the
	// grant what it claims to be: an addition.
	Allow []string
}

// applyTo overlays the flags that were given, and only those.
func (f Flags) applyTo(cfg *Config) {
	if f.Model != nil {
		cfg.DefaultModel = *f.Model
	}
	if f.PermissionMode != nil {
		cfg.Permissions.Mode = *f.PermissionMode
	}
	cfg.Permissions.Allow = append(cfg.Permissions.Allow, f.Allow...)
}

// Load resolves the configuration from the four sources, in the order fixed by
// docs/TECHNICAL_DESIGN.md §6.11:
//
//	flags > ./prx.toml > <home>/config.toml > built-in defaults
//
// The two files are decoded into one configuration from the bottom up — defaults
// first, then the user file, then the project file — so a key a later file does
// not mention keeps what an earlier one decided. Overlaying whole structs
// instead would reinstate the higher layer's *defaults* over the lower layer's
// real settings, which is the bug this shape exists to avoid.
//
// A missing file is ordinary: a first run has no user configuration, and a
// project without one is the common case. A file that cannot be parsed is not,
// and is reported with the path that failed rather than silently skipped.
func Load(projectPath, userPath string, flags Flags) (*Config, []Warning, error) {
	cfg := Default()
	var warnings []Warning

	// Lowest priority first, so each file overwrites only what it names. The
	// project file is the higher layer, and the only one whose user-global
	// settings are held back — keyed by position rather than by comparing paths,
	// which would misfire for a caller that passed the same path twice.
	layers := []struct {
		path         string
		maySetGlobal bool
	}{
		{userPath, true},
		{projectPath, false},
	}
	for _, layer := range layers {
		if layer.path == "" {
			continue
		}
		body, err := os.ReadFile(layer.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading configuration %s: %w", layer.path, err)
		}
		// The settings a project may not decide are captured before the decode
		// and restored after it, so the value never reaches the caller at all.
		before := *cfg
		meta, found, err := apply(cfg, body)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", layer.path, err)
		}
		if !layer.maySetGlobal {
			found = append(found, holdBackUserGlobals(cfg, &before, meta)...)
		}
		for i := range found {
			found[i].Source = layer.path
		}
		warnings = append(warnings, found...)
	}

	flags.applyTo(cfg)
	return cfg, warnings, nil
}

// UserGlobalKeys are the settings only the user's own configuration may decide.
//
// docs/TECHNICAL_DESIGN.md §6.11 marks these "user-global only", and the reason
// is a security one rather than a tidy one: a cloned repository must not be able
// to change how someone's editor behaves, or where their credentials live, by
// committing a file.
//
// A project that names one is not an error. Refusing to start over a key the
// project cannot control would be a worse outcome for the person running it, so
// the value is held back and reported instead — see holdBackUserGlobals.
//
// The credential-path settings §6.11 also marks global-only arrive with E2-T5 and
// join this list then; there is nothing to police yet.
var UserGlobalKeys = []string{"ui.commandmode"}

// holdBackUserGlobals restores every key the project named that it may not
// decide, and reports each one.
func holdBackUserGlobals(cfg, before *Config, meta toml.MetaData) []Warning {
	var held []Warning
	for _, key := range UserGlobalKeys {
		if !meta.IsDefined(strings.Split(key, ".")...) {
			continue
		}
		restoreUserGlobal(cfg, before, key)
		held = append(held, Warning{
			Key:  key,
			Note: "only the user's configuration may set this; the project's value was ignored",
		})
	}
	return held
}

// restoreUserGlobal puts back the value a key had before the project file was
// read.
//
// It is an if rather than a switch while there is one key to restore, and
// becomes a switch when E2-T5 adds the credential-path keys. What matters is that
// the rule is enforced by the assertion in userglobal_test.go — "the project
// changed nothing" — rather than by the warning appearing, so a key with no
// branch here fails the test instead of passing quietly.
func restoreUserGlobal(cfg, before *Config, key string) {
	if key == "ui.commandmode" {
		cfg.UI.CommandMode = before.UI.CommandMode
	}
}

// Sources names the layers in the resolution order, lowest priority first, so
// that a caller which needs to know *which* source decided something — the
// narrowing check in E2-T6, or `prx doctor` — does not have to reconstruct it.
func Sources(projectPath, userPath string) []Source {
	sources := make([]Source, 0, 3)
	if userPath != "" {
		sources = append(sources, Source{Name: "user", Path: userPath})
	}
	if projectPath != "" {
		sources = append(sources, Source{Name: "project", Path: projectPath})
	}
	return append(sources, Source{Name: "flags"})
}

// Source is one place a setting can come from.
type Source struct {
	// Name is "user", "project" or "flags".
	Name string
	// Path is the file, empty for flags.
	Path string
}
