package agent

import (
	"fmt"
	"slices"
	"strings"

	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/model"
)

// Preset is how much autonomy a turn runs with.
type Preset string

const (
	// PresetBalanced asks before anything irreversible.
	PresetBalanced Preset = "balanced"
	// PresetDelivery runs without asking, for a change whose review happens after it lands.
	PresetDelivery Preset = "delivery"
)

// ParsePreset turns a name into a preset, reporting an unknown one rather than falling back to the
// default. A typo that quietly granted more autonomy would be the worst kind of silent success.
func ParsePreset(name string) (Preset, error) {
	switch preset := Preset(strings.ToLower(strings.TrimSpace(name))); preset {
	case PresetBalanced, PresetDelivery:
		return preset, nil
	case "":
		return PresetBalanced, nil
	default:
		return "", fmt.Errorf("%q is not a preset: known presets are %s, %s", name, PresetBalanced, PresetDelivery)
	}
}

// Policy is what one turn is allowed to do, frozen when it begins.
//
// The copies are deep on purpose. Assigning the configuration's slices would freeze the *headers*, so a
// reload that reused the backing arrays — or any caller that wrote into them — would change what a turn
// in flight believes it may do. That is the difference between a policy and a pointer to one.
type Policy struct {
	Model  model.Model
	Effort provider.Effort
	Preset Preset
	// Permissions is the policy layer: what a tool may do at all.
	Permissions config.Permissions
	// Sandbox is the enforcement layer beneath it: where a tool may do it.
	Sandbox config.Sandbox
	// Temperature is how predictable the answer should be. It travels with the posture rather than the
	// provider because two turns on the same endpoint can want different answers.
	Temperature float64
}

// Snapshot copies the parts of a configuration that govern a turn's behaviour.
//
// It deliberately does not copy the whole configuration: a turn needs its posture and its limits, and
// carrying the provider list and the skill roots into every turn would mean a turn could no longer
// answer the question "what was I allowed to do" without reading all of it.
func Snapshot(cfg *config.Config, m model.Model, effort provider.Effort, preset Preset) Policy {
	if preset == "" {
		preset = PresetBalanced
	}
	policy := Policy{Model: m, Effort: effort, Preset: preset, Temperature: cfg.Agent.Temperature}
	if cfg == nil {
		return policy
	}
	policy.Permissions = config.Permissions{
		Mode:  cfg.Permissions.Mode,
		Deny:  slices.Clone(cfg.Permissions.Deny),
		Allow: slices.Clone(cfg.Permissions.Allow),
		Ask:   slices.Clone(cfg.Permissions.Ask),
	}
	policy.Sandbox = config.Sandbox{
		WorkspaceRoot: cfg.Sandbox.WorkspaceRoot,
		AllowWrite:    slices.Clone(cfg.Sandbox.AllowWrite),
		ForbidRead:    slices.Clone(cfg.Sandbox.ForbidRead),
		Bash:          cfg.Sandbox.Bash,
		Network:       cfg.Sandbox.Network,
	}
	return policy
}

// Block renders the policy as the block a host appends to a turn.
//
// It is written as a list rather than prose because the model reads it as instructions and a person
// reads it to see what the turn was told, and a list serves both. What it deliberately omits is the
// rule text itself: a deny rule names a command, and putting the whole ruleset in every turn is both
// expensive and a hint about what to try next.
func (p Policy) Block() string {
	var b strings.Builder
	b.WriteString("Execution policy for this turn (set by the host, not by you):\n")
	fmt.Fprintf(&b, "- preset: %s\n", p.Preset)
	fmt.Fprintf(&b, "- model: %s\n", p.Model.Name)
	if p.Effort.IsSet() {
		fmt.Fprintf(&b, "- effort: %s\n", p.Effort)
	}
	fmt.Fprintf(&b, "- permission mode: %s\n", defaultString(p.Permissions.Mode, "ask"))
	fmt.Fprintf(&b, "- rules: %d deny, %d allow, %d ask\n",
		len(p.Permissions.Deny), len(p.Permissions.Allow), len(p.Permissions.Ask))
	root := p.Sandbox.WorkspaceRoot
	if root == "" {
		root = "the working directory"
	}
	fmt.Fprintf(&b, "- writes confined to: %s", root)
	if len(p.Sandbox.AllowWrite) > 0 {
		fmt.Fprintf(&b, " and %d other location(s)", len(p.Sandbox.AllowWrite))
	}
	b.WriteString("\n")
	if len(p.Sandbox.ForbidRead) > 0 {
		fmt.Fprintf(&b, "- %d path(s) you may not read\n", len(p.Sandbox.ForbidRead))
	}
	fmt.Fprintf(&b, "- shell: %s, network from the sandbox: %t\n", defaultString(p.Sandbox.Bash, "off"), p.Sandbox.Network)
	return b.String()
}

// Same reports whether two policies grant the same thing, for tests and for a UI that wants to show
// that nothing changed between turns.
func (p Policy) Same(other Policy) bool {
	return p.Preset == other.Preset && p.Model.Name == other.Model.Name && p.Effort == other.Effort &&
		slices.Equal(p.Permissions.Deny, other.Permissions.Deny) &&
		slices.Equal(p.Permissions.Allow, other.Permissions.Allow) &&
		slices.Equal(p.Permissions.Ask, other.Permissions.Ask) &&
		slices.Equal(p.Sandbox.AllowWrite, other.Sandbox.AllowWrite) &&
		slices.Equal(p.Sandbox.ForbidRead, other.Sandbox.ForbidRead)
}

// defaultString keeps a rendered block readable when a value is unset, without inventing one in the
// policy itself.
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
