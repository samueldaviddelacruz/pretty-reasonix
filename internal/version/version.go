// Package version carries the build metadata for the prx binary.
//
// The values are injected at link time (see the Makefile's build target) and
// default to development placeholders so an unreleased `go build` still
// produces something honest rather than an empty string.
package version

import "fmt"

var (
	// Version is the release identifier, normally from git describe.
	Version = "dev"
	// Commit is the short revision the binary was built from.
	Commit = "none"
	// Date is the build timestamp in RFC 3339 form.
	Date = "unknown"
)

// String returns the one-line version report printed by `prx version`.
func String() string {
	return fmt.Sprintf("prx %s (commit %s, built %s)", Version, Commit, Date)
}
