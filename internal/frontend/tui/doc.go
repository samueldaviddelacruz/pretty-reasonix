// Package tui is the interactive Bubble Tea application.
//
// It is a Sink over the engine's event stream rather than a second engine: the
// transcript is a reducer over events, and the drawing itself is delegated to
// internal/frontend/termrender so that rendering stays stateless and testable
// (docs/TECHNICAL_DESIGN.md §6.12).
package tui
