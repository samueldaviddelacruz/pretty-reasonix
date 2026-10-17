package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Config is what a factory is given to build one configured provider.
//
// It holds no credential value: APIKey is a resolver, because a string field would be
// printed by the first %v somebody adds to a log line, and would be captured when the
// provider is built — so a rotated key would wait for a restart. The closure the assembly
// supplies reads through config.Secret, so Reveal stays the only way out.
type Config struct {
	// Name is the provider entry's name from configuration, which becomes Provider.Name().
	Name string
	// BaseURL is the root of the vendor's API. A kind appends its own path, because the path
	// is the one part of the URL a vendor owns.
	BaseURL string
	// Model is the model this provider is configured for.
	Model string
	// IdleTimeout bounds how long a request may go without an answer, which is a provider entry's
	// idle_timeout_seconds. Zero means the transport's default.
	IdleTimeout time.Duration
	// PromptCache says the vendor accepts ephemeral cache-control breakpoints. Sending them to a
	// vendor that does not implement the field is a rejected request rather than a no-op, so this
	// is a capability fact and not a preference.
	PromptCache bool
	// APIKey resolves the credential. It may be nil, and it may return an empty
	// string; a kind reports either as a configuration error rather than sending a
	// request the vendor will reject, because "you have not configured a key" is a
	// better message than a 401, and it is one we can give without a round trip.
	APIKey func() string
}

// Factory builds one configured provider. It returns an error rather than panicking for
// anything it cannot reconcile, because its input is a configuration file.
type Factory func(Config) (Provider, error)

var (
	// ErrDuplicateKind reports a kind that is already registered.
	ErrDuplicateKind = errors.New("provider kind already registered")
	// ErrUnknownKind reports a kind nothing registered.
	ErrUnknownKind = errors.New("unknown provider kind")
	// ErrNoFactory reports a factory that produced nothing and no error — a mistake in the kind.
	ErrNoFactory = errors.New("factory produced no provider")
)

// kindRegistry maps a kind to the factory that builds it. The mutex is correctness: Register
// runs at boot while New and Kinds run whenever a session is assembled, and a concurrent map
// read during a write is a panic.
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

// Register teaches the program how to build one kind of provider. Only internal/assembly calls
// it, which a test enforces: a kind registering itself from an init() would make the available
// providers depend on which packages a build linked.
//
// A duplicate kind, a nameless kind or a missing factory is an error, not a panic.
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
