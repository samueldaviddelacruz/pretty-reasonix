// Package session holds the transcript model and its storage: an append-only
// event log, an index sidecar for the picker, and the single-writer lease.
//
// Committed history is never lost. The log has exactly two record kinds and a
// chain check, replay is tolerant up to the first bad record, and an unknown
// schema version is a hard error rather than a guess
// (docs/TECHNICAL_DESIGN.md §6.10).
package session
