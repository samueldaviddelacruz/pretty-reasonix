package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"pretty-reasonix/internal/config"
)

// DefaultContextWindow is the window assumed when nothing says otherwise.
//
// A documented assumption rather than a claim about any model: large enough that compaction is not
// triggered early on a modern one, small enough that a request is not refused for asking to fill a
// window that does not exist. It is written down because getting it wrong shows up as a confusing
// truncation rather than as an error.
const DefaultContextWindow = 160000

var (
	// ErrModelNotFound reports a reference that names nothing configured.
	ErrModelNotFound = errors.New("no configured provider offers that model")
	// ErrAmbiguousModel reports a bare model name that more than one provider lists.
	ErrAmbiguousModel = errors.New("more than one provider offers that model")
)

// Model is one provider's model, resolved from a reference.
type Model struct {
	// Provider is the provider entry's name, which is what the wire is built from.
	Provider string
	// Name is the model as the vendor spells it.
	Name string
	// ContextWindow is how much a request and its answer may occupy, in tokens.
	ContextWindow int
	// MaxOutputTokens is the cap to send, or 0 to let the vendor decide.
	MaxOutputTokens int
	// Price is what this model costs, when the configuration names it.
	Price config.Price
}

// Resolve turns a model reference into a model.
//
// Three forms are accepted, tried in this order: "provider/model" exactly; a provider's own name,
// meaning its default model; and a bare model ID, meaning the one provider that lists it.
//
// A bare ID that more than one provider lists is an error naming them, not a first-wins guess. Two
// entries for the same model may point at different accounts or different prices, and picking one
// silently would charge somebody for a choice they did not make — which is worse than refusing and
// asking them to write down which one they meant.
func Resolve(cfg *config.Config, reference string) (Model, error) {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		return Model{}, fmt.Errorf("%w: the reference is empty", ErrModelNotFound)
	}

	// Tried against real provider names, so a model ID that happens to contain a slash is not
	// mistaken for a provider and a typo in a provider name is not reported as an unknown model.
	if providerName, modelName, found := strings.Cut(ref, "/"); found {
		if entry, ok := findProvider(cfg, providerName); ok {
			return build(entry, modelName)
		}
	}

	if entry, ok := findProvider(cfg, ref); ok {
		return build(entry, defaultModelOf(entry))
	}

	var candidates []string
	for _, entry := range cfg.Providers {
		if slices.Contains(entry.Models, ref) {
			candidates = append(candidates, entry.Name)
		}
	}
	switch len(candidates) {
	case 0:
		return Model{}, fmt.Errorf("%w: %q", ErrModelNotFound, ref)
	case 1:
		entry, _ := findProvider(cfg, candidates[0])
		return build(entry, ref)
	default:
		return Model{}, fmt.Errorf("%w: %q is listed by %s", ErrAmbiguousModel, ref, strings.Join(candidates, ", "))
	}
}

// findProvider looks an entry up by its configured name.
func findProvider(cfg *config.Config, name string) (config.Provider, bool) {
	for _, entry := range cfg.Providers {
		if entry.Name == name {
			return entry, true
		}
	}
	return config.Provider{}, false
}

// defaultModelOf is the model an entry uses when a reference names only the provider.
func defaultModelOf(entry config.Provider) string {
	if entry.Default != "" {
		return entry.Default
	}
	if len(entry.Models) > 0 {
		return entry.Models[0]
	}
	return ""
}

// build assembles a model from its entry and the model's own name.
func build(entry config.Provider, name string) (Model, error) {
	if name == "" {
		return Model{}, fmt.Errorf("%w: provider %q names no default model", ErrModelNotFound, entry.Name)
	}
	override := entry.ModelOverrides[name]

	return Model{
		Provider: entry.Name,
		Name:     name,
		// Provider first, then the per-model override, then what is known about the model itself,
		// then the documented default. That order is the criterion's, and it makes a provider-wide
		// value a floor somebody chose for every model on that endpoint rather than something an
		// individual override quietly undercuts.
		ContextWindow:   firstPositive(entry.ContextWindow, override.ContextWindow, knownContextWindow(name), DefaultContextWindow),
		MaxOutputTokens: firstPositive(entry.MaxOutputTokens, override.MaxOutputTokens),
		Price:           entry.Prices[name],
	}, nil
}

// knownContextWindow is what this program knows about a model without being told.
//
// Deliberately empty for now. The rung exists because the chain needs it, and it stays empty because
// writing a token count from memory would be a claim about a vendor's published limit that nothing
// here can verify — and a wrong one shows up as a truncation nobody can explain. It fills from the
// model catalogue when that exists; until then a configuration that names context_window is always
// right and the documented default covers the rest.
func knownContextWindow(string) int { return 0 }

// firstPositive returns the first positive value, which is how a zero means "not set" all the way
// down the chain rather than being a limit of nothing.
func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// Estimate is what a turn cost at this model's prices, in the currency those prices are written in.
//
// An estimate, not an invoice: the prices come from configuration and the token counts from the
// vendor, and neither is a bill. Fixed-point money and currency conversion belong with billing, which
// is not in v1 — so this returns false rather than zero when no prices are configured, because a
// confident zero is the one wrong answer that looks right.
//
// Cache writes are charged at the input rate. Some vendors bill them at a premium, which this
// estimate does not model; it is stated here rather than left for somebody to discover from a bill.
func (m Model) Estimate(usage TurnUsage) (float64, bool) {
	if m.Price == (config.Price{}) {
		return 0, false
	}
	cached := usage.CacheHit
	// A vendor that counts cached tokens inside the input count and one that reports them beside it
	// both exist; clamping keeps an estimate from going negative rather than picking a side.
	fresh := max(usage.Input-cached, 0)
	const perMillion = 1_000_000
	cost := float64(fresh)/perMillion*m.Price.Input +
		float64(cached)/perMillion*m.Price.CacheHit +
		float64(usage.CacheWrite)/perMillion*m.Price.Input +
		float64(usage.Output)/perMillion*m.Price.Output
	return cost, true
}
