// Package tool defines the model-facing Tool interface, the optional capability
// interfaces the scheduler and permission layer discover by type assertion, and
// the per-run registry.
//
// Capabilities are asserted rather than looked up by name (docs/
// TECHNICAL_DESIGN.md §6.6): a tool that implements ReadOnly may join a parallel
// batch, one that implements Sequential opts out, and one that implements
// DecisionBarrier stops the batch because a human is needed.
package tool
