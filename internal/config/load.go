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

// Load resolves the configuration in the order §6.11 fixes:
//
//	flags > ./prx.toml > <home>/config.toml > built-in defaults
//
// The files decode into one configuration bottom-up, so a key a later file omits keeps
// what an earlier one decided; overlaying whole structs would put the higher layer's
// defaults over the lower layer's real settings. A missing file is ordinary, an
// unparseable one is reported with its path, and anything a project may not decide is
// put back before returning and reported in Result.Ignored.
func Load(projectPath, userPath string, flags Flags) (*Result, error) {
	cfg := Default()
	result := &Result{Config: cfg}

	// Lowest priority first. The project layer is the only one held back; keying that on
	// position rather than on comparing paths avoids misfiring when both are the same.
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
		// Captured before the decode and put back after it.
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

// UserGlobalKeys are the settings only the user's own configuration may decide: a cloned
// repository must not change how someone's editor behaves by committing a file.
//
// A project naming one is not an error — the value is put back and reported in
// Result.Ignored rather than refusing to start.
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
