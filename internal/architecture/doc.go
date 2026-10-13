// Package architecture holds the dependency-rule test: the check that this
// module's import graph matches the layering declared in
// docs/TECHNICAL_DESIGN.md §6.2.
//
// It contains no production code, deliberately. The rule needs one owner, and
// putting the enforcement in its own package means the enforcement is subject to
// the rule too: this package imports nothing else in the module, which is why it
// is ranked alongside the contract layer.
package architecture
