// Package event defines the typed events the engine emits and the Sink
// interface a frontend consumes.
//
// This is the only channel between engine and frontend: the terminal UI, the
// headless runner and any future recorder are all sinks over the same stream,
// which is what stops the two frontends from drifting
// (docs/TECHNICAL_DESIGN.md §6.4). Notices and refusals carry a stable code so
// a test can assert on the code rather than on prose.
package event
