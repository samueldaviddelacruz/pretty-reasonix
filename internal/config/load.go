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

// Result is everything resolving the configuration produced.
type Result struct {
	// Config is the effective configuration: the layers collapsed, with every value
	// a lower layer may not have already put back.
	Config *Config
	// Warnings are keys a file used that this version does not act on. They are
	// compatibility notices — the file is newer than the program, or a key was
	// renamed — and a run continues with them.
	Warnings []Warning
	// Ignored is what a project file asked for and may not have. It is kept apart
	// from Warnings because these are security notices: `prx doctor` should show
	// them prominently, and a stricter caller may refuse to run when a project
	// asked for more than it is entitled to.
	Ignored IgnoredProjectSettings
}

// Load resolves the configuration from the four sources, in the order fixed by
// docs/TECHNICAL_DESIGN.md §6.11:
//
//	flags > ./prx.toml > <home>/config.toml > built-in defaults
//
// The two files are decoded into one configuration from the bottom up — defaults
// first, then the user file, then the project file — so a key a later file does not
// mention keeps what an earlier one decided. Overlaying whole structs instead would
// reinstate the higher layer's *defaults* over the lower layer's real settings,
// which is the bug this shape exists to avoid.
//
// What a project may not decide is captured before its file is decoded and put back
// after it, so a refused value never reaches the caller at all: see
// holdBackUserGlobals and holdBackWidenings, both reported in Result.Ignored.
//
// A missing file is ordinary: a first run has no user configuration, and a project
// without one is the common case. A file that cannot be parsed is not, and is
// reported with the path that failed rather than silently skipped.
func Load(projectPath, userPath string, flags Flags) (*Result, error) {
	cfg := Default()
	result := &Result{Config: cfg}

	// Lowest priority first, so each file overwrites only what it names. The project
	// file is the higher layer and the only one whose settings are held back —
	// keyed by position rather than by comparing paths, which would misfire for a
	// caller that passed the same path twice.
	layers := []struct {
		path    string
		project bool
	}{
		{userPath, false},
		{projectPath, true},
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
			return nil, fmt.Errorf("reading configuration %s: %w", layer.path, err)
		}
		// The settings a project may not decide are captured before the decode and
		// put back after it.
		before := *cfg
		meta, found, err := apply(cfg, body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", layer.path, err)
		}
		if layer.project {
			result.Ignored = append(result.Ignored,
				holdBackWidenings(cfg, &before, meta, layer.path)...)
			result.Ignored = append(result.Ignored,
				holdBackUserGlobals(cfg, &before, meta, layer.path)...)
		}
		for i := range found {
			found[i].Source = layer.path
		}
		result.Warnings = append(result.Warnings, found...)
	}

	flags.applyTo(cfg)
	return result, nil
}

// UserGlobalKeys are the settings only the user's own configuration may decide.
//
// docs/TECHNICAL_DESIGN.md §6.11 marks these "user-global only", and the reason is
// a security one rather than a tidy one: a cloned repository must not be able to
// change how someone's editor behaves by committing a file.
//
// A project that names one is not an error. Refusing to start over a key the project
// cannot control would be a worse outcome for the person running it, so the value is
// held back and reported instead — see holdBackUserGlobals, and restoreKey for where
// each key is put back.
//
// E2-T4 recorded that the credential-path settings §6.11 also marks global-only
// would join this list. They have not, because they turned out not to exist as
// settings: E2-T5 found that what a project must not supply is a credential
// *variable*, and that is policed by name in secrets.go rather than by a
// configuration key here.
var UserGlobalKeys = []string{"ui.commandmode"}

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
