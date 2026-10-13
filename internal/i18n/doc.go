// Package i18n holds the catalogue of user-facing messages.
//
// Every string the terminal UI, the headless runner and the CLI print goes
// through it, including status-line labels, so that adding a language is a
// catalogue rather than a search for string literals
// (docs/TECHNICAL_DESIGN.md §6.16). Model-visible prompt text is not localised
// here.
package i18n
