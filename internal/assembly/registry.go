package assembly

import (
	"errors"
	"os"
	"sync"

	"pretty-reasonix/internal/contract/provider"
	contracttool "pretty-reasonix/internal/contract/tool"
	"pretty-reasonix/internal/provider/anthropic"
	"pretty-reasonix/internal/provider/openai"
	"pretty-reasonix/internal/provider/responses"
	"pretty-reasonix/internal/tool"
	"pretty-reasonix/internal/tool/builtin"
)

// ErrRegistrationFailed reports a kind that could not be registered, wrapping the reason.
var ErrRegistrationFailed = errors.New("registering a provider kind failed")

// registered guards the one-time registration, so a second call is a no-op rather than a duplicate-kind
// error. Two callers — a probe and a frontend, say — both need the registry populated and neither should
// have to know whether the other ran first.
var (
	registerOnce sync.Once
	errRegister  error
)

// RegisterProviders makes the compiled-in provider kinds available by name.
//
// This is the only place in the program that names the concrete kinds, and the only caller of
// provider.Register (rule [#3], TDD §6.2) — a test enforces the second half. The point is not tidiness: a
// kind becomes reachable because somebody decided it should be, in one file, rather than because a package
// was imported somewhere and registered itself in an init() nobody reads.
//
// The literals are the kinds' names rather than constants in their packages, because a constant there would
// be a second declaration of the same string and the name is this layer's to fix. If a kind needs to know
// its own name it can ask this package.
func RegisterProviders() error {
	registerOnce.Do(func() {
		kinds := []struct {
			name    string
			factory provider.Factory
		}{
			{"openai", openai.New},
			{"anthropic", anthropic.New},
			{"responses", responses.New},
		}
		for _, kind := range kinds {
			if err := provider.Register(kind.name, kind.factory); err != nil {
				errRegister = errors.Join(ErrRegistrationFailed, err)
				return
			}
		}
	})
	return errRegister
}

// RegisterTools adds the compiled-in read-only tools to a registry, confined to one workspace root.
//
// The root is the argument rather than a field because confinement is what makes a tool safe to hand a model:
// os.Root refuses a path that climbs out, so a tool cannot be talked into reading what is above the workspace
// however its arguments are written.
func RegisterTools(registry *tool.Registry, root *os.Root) error {
	tools := []contracttool.Tool{
		builtin.NewReadFile(root),
		builtin.NewLs(root),
		builtin.NewGlob(root),
		builtin.NewGrep(root),
	}
	for _, one := range tools {
		if err := registry.Add(one); err != nil {
			return errors.Join(ErrRegistrationFailed, err)
		}
	}
	return nil
}
