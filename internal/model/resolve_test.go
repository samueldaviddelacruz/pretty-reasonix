package model_test

import (
	"errors"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/model"
)

// configured is the two-provider configuration most of these cases share, so each test states only
// what it is about.
func configured() *config.Config {
	return &config.Config{
		DefaultModel: "first",
		Providers: []config.Provider{
			{
				Name:            "first",
				Kind:            "openai",
				Models:          []string{"shared-model", "only-first"},
				Default:         "only-first",
				ContextWindow:   100000,
				MaxOutputTokens: 8192,
				Prices: map[string]config.Price{
					"only-first": {Input: 1, Output: 2, CacheHit: 0.25, Currency: "USD"},
				},
				ModelOverrides: map[string]config.ModelOverride{
					"only-first": {ContextWindow: 50000, MaxOutputTokens: 4096},
				},
			},
			{
				Name:   "second",
				Kind:   "anthropic",
				Models: []string{"shared-model", "only-second"},
				// No Default: the first listed model is the default.
			},
		},
	}
}

// TestAReferenceResolvesInItsThreeForms is the criterion's first half.
func TestAReferenceResolvesInItsThreeForms(t *testing.T) {
	t.Parallel()
	t.Run("provider/model exactly", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "second/only-second")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Provider != "second" || got.Name != "only-second" {
			t.Errorf("resolved %+v", got)
		}
	})

	t.Run("a provider name means its default", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "first")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Name != "only-first" {
			t.Errorf("resolved %q, want the provider's declared default", got.Name)
		}
	})

	t.Run("a provider with no default means its first model", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "second")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Name != "shared-model" {
			t.Errorf("resolved %q, want the first listed model", got.Name)
		}
	})

	t.Run("a bare model id means the provider listing it", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "only-second")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.Provider != "second" {
			t.Errorf("resolved %+v", got)
		}
	})
}

// TestAmbiguityIsReportedRatherThanGuessed is the part of the criterion that keeps somebody from being
// charged for a choice they did not make.
func TestAmbiguityIsReportedRatherThanGuessed(t *testing.T) {
	t.Parallel()
	_, err := model.Resolve(configured(), "shared-model")
	if !errors.Is(err, model.ErrAmbiguousModel) {
		t.Fatalf("error is %v, want ErrAmbiguousModel", err)
	}
	for _, name := range []string{"first", "second"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not name the candidate %q: %v", name, err)
		}
	}
}

func TestAnUnknownReferenceSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a bare id nobody lists": "no-such-model",
		"a provider nobody has":  "nope/a-model",
		"an empty reference":     "",
		"nothing but spaces":     "   ",
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := model.Resolve(configured(), ref); !errors.Is(err, model.ErrModelNotFound) {
				t.Errorf("error is %v, want ErrModelNotFound", err)
			}
		})
	}
}

// TestTheWindowFollowsTheDocumentedChain is the criterion's second half: provider, then the per-model
// override, then what is known, then the documented default.
func TestTheWindowFollowsTheDocumentedChain(t *testing.T) {
	t.Parallel()

	t.Run("the provider-wide value wins over the override", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "first/only-first")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.ContextWindow != 100000 {
			t.Errorf("window is %d, want the provider-wide 100000 rather than the override's 50000", got.ContextWindow)
		}
		if got.MaxOutputTokens != 8192 {
			t.Errorf("cap is %d, want the provider-wide 8192", got.MaxOutputTokens)
		}
	})

	t.Run("the override applies when the provider says nothing", func(t *testing.T) {
		t.Parallel()
		cfg := configured()
		cfg.Providers[0].ContextWindow = 0
		cfg.Providers[0].MaxOutputTokens = 0
		got, err := model.Resolve(cfg, "first/only-first")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.ContextWindow != 50000 || got.MaxOutputTokens != 4096 {
			t.Errorf("resolved window %d cap %d, want the override's 50000 and 4096", got.ContextWindow, got.MaxOutputTokens)
		}
	})

	t.Run("the documented default covers a model nothing is known about", func(t *testing.T) {
		t.Parallel()
		got, err := model.Resolve(configured(), "second/only-second")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		// The literal, not the constant. Comparing against model.DefaultContextWindow let a change to
		// the constant move both sides and keep this test green — a tautology the mutation check found
		// on its first run, which is the entire argument for having one.
		if got.ContextWindow != 160000 {
			t.Errorf("window is %d, want the documented default of 160000", got.ContextWindow)
		}
		// Zero means the vendor decides, which is not the same as a cap of nothing.
		if got.MaxOutputTokens != 0 {
			t.Errorf("cap is %d, want 0 so the vendor decides", got.MaxOutputTokens)
		}
	})
}

// TestCostFollowsThePricesWhenThereAreSome, and says so when there are not.
func TestCostFollowsThePricesWhenThereAreSome(t *testing.T) {
	t.Parallel()
	got, err := model.Resolve(configured(), "first/only-first")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// A million fresh input tokens, a million cached, and a million of output, which makes the
	// arithmetic checkable by eye against the configured prices.
	cost, priced := got.Estimate(model.TurnUsage{Input: 1_000_000, CacheHit: 0, Output: 1_000_000})
	if !priced {
		t.Fatal("a model with prices reported no estimate")
	}
	if cost != 3 {
		t.Errorf("cost is %v, want 3: one per million in, two per million out", cost)
	}

	// Cached tokens are charged at the cache rate instead of the input rate.
	cost, _ = got.Estimate(model.TurnUsage{Input: 1_000_000, CacheHit: 1_000_000, Output: 0})
	if cost != 0.25 {
		t.Errorf("cost is %v, want 0.25 for a fully cached prompt", cost)
	}
}

func TestAModelWithNoPricesEstimatesNothingRatherThanZero(t *testing.T) {
	t.Parallel()
	got, err := model.Resolve(configured(), "second/only-second")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cost, priced := got.Estimate(model.TurnUsage{Input: 1_000_000, Output: 1_000_000}); priced || cost != 0 {
		t.Errorf("estimate is %v/%v, want unpriced: a confident zero is the one wrong answer that looks right", cost, priced)
	}
}
