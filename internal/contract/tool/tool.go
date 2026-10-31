package tool

import (
	"context"
	"encoding/json"
)

// Tool is one action the model may take.
//
// It is the only thing the model-facing surface promises. Everything a tool can do beyond
// Name/Description/Schema/Execute is declared as an optional interface and discovered by type
// assertion, so the loop can ask "may this run in parallel" without a tool having to care, and without
// a name ever being switched on.
type Tool interface {
	// Name is the identifier the model calls, and must be unique in a registry.
	Name() string
	// Description is what the model reads to decide whether to call it.
	Description() string
	// Schema is the JSON Schema for the arguments. It is canonicalised once at registration, because it
	// travels in every request.
	Schema() json.RawMessage
	// Execute runs the tool. A returned error means the tool is broken; a failure the model should see
	// and can act on belongs in Result.Err instead.
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result is what a tool produced.
type Result struct {
	// Output is what the model reads. It is bounded before it gets here (E5-T3), and Truncated says
	// whether that happened.
	Output string
	// Truncated reports that Output was cut to fit the budget. The model has to know it is not seeing
	// everything, or it concludes a thing it cannot see does not exist.
	Truncated bool
	// Err is a failure of the world rather than of the tool: a file that is not there, a command that
	// exited non-zero. The loop feeds it back to the model instead of ending the turn.
	Err error
	// DurationMs is how long the tool took, for the receipt a person reads.
	DurationMs int
}
