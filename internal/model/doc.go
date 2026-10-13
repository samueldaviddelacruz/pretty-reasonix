// Package model resolves a model reference to a concrete provider and model,
// and answers the questions the loop asks about it: context window, output
// ceiling, effort levels and price.
//
// A reference may be a provider name, a bare model name or an explicit
// provider/model pair; ambiguity is reported rather than guessed
// (docs/TECHNICAL_DESIGN.md §6.11).
package model
