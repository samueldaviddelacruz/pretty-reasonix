// Package mcp is the Model Context Protocol client: it starts or connects to a
// server, lists its capabilities, and adapts its tools, prompts and resources
// into this module's own abstractions.
//
// Only the transport differs between a local subprocess and a remote endpoint;
// the protocol logic is written once (docs/TECHNICAL_DESIGN.md §6.14).
package mcp
