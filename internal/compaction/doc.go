// Package compaction decides what to fold when a session approaches its context
// window, and produces the single digest that replaces the folded region.
//
// The window is only rewritten at one trigger, and the canonical transcript is
// never touched: compaction writes a projection, so folded detail stays
// recoverable (docs/TECHNICAL_DESIGN.md §6.9).
//
// The package is named compaction rather than context to avoid colliding with
// the standard library's context package, which every file here also imports
// (rule [#14]).
package compaction
