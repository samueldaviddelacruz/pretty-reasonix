// Package assembly is the one place that wires a run together: configuration,
// then registries, then the provider and tools, then the controller, then the
// chosen frontend.
//
// Registration is explicit here and nowhere else. Capability packages never
// self-register through init(), so wiring order is visible in one file and a
// duplicate registration is an error rather than a panic
// (docs/TECHNICAL_DESIGN.md §6.2, rules [#3] and [#48]).
package assembly
