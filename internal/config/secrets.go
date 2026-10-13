package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// Secret is a configuration value that must not reach a log line, an error
// message or a rendered table.
//
// It is a type rather than a convention because a plain string leaks at the first
// fmt.Sprintf somebody adds — including one written months later by somebody with
// no reason to know the value was sensitive. Every formatting path is closed:
// String and GoString cover the stringable verbs and the %#v form, and Format
// covers the rest. Reading the value means calling Reveal, whose name is
// deliberately awkward so that every place a secret is handled is greppable and
// reviewable.
type Secret struct {
	// Name is the variable the value came from. Names are safe to print, and they
	// are what make a diagnostic useful: "TEST_KEY is set" tells a reader what to
	// look at.
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

// Format redacts every verb, not only the stringable ones.
//
// String alone is not enough, and the gap is not obvious: for a verb it cannot
// handle through a Stringer — %d, %t, %f — fmt falls back to reflection, and
// reflect can read an unexported field even though Interface cannot. The result is
// that fmt prints the value inside its own complaint, as %!d(string=<the secret>),
// in a log line somebody was writing precisely because they did not know the value
// was sensitive.
func (s Secret) Format(state fmt.State, _ rune) {
	// The write result is deliberately discarded: there is nowhere to report a
	// failure to format, and fmt itself does the same.
	_, _ = state.Write([]byte(s.String()))
}

// envProblem is a line of a dotenv file this package would not read as written,
// kept with its line number so a warning can say where to look.
type envProblem struct {
	line int
	note string
}

// LoadCredentials reads provider keys from the one file that may hold them.
//
// Only the global env file is a source, and the caller is expected to pass
// Paths.EnvFile rather than an arbitrary path — the criterion is that provider keys
// live in one place, and passing somewhere else is how that stops being true.
// A project's own .env deliberately is not a source: a repository that could
// supply a provider key could point the user's requests — and their bill — at
// somebody else's account, and it is a committed file.
//
// A missing file is ordinary, because a user who has configured no provider yet
// has not written one. A line this package could not read, a repeated name, and a
// file others can read are all reported rather than passed over, so that a typo
// does not look like a key that simply does not work.
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
	// A key the whole machine can read is worth knowing about. It is reported
	// rather than refused: the keys are still the user's own, and locking them out
	// of their own configuration over a permission bit would be a worse trade.
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

// parseEnv reads the assignments in a dotenv-style file, returning them and the
// lines it would not read as written.
//
// It is deliberately small: NAME=VALUE, a `#` comment, blank lines, and one layer
// of optional quotes around a value. It does not interpolate, because a
// credentials file that expanded other variables would make what a key resolves to
// depend on the environment it happened to be read in. It does not strip a `#`
// inside a value either: a key may legally contain one, and corrupting a value is
// worse than a trailing comment staying in it.
//
// A repeated name keeps the **first** value and reports the later line, so a
// duplicate can shadow an assignment rather than silently replace it.
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

// UserProviderKeyNames returns the credential variable names declared by the user's
// own configuration, and only by that.
//
// A caller building a scrub list or a reserved-name set must use this rather than
// ProviderKeyNames on a merged configuration, and the reason is a security one.
// Provider entries accumulate across layers, so a committed file can add one — and
// a project that can *widen* this list can name PATH or HOME and have them stripped
// from every tool child, while a project that can *shrink* it turns scrubbing off
// altogether. Reading the user layer alone makes both directions impossible rather
// than merely unlikely, and it does so now, without waiting for the narrowing rules
// that E2-T6 adds.
//
// A missing file is an empty list: a user with no providers has no names to
// reserve. On Windows the comparison of these names against the environment
// necessarily matches case-insensitively, because that is how the platform reads
// it; v1 does not ship there (TDD §5.4).
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

// EnvExpander expands ${NAME} and ${NAME:-default} in a setting's value from an
// explicit set of names.
//
// It is deliberately not os.ExpandEnv. Two differences carry weight: the source is
// a map rather than the process environment, so what a setting can refer to is
// visible where the expander is built; and a name reserved for provider
// credentials is refused outright, so a project's .env cannot supply one by
// defining it.
type EnvExpander struct {
	values map[string]string
	// refused holds the names reserved for credentials. It is a set rather than a
	// slice because the question is only ever "is this name reserved".
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

// ProjectExpander builds the expander for a project's own settings: the values it
// may define, plus the names it may not.
//
// This is where "a project file can never supply a provider key" becomes a property
// of the code rather than a promise in a document. A project .env that defines a
// reserved name is not an error — it is somebody's committed file, and refusing to
// start would be unhelpful — but the definition is dropped and reported.
//
// reserved should come from UserProviderKeyNames.
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

// Expand replaces the references in value. It also returns the reserved names the
// value tried to use, so a caller can report the attempt rather than leave a
// silently empty setting.
//
// A reference with a default still yields that default, because the default is text
// the project wrote rather than the credential it was not allowed to name.
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

// essentialEnvNames are never removed from a child environment, whatever a
// configuration claims. Stripping one of these leaves a shell unable to find its own
// commands, which is a denial of service nobody asked for; a provider entry that
// names one is a mistake, and the name is reported rather than honoured.
var essentialEnvNames = []string{"HOME", "LANG", "PATH", "SHELL", "TMPDIR", "USER"}

// ChildEnv returns the environment for a tool child process with the provider
// credential variables removed, and the requested names it refused to remove because
// a child cannot work without them.
//
// The agent runs shell commands and third-party programs, and neither has any
// business holding the user's provider keys: one `env` in a transcript would put
// them there permanently. Scrubbing is by name rather than by value, so a key read
// from anywhere at all is removed rather than only the ones this package happened to
// load.
//
// An empty keyNames returns the environment unchanged. That is deliberate rather
// than an oversight: a session with no configured provider has nothing to scrub, and
// failing closed here would stop shell commands working on a first run. The guard
// that matters is upstream — keyNames must come from UserProviderKeyNames, so that a
// project file cannot decide it — and the returned slice may share the caller's
// backing array on that path, which is safe because nothing in place is modified.
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
