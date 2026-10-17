package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// Secret is a configuration value that must not reach a log line, an error message or a table.
//
// A type rather than a convention, because a plain string leaks at the first fmt.Sprintf somebody
// adds. String, GoString and Format close every formatting path, and Reveal is the only way out —
// named awkwardly so each place a secret is handled is greppable.
type Secret struct {
	// Name is the variable it came from: safe to print, and what makes a diagnostic useful.
	Name string
	// value is unexported so that the only way out is Reveal.
	value string
}

// NewSecret wraps a value read from somewhere, naming where it came from.
func NewSecret(name, value string) Secret {
	return Secret{Name: name, value: value}
}

// Reveal returns the value itself. Every caller is a place a secret is handled,
// and every one of them should be visible in a diff.
func (s Secret) Reveal() string { return s.value }

// IsSet reports whether a value was found, without revealing it.
func (s Secret) IsSet() bool { return s.value != "" }

// String redacts: it reports whether the secret is set, never what it is.
func (s Secret) String() string {
	if !s.IsSet() {
		return s.Name + " is not set"
	}
	return s.Name + " is set ([redacted])"
}

// GoString redacts the %#v form, which would otherwise print the struct's fields
// and with them the value.
func (s Secret) GoString() string { return s.String() }

// Format redacts every verb, not only the stringable ones. For %d, %t or %f, fmt falls
// back to reflection, which reads an unexported field — and prints the value inside its
// own complaint: %!d(string=<the secret>).
func (s Secret) Format(state fmt.State, _ rune) {
	// Nothing to do about a write error here; fmt discards it too.
	_, _ = state.Write([]byte(s.String()))
}

// envProblem is a line of a dotenv file this package would not read as written,
// kept with its line number so a warning can say where to look.
type envProblem struct {
	line int
	note string
}

// LoadCredentials reads provider keys from the global env file — Paths.EnvFile, not an
// arbitrary path.
//
// A project's own .env is deliberately not a source: a repository that could supply a
// provider key could point the user's requests, and their bill, at another account. A
// missing file is ordinary; an unreadable line, a repeated name and a file others can read
// are all reported.
func LoadCredentials(path string) (map[string]Secret, []Warning, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: the home directory's env file, resolved by this package
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Secret{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	assignments, problems := parseEnv(string(body))
	credentials := make(map[string]Secret, len(assignments))
	for name, value := range assignments {
		credentials[name] = NewSecret(name, value)
	}

	warnings := warningsForEnvProblems(path, problems)
	// Reported rather than refused: locking someone out of their own keys over a mode bit is worse.
	if mode, modeErr := envFileMode(path); modeErr == nil && mode.Perm()&0o077 != 0 {
		warnings = append(warnings, Warning{
			Source: path,
			Key:    fmt.Sprintf("mode %#o", mode.Perm()),
			Note:   "provider keys should live in a file only their owner can read, and this one is readable by others",
		})
	}
	return credentials, warnings, nil
}

// envFileMode reports the permissions of an existing env file.
func envFileMode(path string) (fs.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("checking %s: %w", path, err)
	}
	return info.Mode(), nil
}

// parseEnv reads NAME=VALUE, `#` comments, blank lines and one layer of optional quotes. It
// does not interpolate — a key whose value depended on the reading environment would be a
// trap — and does not strip an inline `#`, which may legitimately be part of a value.
//
// A repeated name keeps the first value and reports the later line, so a duplicate cannot
// silently replace an assignment.
func parseEnv(body string) (map[string]string, []envProblem) {
	assignments := map[string]string{}
	var problems []envProblem
	for i, line := range strings.Split(body, "\n") {
		number := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, found := strings.Cut(trimmed, "=")
		name = strings.TrimSpace(name)
		if !found || !validEnvName(name) {
			problems = append(problems, envProblem{
				line: number,
				note: "this line is not a NAME=VALUE assignment; it was ignored",
			})
			continue
		}
		if _, repeated := assignments[name]; repeated {
			problems = append(problems, envProblem{
				line: number,
				note: "this name is already assigned above; the earlier value stands and this line was ignored",
			})
			continue
		}
		assignments[name] = unquoteEnvValue(strings.TrimSpace(value))
	}
	return assignments, problems
}

// warningsForEnvProblems turns the lines this package would not read into warnings
// naming the file and the line, so that "which line?" is answerable.
func warningsForEnvProblems(path string, problems []envProblem) []Warning {
	if len(problems) == 0 {
		return nil
	}
	warnings := make([]Warning, 0, len(problems))
	for _, problem := range problems {
		warnings = append(warnings, Warning{
			Source: path,
			Key:    fmt.Sprintf("line %d", problem.line),
			Note:   problem.note,
		})
	}
	return warnings
}

// validEnvName reports whether name is usable as a variable name, which is what
// stops a line like "this is prose" from becoming a credential.
func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// unquoteEnvValue strips one layer of matching quotes, so that a value with a
// trailing space can be written in a file without the space becoming part of it.
func unquoteEnvValue(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// ProviderKeyNames returns the variables the providers in this configuration read
// their keys from.
//
// Use it only on a **user-layer** configuration — see UserProviderKeyNames. On a
// merged one it answers with whatever the highest layer said.
func (c *Config) ProviderKeyNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.Providers))
	for _, provider := range c.Providers {
		if provider.APIKeyEnv != "" {
			names = append(names, provider.APIKeyEnv)
		}
	}
	return names
}

// UserProviderKeyNames returns the credential variable names the user's own configuration
// declares, and only those.
//
// A scrub list must come from here rather than from the merged configuration: provider entries
// accumulate across layers, so a committed file could widen this list to PATH or HOME and have
// them stripped from every tool child, or shrink it and turn scrubbing off. A missing file is an
// empty list.
func UserProviderKeyNames(userPath string) ([]string, error) {
	if userPath == "" {
		return nil, nil
	}
	cfg, _, err := LoadFile(userPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cfg.ProviderKeyNames(), nil
}

// EnvExpander expands ${NAME} and ${NAME:-default} from an explicit set of names.
//
// Deliberately not os.ExpandEnv: the source is a map rather than the process environment, and a
// name reserved for provider credentials is refused outright, so a project's .env cannot supply
// one by defining it.
type EnvExpander struct {
	values map[string]string
	// refused holds the credential names; a set, since the question is only "is this reserved".
	refused map[string]bool
}

// NewExpander builds an expander over named values, refusing the reserved names.
func NewExpander(values map[string]string, reserved []string) EnvExpander {
	refused := make(map[string]bool, len(reserved))
	for _, name := range reserved {
		refused[name] = true
	}
	permitted := make(map[string]string, len(values))
	for name, value := range values {
		if !refused[name] {
			permitted[name] = value
		}
	}
	return EnvExpander{values: permitted, refused: refused}
}

// ProjectExpander builds the expander for a project's own settings: the values it may define, plus
// the names it may not, which should come from UserProviderKeyNames.
//
// This is where "a project file can never supply a provider key" is code rather than a promise. A
// definition of a reserved name is dropped and reported rather than treated as an error: it is
// somebody's committed file, and refusing to start would be unhelpful.
func ProjectExpander(path string, reserved []string) (EnvExpander, []Warning, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: the project's own env file, named by the caller
	if errors.Is(err, fs.ErrNotExist) {
		return NewExpander(nil, reserved), nil, nil
	}
	if err != nil {
		return EnvExpander{}, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	assignments, problems := parseEnv(string(body))
	warnings := warningsForEnvProblems(path, problems)
	for _, name := range reserved {
		if _, defined := assignments[name]; !defined {
			continue
		}
		warnings = append(warnings, Warning{
			Source: path,
			Key:    name,
			Note:   "a project file may not supply a provider credential; this definition was dropped",
		})
	}
	return NewExpander(assignments, reserved), warnings, nil
}

// Expand replaces the references in value, and returns the reserved names it refused so a caller
// can report the attempt rather than leaving a silently empty setting.
//
// A reference with a default still yields it: the default is text the project wrote, not the
// credential it was not allowed to name.
func (e EnvExpander) Expand(value string) (string, []string) {
	var attempted []string
	expanded := os.Expand(value, func(reference string) string {
		name, fallback, hasDefault := strings.Cut(reference, ":-")
		if e.refused[name] {
			attempted = append(attempted, name)
			if hasDefault {
				return fallback
			}
			return ""
		}
		if resolved, ok := e.values[name]; ok && resolved != "" {
			return resolved
		}
		if hasDefault {
			return fallback
		}
		return ""
	})
	return expanded, attempted
}

// essentialEnvNames are never removed from a child environment: stripping one leaves a shell unable
// to find its commands. A provider entry naming one is reported rather than honoured.
var essentialEnvNames = []string{"HOME", "LANG", "PATH", "SHELL", "TMPDIR", "USER"}

// ChildEnv returns a tool child's environment with the provider credential variables removed, plus
// the requested names it refused to remove because a child cannot work without them. Scrubbing is by
// name, so a key read from anywhere at all is removed rather than only the ones this package loaded.
//
// An empty keyNames returns the environment unchanged, deliberately: a session with no provider has
// nothing to scrub, and failing closed would stop shell commands on a first run. The guard that
// matters is upstream — keyNames must come from UserProviderKeyNames — and the returned slice may
// share the caller's array.
func ChildEnv(environ, keyNames []string) ([]string, []string) {
	if len(keyNames) == 0 {
		return slices.Clip(environ), nil
	}
	childEnv := make([]string, 0, len(environ))
	var refused []string
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		switch {
		case slices.Contains(essentialEnvNames, name):
			if slices.Contains(keyNames, name) {
				refused = append(refused, name)
			}
			childEnv = append(childEnv, entry)
		case slices.Contains(keyNames, name):
			// Removed, and nothing to say: this is the case the function exists for.
		default:
			childEnv = append(childEnv, entry)
		}
	}
	return childEnv, refused
}
