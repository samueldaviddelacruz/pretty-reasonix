package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Config is what a factory is given to build one configured provider.
//
// It carries no credential value, and that is the point of its shape. APIKey is a
// resolver rather than a string, because a string field here would be printed by the
// first %v somebody adds to a log line or a diagnostic, and it would also be captured
// when the provider is built — so a key the user rotates would not take effect until
// the next restart. Resolving at request time costs one call and removes both
// problems, and the function the assembly supplies closes over the same Secret type
// that redacts everywhere else (internal/config), so the value's only way out remains
// an explicit Reveal.
type Config struct {
	// Name is the provider entry's name from configuration, which becomes
	// Provider.Name().
	Name string
	// BaseURL is the root of the vendor's API. A kind appends its own path, because
	// the path is the one part of the URL a vendor owns.
	BaseURL string
	// Model is the model this provider is configured for.
	Model string
	// APIKey resolves the credential. It may be nil, and it may return an empty
	// string; a kind reports either as a configuration error rather than sending a
	// request the vendor will reject, because "you have not configured a key" is a
	// better message than a 401, and it is one we can give without a round trip.
	APIKey func() string
}

// Factory builds one configured provider.
//
// It returns an error rather than panicking for anything it cannot reconcile — a base
// URL it cannot parse, a missing credential — because its input comes from a
// configuration file, and user input is not an exceptional condition.
type Factory func(Config) (Provider, error)

var (
	// ErrDuplicateKind reports a kind that is already registered.
	ErrDuplicateKind = errors.New("provider kind already registered")
	// ErrUnknownKind reports a kind nothing registered.
	ErrUnknownKind = errors.New("unknown provider kind")
	// ErrNoFactory reports a factory that produced nothing and no error, which is a
	// programming mistake in the kind rather than in the caller's configuration.
	ErrNoFactory = errors.New("factory produced no provider")
)

// kindRegistry maps a kind to the factory that builds it.
//
// A mutex rather than a bare map because Register happens at boot while New and Kinds
// are called from wherever a session is assembled, and a concurrent map read during a
// write is a panic rather than a wrong answer.
type kindRegistry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// kinds is the process-wide registry.
//
// Registration being global is the design (TDD §6.3) and it is what makes the set of
// available kinds independent of which packages a build happens to link. The cost is
// that a test shares this map with every other test in its binary: a test registers
// under a name of its own and asserts membership, never that Kinds() is exactly some
// list.
var kinds = &kindRegistry{factories: map[string]Factory{}}

// Register teaches the program how to build one kind of provider.
//
// It is called from internal/assembly and nowhere else (rule [#3], enforced by a test
// in internal/architecture): a kind that registered itself from an init() would make
// the set of available providers depend on which packages happened to be imported,
// which is the invisible coupling the explicit call site exists to avoid.
//
// A duplicate kind is an error rather than a panic (rule [#48]). The distinction earns
// its keep at boot: two entries claiming one kind is a configuration mistake to report
// and fix, and a panic in its place turns a message into a crash report. The same goes
// for a nameless kind or a missing factory — the mistake is made here, so it is
// reported here rather than as a nil dereference later.
func Register(kind string, f Factory) error {
	if kind == "" {
		return errors.New("a provider kind needs a name")
	}
	if f == nil {
		return fmt.Errorf("registering provider kind %q with no factory", kind)
	}
	kinds.mu.Lock()
	defer kinds.mu.Unlock()
	if _, taken := kinds.factories[kind]; taken {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, kind)
	}
	kinds.factories[kind] = f
	return nil
}

// New builds the provider for a kind.
//
// An unknown kind is reported with the kinds that do exist, because the mistake is
// almost always a spelling in a configuration file and the list is the fix. The kind is
// matched exactly rather than case-folded for the same reason: folding would accept
// "OpenAI" and hide a typo that the list of known kinds would have shown.
func New(kind string, cfg Config) (Provider, error) {
	kinds.mu.RLock()
	factory, known := kinds.factories[kind]
	kinds.mu.RUnlock()

	if !known {
		available := Kinds()
		if len(available) == 0 {
			return nil, fmt.Errorf("%w %q: nothing has registered a provider kind", ErrUnknownKind, kind)
		}
		return nil, fmt.Errorf("%w %q: known kinds are %s", ErrUnknownKind, kind, strings.Join(available, ", "))
	}

	built, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("building provider kind %q: %w", kind, err)
	}
	if built == nil {
		return nil, fmt.Errorf("%w for kind %q", ErrNoFactory, kind)
	}
	return built, nil
}

// Kinds returns the registered kinds, sorted, as a copy.
//
// Sorted because the answer reaches a user in an error message and a test in an
// assertion, and neither should depend on map iteration order. A copy because a caller
// holding the registry's own slice could rearrange it, and the registry is shared with
// every other caller in the process.
func Kinds() []string {
	kinds.mu.RLock()
	defer kinds.mu.RUnlock()
	available := make([]string, 0, len(kinds.factories))
	for kind := range kinds.factories {
		available = append(available, kind)
	}
	slices.Sort(available)
	return available
}
