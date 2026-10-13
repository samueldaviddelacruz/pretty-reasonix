package config

// The identity constants live in this file and nowhere else.
//
// A test asserts that (E1-T8): the binary name, the home directory and the
// project file are the three strings that would otherwise leak into every path,
// prompt and message in the program, so changing them is meant to be one edit
// rather than a search. Code elsewhere refers to these constants; only
// documentation is allowed to spell them out.
//
// docs/TECHNICAL_DESIGN.md §3.1 records why they are ours rather than Reasonix's:
// the vocabulary is mirrored so its documentation and habits carry over, but a
// second program with the same name, the same home directory and the same
// project file would be hostile to anyone who has both installed.
const (
	// BinaryName is the command the user types and the Makefile builds.
	BinaryName = "prx"

	// HomeDirName is the directory under the user's home that holds all of this
	// program's state.
	HomeDirName = ".prx"

	// HomeEnvVar relocates HomeDirName, which is what lets a test or a portable
	// install keep its state to itself.
	HomeEnvVar = "PRX_HOME"

	// EnvPrefix is the prefix for this program's own environment variables.
	EnvPrefix = "PRX_"

	// ProjectConfigFile is the per-project configuration, found beside the code
	// it configures.
	ProjectConfigFile = "prx.toml"
)
