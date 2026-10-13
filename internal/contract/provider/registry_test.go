package provider_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
)

// stubProvider speaks no protocol, so that a test of the registry does not have to
// pretend to. It records the Config it was built from, which is how a test checks the
// configuration arrived intact.
type stubProvider struct {
	name   string
	config provider.Config
}

func (s *stubProvider) Name() string { return s.name }

func (*stubProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("a registry test never streams")
}

// stubFactory builds a stubProvider. A nil sink is allowed for the tests that only
// care about the registration.
func stubFactory(sink **stubProvider) provider.Factory {
	return func(cfg provider.Config) (provider.Provider, error) {
		built := &stubProvider{name: cfg.Name, config: cfg}
		if sink != nil {
			*sink = built
		}
		return built, nil
	}
}

// Every test here registers under a kind of its own and asserts membership rather than
// the exact contents of Kinds(): the registry is process-global by design (TDD §6.3),
// so it is shared with every other test in this binary.

func TestRegisterThenNewBuildsTheConfiguredProvider(t *testing.T) {
	t.Parallel()
	var built *stubProvider
	if err := provider.Register("regtest/basic", stubFactory(&built)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := provider.New("regtest/basic", provider.Config{
		Name:    "vendor",
		BaseURL: "https://api.example.com",
		Model:   "vendor/model",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got.Name() != "vendor" {
		t.Errorf("Name() = %q, want the configured name", got.Name())
	}
	if built == nil {
		t.Fatal("the factory was not called")
	}
	if built.config.BaseURL != "https://api.example.com" || built.config.Model != "vendor/model" {
		t.Errorf("the factory received %+v, want the configuration unchanged", built.config)
	}
	if fromNew, ok := got.(*stubProvider); !ok || fromNew != built {
		t.Error("New returned something other than what the factory built")
	}
}

// TestADuplicateKindIsAnErrorAndNotAPanic is rule [#48]. Reaching the assertions at all
// is half the point: a panic would take the process with it instead of reporting a
// configuration mistake.
func TestADuplicateKindIsAnErrorAndNotAPanic(t *testing.T) {
	t.Parallel()
	const kind = "regtest/duplicate"
	if err := provider.Register(kind, stubFactory(nil)); err != nil {
		t.Fatalf("the first registration failed: %v", err)
	}

	err := provider.Register(kind, stubFactory(nil))
	if !errors.Is(err, provider.ErrDuplicateKind) {
		t.Errorf("the second registration returned %v, want ErrDuplicateKind", err)
	}
}

// TestRegisterRefusesWhatItCannotUse keeps the failure where the mistake is made: a
// nameless kind or a missing factory would otherwise become a nil dereference at New
// time, far from the call that caused it.
func TestRegisterRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		kind    string
		factory provider.Factory
	}{
		"a nameless kind": {kind: "", factory: stubFactory(nil)},
		"no factory":      {kind: "regtest/nil", factory: nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := provider.Register(tc.kind, tc.factory); err == nil {
				t.Errorf("Register(%q) was accepted", tc.kind)
			}
		})
	}
}

// TestAnUnknownKindNamesTheKnownOnes: the mistake is almost always a spelling in a
// configuration file, so the error carries the fix. The name asked for is deliberately
// one that was never registered — spelled correctly, because misspell is right to object
// to a deliberate typo and this test does not need one to make its point.
func TestAnUnknownKindNamesTheKnownOnes(t *testing.T) {
	t.Parallel()
	if err := provider.Register("regtest/visible", stubFactory(nil)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := provider.New("regtest/unregistered", provider.Config{})
	if !errors.Is(err, provider.ErrUnknownKind) {
		t.Fatalf("New returned %v, want ErrUnknownKind", err)
	}
	if !strings.Contains(err.Error(), "regtest/visible") {
		t.Errorf("the error does not list the known kinds, so it does not help fix the typo: %v", err)
	}
}

func TestKindsAreSortedAndHandedOutAsACopy(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"regtest/zeta", "regtest/alpha"} {
		if err := provider.Register(kind, stubFactory(nil)); err != nil {
			t.Fatalf("Register(%q): %v", kind, err)
		}
	}

	first := provider.Kinds()
	if !slices.IsSorted(first) {
		t.Errorf("Kinds() is not sorted: %v", first)
	}
	for _, kind := range []string{"regtest/zeta", "regtest/alpha"} {
		if !slices.Contains(first, kind) {
			t.Errorf("Kinds() does not include %q: %v", kind, first)
		}
	}

	// The direction that would be caught late: the slice is handed to a diagnostic that
	// sorts or truncates it, and the registry changes underneath every other caller.
	first[0] = "clobbered"
	if second := provider.Kinds(); slices.Contains(second, "clobbered") {
		t.Error("Kinds() handed out the registry's own slice")
	}
}

func TestAFactoryThatReturnsNothingIsAnError(t *testing.T) {
	t.Parallel()
	if err := provider.Register("regtest/empty", func(provider.Config) (provider.Provider, error) {
		return nil, nil //nolint:nilnil // the test is about what happens when a factory does exactly this
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := provider.New("regtest/empty", provider.Config{})
	if !errors.Is(err, provider.ErrNoFactory) {
		t.Errorf("New returned %v, want ErrNoFactory rather than a provider that would panic on use", err)
	}
}

func TestAFactoryFailureIsWrappedAndMatchable(t *testing.T) {
	t.Parallel()
	refused := errors.New("the configured URL is not a URL")
	if err := provider.Register("regtest/broken", func(provider.Config) (provider.Provider, error) {
		return nil, refused
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := provider.New("regtest/broken", provider.Config{Name: "vendor"})
	if err == nil {
		t.Fatal("a factory that failed produced a provider and no error")
	}
	if !errors.Is(err, refused) {
		t.Errorf("New lost the factory's own error, so a caller cannot match it: %v", err)
	}
	if !strings.Contains(err.Error(), "regtest/broken") {
		t.Errorf("the error does not name the kind: %v", err)
	}
}

// TestAConfigNeverPrintsACredential is the property APIKey's shape exists for. A string
// field holding a key would appear in the first %v that a log line or a diagnostic
// adds, and a factory is handed a resolver precisely so that cannot happen by accident.
func TestAConfigNeverPrintsACredential(t *testing.T) {
	t.Parallel()
	const credential = "sk-live-must-not-appear"
	cfg := provider.Config{
		Name:    "vendor",
		BaseURL: "https://api.example.com",
		Model:   "vendor/model",
		APIKey:  func() string { return credential },
	}

	// The value is still reachable, which is what makes this a door rather than a wall.
	if got := cfg.APIKey(); got != credential {
		t.Fatalf("APIKey() = %q, want the credential", got)
	}
	for _, verb := range []string{"%v", "%+v", "%s", "%q"} {
		if printed := fmt.Sprintf(verb, cfg); strings.Contains(printed, credential) {
			t.Errorf("%s printed the credential: %s", verb, printed)
		}
	}
}

// TestTheCredentialResolverIsReadWhenItIsUsed is the second reason APIKey is a resolver:
// a key the user rotates takes effect on the next read rather than at the next restart,
// because the provider keeps the function rather than the value it returned.
func TestTheCredentialResolverIsReadWhenItIsUsed(t *testing.T) {
	t.Parallel()
	current := "first-key"
	var built *stubProvider
	if err := provider.Register("regtest/live", stubFactory(&built)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := provider.New("regtest/live", provider.Config{
		Name:   "vendor",
		APIKey: func() string { return current },
	}); err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := built.config.APIKey(); got != "first-key" {
		t.Errorf("the resolver returned %q, want the key as it was at construction", got)
	}
	current = "second-key"
	if got := built.config.APIKey(); got != "second-key" {
		t.Errorf("the resolver returned %q after a rotation, want the new key", got)
	}
}
