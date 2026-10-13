// Package agent is the turn loop: build a request, stream the answer, collect
// tool calls, execute them as a batch, and repeat until the model stops or a
// budget is exhausted.
//
// Host policy lives here and is deterministic — batching, readiness and the
// completion report are decided from structure, never from the model's wording
// (docs/TECHNICAL_DESIGN.md §6.5).
package agent
