package assembly

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
	"pretty-reasonix/internal/session"
	"pretty-reasonix/internal/tool"
)

var (
	// ErrNoModel reports a configuration that names no model a provider offers.
	ErrNoModel = errors.New("no model resolved from the configuration")
	// ErrNoKey reports a provider whose key is named but absent from the environment file.
	ErrNoKey = errors.New("the provider's key is not in the environment file")
)

// Options is what a caller can decide before a run is wired.
type Options struct {
	// Root is the workspace tools are confined to. Empty means the process working directory.
	Root string
	// Model is a reference from a flag, or empty to take the configured default.
	Model string
	// Effort is a reasoning level from a flag, or empty to take the model's default.
	Effort string
	// Preset is balanced or delivery; empty means balanced.
	Preset string
	// Sink receives the turn's events. Required: a run with nowhere to report is a run with no frontend.
	Sink event.Sink
	// Continue resumes the most recently written session, or starts a fresh one when there is nothing to resume.
	Continue bool
	// Resume names a session by id, by path, or by a substring of its title or preview.
	Resume string
	// Copy starts from a copy of the resumed session, leaving the original untouched.
	Copy bool
	// Now is the clock a new session is named from. Required rather than defaulted: a function that reads the clock
	// cannot be tested, and the caller is the only place that knows what time it is.
	Now time.Time
}

// Run is everything one run needs, wired.
//
// It is a value rather than a set of globals because the TUI and the headless runner must drive the same
// engine: two frontends that assembled their own would drift, and the first symptom would be one of them
// seeing tools the other does not.
type Run struct {
	Controller *agent.Controller
	Transcript *session.Transcript
	Registry   *tool.Registry
	Paths      config.Paths
	Model      model.Model
	Layout     session.Layout
	// Recovered reports what a resume had to set aside, so the frontend can say so.
	Recovered session.Recovered
	// Resumed is true when the run continued an existing session rather than starting one.
	Resumed  bool
	Warnings []config.Warning
	Ignored  config.IgnoredProjectSettings
}

// Close finishes the session, reporting a failure to persist rather than swallowing it.
//
// The answer the turn produced has already been shown, so this reports history that was lost rather than turning a
// finished run into a failed one — but it does report it, because a transcript that quietly stopped writing looks
// exactly like one that worked.
func (r *Run) Close() error { return r.Transcript.Close() }

// Build wires a run: configuration, then registries, then the provider and tools, then the controller.
//
// The order is the point of this package (§6.2): a kind becomes reachable because this function decided it
// should be, not because a package imported another and registered itself somewhere unread.
func Build(options Options) (*Run, error) {
	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	paths := config.Resolve(home)
	workspace := options.Root
	if workspace == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return nil, fmt.Errorf("finding the workspace: %w", cwdErr)
		}
		workspace = cwd
	}
	loaded, err := config.Load(filepath.Join(workspace, config.ProjectConfigFile), paths.ConfigFile, config.Flags{})
	if err != nil {
		return nil, err
	}

	reference := options.Model
	if reference == "" {
		reference = loaded.Config.DefaultModel
	}
	resolved, err := model.Resolve(loaded.Config, reference)
	if err != nil {
		return nil, err
	}
	effort, err := resolveEffort(loaded.Config, resolved, options.Effort)
	if err != nil {
		return nil, err
	}
	preset, err := agent.ParsePreset(options.Preset)
	if err != nil {
		return nil, err
	}

	if regErr := RegisterProviders(); regErr != nil {
		return nil, regErr
	}
	built, err := newProvider(paths, loaded.Config, resolved)
	if err != nil {
		return nil, err
	}

	// One session per run, created here so that persistence is a wiring decision rather than something the loop knows
	// about. Resuming is E9-T5's flags; this ticket only has to make a run outlive itself.
	if options.Now.IsZero() {
		return nil, errors.New("a run needs the current time, so a session can be named after it")
	}
	id, err := session.NewID(options.Now)
	if err != nil {
		return nil, err
	}
	layout, resumed, recovered, err := openSession(paths, options, id)
	if err != nil {
		return nil, err
	}

	// Only the output directory exists this early: spilling needs somewhere to go, and a run that fails later must not
	// leave an event log behind, because Newest orders by that file and --continue would resume a session that never
	// said anything.
	if mkErr := os.MkdirAll(layout.Outputs, session.ModeDir); mkErr != nil {
		return nil, fmt.Errorf("creating the output directory: %w", mkErr)
	}

	confined, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, fmt.Errorf("confining tools to %s: %w", workspace, err)
	}
	registry := tool.NewRegistry()
	if toolErr := RegisterTools(registry, confined); toolErr != nil {
		return nil, toolErr
	}

	// Opened here, last: everything that can fail has failed by now.
	transcript, openErr := openTranscript(layout, resumed)
	if openErr != nil {
		return nil, openErr
	}

	controller, err := agent.NewController(agent.Deps{
		Provider:   built,
		Transcript: transcript,
		Sink:       options.Sink,
		Policy: agent.Policy{
			Model:       resolved,
			Effort:      effort,
			Preset:      preset,
			Permissions: loaded.Config.Permissions,
			Sandbox:     loaded.Config.Sandbox,
		},
		Models:  []model.Model{resolved},
		Tools:   registry.Names(),
		Schemas: registry.Schemas(),
		// Handed over here rather than defaulted in the controller: a turn without a registry can only
		// talk, and that should be a decision, not an accident.
		Registry: registry,
		// The session's own outputs directory, now that there is one: the placeholder existed because a run had nowhere
		// to spill to, and spilling beside every other session is not where spilled output belongs.
		Boundary: tool.Boundary{Dir: layout.Outputs},
	})
	if err != nil {
		return nil, err
	}

	return &Run{
		Controller: controller,
		Transcript: transcript,
		Registry:   registry,
		Paths:      paths,
		Model:      resolved,
		Layout:     layout,
		Recovered:  recovered,
		Resumed:    resumed,
		Warnings:   loaded.Warnings,
		Ignored:    loaded.Ignored,
	}, nil
}

// newProvider builds the provider a resolved model names.
func newProvider(paths config.Paths, cfg *config.Config, resolved model.Model) (provider.Provider, error) {
	var entry config.Provider
	for _, candidate := range cfg.Providers {
		if candidate.Name == resolved.Provider {
			entry = candidate
			break
		}
	}
	if entry.Name == "" {
		return nil, fmt.Errorf("provider %q is not configured", resolved.Provider)
	}

	credentials, _, err := config.LoadCredentials(paths.EnvFile)
	if err != nil {
		return nil, err
	}
	secret, found := credentials[entry.APIKeyEnv]
	if entry.APIKeyEnv != "" && (!found || !secret.IsSet()) {
		// Named but absent is a configuration mistake worth its own message: the alternative is a 401 that
		// reads like a bad key.
		return nil, fmt.Errorf("%w: %s", ErrNoKey, entry.APIKeyEnv)
	}

	return provider.New(entry.Kind, provider.Config{
		Name:       entry.Name,
		BaseURL:    entry.BaseURL,
		RequestURL: entry.RequestURL,
		Model:      resolved.Name,
		APIKey:     secret.Reveal,
	})
}

// resolveEffort applies a flag, then the model's own default, then nothing.
func resolveEffort(cfg *config.Config, resolved model.Model, flag string) (provider.Effort, error) {
	requested, err := provider.ParseEffort(flag)
	if err != nil {
		return provider.EffortUnset, err
	}
	for _, entry := range cfg.Providers {
		if entry.Name != resolved.Provider {
			continue
		}
		override := entry.ModelOverrides[resolved.Name]
		return model.EffortFor(requested, override.SupportedEfforts, override.DefaultEffort)
	}
	return provider.EffortUnset, nil
}

// openSession decides which session this run belongs to.
//
// Three ways in and one way out. A resume names a session, a continue takes the newest, and anything else starts a
// fresh one — and a continue with nothing to continue is deliberately *not* an error: a first run is the common case,
// and refusing it would make `--continue` unusable in a script that also runs for the first time. The caller reports
// that it started fresh, which is the notice the criterion asks for.
func openSession(paths config.Paths, options Options, id string) (session.Layout, bool, session.Recovered, error) {
	fresh := session.NewLayout(paths.SessionsDir, id)

	switch {
	case options.Resume != "":
		resolved, err := session.Resolve(paths.SessionsDir, options.Resume, options.Now)
		if err != nil {
			return session.Layout{}, false, session.Recovered{}, err
		}
		if options.Copy {
			copied, copyErr := session.Copy(resolved, id, options.Now)
			return copied, false, session.Recovered{}, copyErr
		}
		recovered, err := session.Recover(resolved)
		return resolved, true, recovered, err

	case options.Continue:
		newest, found, err := session.Newest(paths.SessionsDir)
		if err != nil {
			return session.Layout{}, false, session.Recovered{}, err
		}
		if !found {
			return fresh, false, session.Recovered{}, nil
		}
		if options.Copy {
			copied, copyErr := session.Copy(newest, id, options.Now)
			return copied, false, session.Recovered{}, copyErr
		}
		recovered, err := session.Recover(newest)
		return newest, true, recovered, err

	default:
		if options.Copy {
			// A copy of nothing is nothing: reported rather than silently starting fresh, because the person asked for
			// something specific and got something else.
			return session.Layout{}, false, session.Recovered{}, errors.New("--copy needs --resume or --continue")
		}
		return fresh, false, session.Recovered{}, nil
	}
}

// openTranscript opens a session for appending, existing or new.
func openTranscript(layout session.Layout, resumed bool) (*session.Transcript, error) {
	if resumed {
		transcript, _, err := session.LoadPersistent(layout)
		return transcript, err
	}
	return session.NewPersistent(layout)
}
