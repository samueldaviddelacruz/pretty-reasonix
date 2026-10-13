// Package openai implements the "openai" provider kind: any OpenAI-compatible
// /chat/completions endpoint, streaming over server-sent events.
//
// Adding another OpenAI-compatible vendor is a configuration entry naming this
// kind, never a code change (docs/TECHNICAL_DESIGN.md §6.3).
package openai
