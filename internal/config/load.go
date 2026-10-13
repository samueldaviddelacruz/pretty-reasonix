package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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

	// Lowest priority first, so each file overwrites only what it names.
	for _, source := range []string{userPath, projectPath} {
		if source == "" {
			continue
		}
		body, err := os.ReadFile(source) //nolint:gosec // G304: the path is the layer this loader was told to read
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading configuration %s: %w", source, err)
		}
		found, err := apply(cfg, body)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", source, err)
		}
		for i := range found {
			found[i].Source = source
		}
		warnings = append(warnings, found...)
	}

	flags.applyTo(cfg)
	return cfg, warnings, nil
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
