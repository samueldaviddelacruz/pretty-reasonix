// Package skill discovers skills on disk and executes them.
//
// Only a compact index line is kept in the prompt so the prefix stays
// cache-stable, and a skill's body is loaded on demand when it is invoked
// (docs/TECHNICAL_DESIGN.md §6.14).
package skill
