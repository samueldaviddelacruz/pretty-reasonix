// Package hook runs shell commands at named points in a turn, and turns their
// exit status into a verdict.
//
// A hook may observe or block; only the tool and prompt events are blocking, and
// a hook declared by the project runs only after the user approves it
// (docs/TECHNICAL_DESIGN.md §6.14).
package hook
