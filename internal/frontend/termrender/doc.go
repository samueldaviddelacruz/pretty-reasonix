// Package termrender draws output for a terminal and holds no session state: the
// same input values produce the same bytes.
//
// The palette, markdown, diffs, tool cards and width arithmetic live here so
// that the application layer only composes their output, and so that rendering
// can be tested without a terminal (docs/TECHNICAL_DESIGN.md §6.12).
package termrender
