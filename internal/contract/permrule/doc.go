// Package permrule holds the permission rule grammar: parsing, formatting and
// matching for the Tool, Tool(specifier) and legacy Tool=literal forms.
//
// The grammar is deliberately its own package so that policy evaluation in
// internal/perm stays pure and I/O-free, and so rule text round-trips exactly
// (docs/TECHNICAL_DESIGN.md §6.7).
package permrule
