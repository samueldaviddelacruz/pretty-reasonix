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

// String returns the build report, without the program's name: the caller knows
// what the binary is called, and naming it here would make this package a second
// owner of that string (E1-T8, internal/architecture/identity_test.go).
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
