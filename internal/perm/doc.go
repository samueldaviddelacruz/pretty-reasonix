// Package perm evaluates the permission policy for a single tool call and
// resolves the Ask outcome through an approver.
//
// Policy evaluation is pure and I/O-free, so precedence is testable in
// isolation: deny, then session grant, then ask, then allow, then the posture
// fallback (docs/TECHNICAL_DESIGN.md §6.7). Permission is policy; enforcement is
// internal/sandbox.
package perm
