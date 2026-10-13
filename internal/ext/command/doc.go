// Package command loads custom slash commands from markdown files in the
// project and user directories, and renders their arguments.
//
// A malformed file is skipped with a warning rather than being fatal, and a
// project command shadows a global one of the same name
// (docs/TECHNICAL_DESIGN.md §6.14).
package command
