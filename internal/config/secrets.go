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
// no reason to know the value was sensitive. String and GoString both redact,
// which covers %v, %s, %q, %x and %#v; reading the value means calling Reveal,
// whose name is deliberately awkward so that every place a secret is handled is
// greppable and reviewable.
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

// GoString redacts the %#v form as well, which would otherwise print the struct's
// fields and with them the value.
func (s Secret) GoString() string { return s.String() }

// LoadCredentials reads provider keys from the one file that may hold them.
//
// Only the global env file is a source. A project's own .env deliberately is not:
// a repository that could supply a provider key could point the user's requests —
// and their bill — at somebody else's account, and it is a committed file.
//
// A missing file is ordinary, because a user who has configured no provider yet
// has not written one. A malformed line is reported rather than skipped in
// silence, so a typo does not look like a key that simply does not work.
func LoadCredentials(path string) (map[string]Secret, []Warning, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: the path is the home directory's env file, resolved by this package
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Secret{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	assignments, ignored := parseEnv(string(body))
	credentials := make(map[string]Secret, len(assignments))
	for name, value := range assignments {
		credentials[name] = NewSecret(name, value)
	}

	// The line number is the key: what was ignored here is a line rather than a
	// setting, and the warning has to say which one to look at.
	warnings := make([]Warning, 0, len(ignored))
	for _, line := range ignored {
		warnings = append(warnings, Warning{
			Source: path,
			Key:    fmt.Sprintf("line %d", line),
			Note:   "this line is not a NAME=VALUE assignment; it was ignored",
		})
	}
	return credentials, warnings, nil
}

// parseEnv reads the assignments in a dotenv-style file, returning them and the
// line numbers it could not read.
//
// It is deliberately small: NAME=VALUE, a `#` comment, blank lines, and optional
// single or double quotes around a value. It does not interpolate, because a
// credentials file that expanded other variables would make what a key resolves
// to depend on the environment it was read in.
func parseEnv(body string) (map[string]string, []int) {
	assignments := map[string]string{}
	var ignored []int
	for i, line := range strings.Split(body, "\n") {
		number := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, found := strings.Cut(trimmed, "=")
		name = strings.TrimSpace(name)
		if !found || !validEnvName(name) {
			ignored = append(ignored, number)
			continue
		}
		assignments[name] = unquoteEnvValue(strings.TrimSpace(value))
	}
	return assignments, ignored
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

// ProviderKeyNames returns the variables the configured providers read their keys
// from. A project file may never supply one of these — see ProjectExpander.
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
// This is where "a project file can never supply a provider key" becomes a
// property of the code rather than a promise in a document. A project .env that
// defines a reserved name is not an error — it is somebody's committed file, and
// refusing to start would be unhelpful — but the definition is dropped and
// reported.
func ProjectExpander(path string, reserved []string) (EnvExpander, []Warning, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: the project's own env file, named by the caller
	if errors.Is(err, fs.ErrNotExist) {
		return NewExpander(nil, reserved), nil, nil
	}
	if err != nil {
		return EnvExpander{}, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	assignments, ignored := parseEnv(string(body))
	warnings := make([]Warning, 0, len(ignored)+len(reserved))
	for _, line := range ignored {
		warnings = append(warnings, Warning{
			Source: path,
			Key:    fmt.Sprintf("line %d", line),
			Note:   "this line is not a NAME=VALUE assignment; it was ignored",
		})
	}
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
// A reference with a default still yields that default, because the default is
// text the project wrote rather than the credential it was not allowed to name.
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

// ChildEnv returns the environment for a tool child process, with the provider
// credential variables removed.
//
// The agent runs shell commands and third-party programs, and neither has any
// business holding the user's provider keys: one `env` in a transcript would put
// them there permanently. Scrubbing is by name rather than by value, so a key read
// from anywhere at all is removed rather than only the ones this package happens
// to have loaded.
func ChildEnv(environ, keyNames []string) []string {
	if len(keyNames) == 0 {
		return slices.Clip(environ)
	}
	scrubbed := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(keyNames, name) {
			continue
		}
		scrubbed = append(scrubbed, entry)
	}
	return scrubbed
}
