package assembly

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	Warnings   []config.Warning
	Ignored    config.IgnoredProjectSettings
}

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

	confined, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, fmt.Errorf("confining tools to %s: %w", workspace, err)
	}
	registry := tool.NewRegistry()
	if toolErr := RegisterTools(registry, confined); toolErr != nil {
		return nil, toolErr
	}

	transcript := &session.Transcript{}
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
