package config

import (
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// IgnoredProjectSettings is what a project file asked for and may not have: an
// attempt to widen a permission, the sandbox or the tool surface, or to set a
// setting only the user's own configuration may decide.
//
// It is a distinct type rather than more Warning values because these are security
// notices and the compatibility ones are not: `prx doctor` should show them
// prominently, and a stricter caller may want to refuse to run when a project asked
// for more than it is entitled to.
type IgnoredProjectSettings []Warning

// String lists the keys, which is what a one-line summary needs.
func (s IgnoredProjectSettings) String() string {
	if len(s) == 0 {
		return "the project file asked for nothing it may not have"
	}
	keys := make([]string, 0, len(s))
	for _, ignored := range s {
		keys = append(keys, ignored.Key)
	}
	slices.Sort(keys)
	return "the project file asked for settings it may not have: " + strings.Join(keys, ", ")
}

// The shell jail's enforcing mode and its escape hatch, named so that the rule
// below reads as a sentence rather than as two string literals.
const (
	bashEnforce = "enforce"
	bashOff     = "off"
)

// narrowingRule is one setting a project may only make more restrictive.
//
// wider answers the only question that matters — did the project's value grant
// something the value it replaced did not — and the answer decides whether the
// project's value is kept or put back. Every rule compares against what the key held
// *before* the project file was read, which is what makes a rule independent of both
// the defaults and the layers underneath.
type narrowingRule struct {
	// key is the dotted TOML path, which is also what the notice names.
	key string
	// wider reports whether after grants more than before.
	wider func(before, after *Config) bool
	// why explains the refusal in the user's terms.
	why string
}

// narrowOnly is the table of settings a project may narrow but never widen.
//
// The rows are deliberately conservative: where a direction was arguable the setting
// is treated as wider. The cost of refusing a legitimate narrowing is a notice the
// user can read; the cost of allowing a widening is a repository silently changing
// what the agent may do on their machine.
var narrowOnly = []narrowingRule{
	{
		key: "permissions.mode",
		wider: func(before, after *Config) bool {
			return permissionRank(after.Permissions.Mode) > permissionRank(before.Permissions.Mode)
		},
		why: "a project may move the fallback toward denying, not toward allowing",
	},
	{
		key: "permissions.allow",
		wider: func(before, after *Config) bool {
			return grantsSomething(before.Permissions.Allow, after.Permissions.Allow)
		},
		why: "a project may not grant itself a permission the user did not",
	},
	{
		key: "permissions.ask",
		wider: func(before, after *Config) bool {
			return withholdsSomething(before.Permissions.Ask, after.Permissions.Ask)
		},
		why: "a project may add a prompt, not remove one",
	},
	{
		key:   "sandbox.network",
		wider: func(before, after *Config) bool { return !before.Sandbox.Network && after.Sandbox.Network },
		why:   "a project may not turn network access on",
	},
	{
		key: "sandbox.bash",
		wider: func(before, after *Config) bool {
			return before.Sandbox.Bash == bashEnforce && after.Sandbox.Bash == bashOff
		},
		why: "a project may not take shell commands out of the OS jail",
	},
	{
		key: "sandbox.allow_write",
		wider: func(before, after *Config) bool {
			return grantsSomething(before.Sandbox.AllowWrite, after.Sandbox.AllowWrite)
		},
		why: "a project may narrow the writable roots, not add one",
	},
	{
		key: "sandbox.forbid_read",
		wider: func(before, after *Config) bool {
			return withholdsSomething(before.Sandbox.ForbidRead, after.Sandbox.ForbidRead)
		},
		why: "a project may protect more paths, not fewer",
	},
	{
		key: "tools.enabled",
		// An empty list means every built-in, so the set comparison runs the other
		// way round: anything a project *lists* against a user who listed nothing
		// is a restriction, and a project that empties the list has turned the
		// restriction off. Writing the cases out is what makes this rule readable —
		// [] to [x] narrows, [x] to [] widens, [x] to [x,y] widens, [x,y] to [x]
		// narrows.
		wider: func(before, after *Config) bool {
			user, project := before.Tools.Enabled, after.Tools.Enabled
			if len(user) == 0 {
				return false
			}
			if len(project) == 0 {
				return true
			}
			return grantsSomething(user, project)
		},
		why: "a project may narrow the tool surface, and an empty list means every built-in",
	},
}

// permissionRank orders the writer fallback by how much it grants, so a move in
// either direction is one comparison rather than a pair of special cases.
func permissionRank(mode string) int {
	switch mode {
	case "deny":
		return 0
	case "ask":
		return 1
	default:
		// "allow", and anything unrecognised. An unknown mode is ranked as
		// permissive so that a project cannot widen by writing a value this version
		// does not yet know.
		return 2
	}
}

// grantsSomething reports whether after names an entry before did not.
func grantsSomething(before, after []string) bool {
	for _, entry := range after {
		if !slices.Contains(before, entry) {
			return true
		}
	}
	return false
}

// withholdsSomething reports whether before named an entry after does not.
func withholdsSomething(before, after []string) bool {
	for _, entry := range before {
		if !slices.Contains(after, entry) {
			return true
		}
	}
	return false
}

// NarrowOnlyKeys returns the settings a project may only make more restrictive.
//
// It is exported for the same reason UserGlobalKeys is: a caller that reports what a
// project may not do — `prx doctor`, the TUI's notice area — needs the list itself,
// and a test that proves every declared rule is actually enforced needs to iterate
// the declarations rather than a copy that can drift from them.
func NarrowOnlyKeys() []string {
	keys := make([]string, 0, len(narrowOnly))
	for _, rule := range narrowOnly {
		keys = append(keys, rule.key)
	}
	return keys
}

// holdBackWidenings puts back every narrow-only setting the project widened, and
// returns the notices listing them.
//
// meta decides whether the project *named* a key at all: a key it never mentioned
// keeps what the user's configuration decided, and must not be reported as an
// attempt merely because it differs from the default.
func holdBackWidenings(cfg, before *Config, meta toml.MetaData, source string) IgnoredProjectSettings {
	var ignored IgnoredProjectSettings
	for _, rule := range narrowOnly {
		if !meta.IsDefined(strings.Split(rule.key, ".")...) {
			continue
		}
		if !rule.wider(before, cfg) {
			continue // a narrowing, which is the point of a project file
		}
		restoreKey(cfg, before, rule.key)
		ignored = append(ignored, Warning{
			Source: source,
			Key:    rule.key,
			Note:   rule.why + "; the user's value stands",
		})
	}
	return ignored
}

// holdBackUserGlobals restores every key the project named that only the user's own
// configuration may decide, and reports each one.
func holdBackUserGlobals(cfg, before *Config, meta toml.MetaData, source string) IgnoredProjectSettings {
	var ignored IgnoredProjectSettings
	for _, key := range UserGlobalKeys {
		if !meta.IsDefined(strings.Split(key, ".")...) {
			continue
		}
		restoreKey(cfg, before, key)
		ignored = append(ignored, Warning{
			Source: source,
			Key:    key,
			Note:   "only the user's configuration may set this; the project's value was ignored",
		})
	}
	return ignored
}

// restoreKey puts back the value a key held before the project file was read, and
// reports whether it knows how to restore that key.
//
// It is shared by both hold-back rules, and the return value is the reason: a key
// declared in UserGlobalKeys or in narrowOnly with no branch here would leave the
// value it was supposed to refuse in place, so a test asserts every declared key is
// restorable. That is what keeps the declarations and this function in step.
func restoreKey(cfg, before *Config, key string) bool {
	switch key {
	case "ui.commandmode":
		cfg.UI.CommandMode = before.UI.CommandMode
	case "permissions.mode":
		cfg.Permissions.Mode = before.Permissions.Mode
	case "permissions.allow":
		cfg.Permissions.Allow = before.Permissions.Allow
	case "permissions.ask":
		cfg.Permissions.Ask = before.Permissions.Ask
	case "sandbox.network":
		cfg.Sandbox.Network = before.Sandbox.Network
	case "sandbox.bash":
		cfg.Sandbox.Bash = before.Sandbox.Bash
	case "sandbox.allow_write":
		cfg.Sandbox.AllowWrite = before.Sandbox.AllowWrite
	case "sandbox.forbid_read":
		cfg.Sandbox.ForbidRead = before.Sandbox.ForbidRead
	case "tools.enabled":
		cfg.Tools.Enabled = before.Tools.Enabled
	default:
		return false
	}
	return true
}
