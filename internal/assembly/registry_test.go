package assembly_test

import (
	"slices"
	"testing"

	"pretty-reasonix/internal/assembly"
	"pretty-reasonix/internal/contract/provider"
)

// TestEveryCompiledKindBecomesAvailable is the link that was missing: the kinds exist, the registry works,
// and nothing joined them — so a hand-run probe failed with "unknown provider kind" while every unit test
// passed. This asserts the join.
func TestEveryCompiledKindBecomesAvailable(t *testing.T) {
	t.Parallel()
	if err := assembly.RegisterProviders(); err != nil {
		t.Fatalf("RegisterProviders: %v", err)
	}
	available := provider.Kinds()
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		if !slices.Contains(available, kind) {
			t.Errorf("kind %q is compiled in but not registered; available: %v", kind, available)
		}
	}
}

// TestRegisteringTwiceIsANoOp: two callers — a probe and a frontend — both need the registry populated, and
// neither should have to know whether the other ran first.
func TestRegisteringTwiceIsANoOp(t *testing.T) {
	t.Parallel()
	if err := assembly.RegisterProviders(); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := assembly.RegisterProviders(); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := len(provider.Kinds()); got < 3 {
		t.Errorf("%d kinds registered, want at least 3", got)
	}
}

// TestARegisteredKindBuildsTheProviderItNames: registering a factory that cannot be reached by New would be
// registration in name only.
func TestARegisteredKindBuildsTheProviderItNames(t *testing.T) {
	t.Parallel()
	if err := assembly.RegisterProviders(); err != nil {
		t.Fatalf("RegisterProviders: %v", err)
	}
	built, err := provider.New("openai", provider.Config{
		Name:    "probe",
		BaseURL: "http://127.0.0.1:1/v1",
		Model:   "some-model",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if built.Name() != "probe" {
		t.Errorf("the built provider is named %q", built.Name())
	}
}
