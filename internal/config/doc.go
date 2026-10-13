// Package config loads, validates and resolves the TOML configuration, and owns
// the paths, roots and identity constants for the whole program.
//
// Resolution order is flags, then the project file, then the user file, then
// built-in defaults (docs/TECHNICAL_DESIGN.md §6.11). Two safety properties
// live here rather than being spread across callers: user-global-only keys a
// project cannot set, and keys a project may only narrow.
package config
