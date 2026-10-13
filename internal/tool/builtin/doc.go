// Package builtin holds the built-in tool implementations, one file per tool.
//
// A tool declares its own JSON Schema and parses its own arguments, returning
// errors as data so the model can self-correct (docs/TECHNICAL_DESIGN.md §6.6).
// Registration is explicit from internal/assembly: there is no init() and no
// side-effecting blank import anywhere in this module.
package builtin
