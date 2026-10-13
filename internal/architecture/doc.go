// Package architecture holds the tests that guard this repository's own
// structural rules: the import layering in docs/TECHNICAL_DESIGN.md §6.2 and the
// path-scoped exclusions in .golangci.yml.
//
// It contains no production code, deliberately. The rules need one owner, and
// putting the enforcement in its own package means the enforcement is subject to
// the layering rule too: this package imports nothing else in the module, which
// is why it is ranked alongside the contract layer.
package architecture
