package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Location is one entry in the on-disk layout.
//
// The layout is described once, here, and everything else derives from it: a
// path that is not in this table has no documented home, and the test in
// paths_test.go fails on a Paths field the table does not cover.
type Location struct {
	// Field is the Paths field this entry resolves to. The test uses the name to
	// prove the table and the struct have not drifted apart.
	Field string
	// Rel is the path relative to the home directory; "." is the home itself.
	Rel string
	// Dir is true when the entry is a directory.
	Dir bool
	// Mode is the permission the entry is created with, as documented in
	// docs/TECHNICAL_DESIGN.md §6.10. Directories are 0700 because the tree
	// holds credentials and transcripts: only their owner may traverse it.
	Mode fs.FileMode
	// Purpose is what lives there, in one line. It is what `prx doctor` (E19-T3)
	// prints and what a layout failure explains.
	Purpose string
}

// Layout is the single description of where this program keeps things, matching
// docs/TECHNICAL_DESIGN.md §6.10.
var Layout = []Location{
	{Field: "Home", Rel: ".", Dir: true, Mode: 0o700,
		Purpose: "the home directory: configuration, credentials and all state"},
	{Field: "ConfigFile", Rel: "config.toml", Mode: 0o644,
		Purpose: "the user-global configuration"},
	{Field: "EnvFile", Rel: ".env", Mode: 0o600,
		Purpose: "provider API keys, named by a provider's api_key_env"},
	{Field: "TrustFile", Rel: "trust.json", Mode: 0o600,
		Purpose: "the workspace folders the user has trusted"},
	{Field: "CommandsDir", Rel: "commands", Dir: true, Mode: 0o700,
		Purpose: "custom slash commands"},
	{Field: "SkillsDir", Rel: "skills", Dir: true, Mode: 0o700,
		Purpose: "custom skills"},
	{Field: "SettingsFile", Rel: "settings.json", Mode: 0o600,
		Purpose: "hook configuration"},
	{Field: "SessionsDir", Rel: "sessions", Dir: true, Mode: 0o700,
		Purpose: "session transcripts, indexes, leases and spilled output"},
	{Field: "LogsDir", Rel: "logs", Dir: true, Mode: 0o700,
		Purpose: "local diagnostics, rotated and bounded"},
}

// Paths is the resolved layout: every location this program uses, derived from
// one home directory.
type Paths struct {
	Home         string
	ConfigFile   string
	EnvFile      string
	TrustFile    string
	CommandsDir  string
	SkillsDir    string
	SettingsFile string
	SessionsDir  string
	LogsDir      string
}

// Resolve derives every path from one home directory.
//
// It performs no I/O, so it is testable without a filesystem, and it is the only
// place any of these paths is built — which is what makes the layout in
// docs/TECHNICAL_DESIGN.md §6.10 a description of the program rather than a
// coincidence.
func Resolve(home string) Paths {
	return Paths{
		Home:         home,
		ConfigFile:   filepath.Join(home, "config.toml"),
		EnvFile:      filepath.Join(home, ".env"),
		TrustFile:    filepath.Join(home, "trust.json"),
		CommandsDir:  filepath.Join(home, "commands"),
		SkillsDir:    filepath.Join(home, "skills"),
		SettingsFile: filepath.Join(home, "settings.json"),
		SessionsDir:  filepath.Join(home, "sessions"),
		LogsDir:      filepath.Join(home, "logs"),
	}
}

// Home returns the home directory: $PRX_HOME when it is set and non-empty,
// otherwise the user's home directory joined with HomeDirName.
//
// A relative PRX_HOME is refused rather than resolved. A relocated home that
// depended on the working directory would silently point somewhere different in
// every session, which is a class of bug worth failing loudly at startup.
func Home() (string, error) {
	relocated := os.Getenv(HomeEnvVar)
	if relocated == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the user home directory: %w", err)
		}
		return filepath.Join(user, HomeDirName), nil
	}
	if !filepath.IsAbs(relocated) {
		return "", fmt.Errorf("%s must be an absolute path, got %q", HomeEnvVar, relocated)
	}
	return filepath.Clean(relocated), nil
}

// ProjectConfigPath returns the project configuration that sits beside dir.
//
// A project file is read from here and never written by this program unless the
// user asks for a project-scoped change (E19-T2), so this only composes a path.
func ProjectConfigPath(dir string) string {
	return filepath.Join(dir, ProjectConfigFile)
}
