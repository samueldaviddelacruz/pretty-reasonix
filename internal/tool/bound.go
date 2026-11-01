package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"pretty-reasonix/internal/contract/tool"
)

// Bounds on what a tool's output costs.
const (
	// MaxVisibleBytes is how much of a tool's output the model reads. 32 KiB is roughly eight thousand
	// tokens: big enough for a whole source file, small enough that four of them do not fill a context
	// window. Named rather than inlined because it is a budget, and a budget somebody can find.
	MaxVisibleBytes = 32 * 1024
	// SpillAbove is the size above which a body goes to disk instead of being held in memory. The whole
	// point of a spill is not to keep the megabytes in the process, so this sits just above what a
	// transcript would tolerate anyway.
	SpillAbove = 256 * 1024
)

// ErrNoSpillDir reports a body that needed spilling and had nowhere to go.
var ErrNoSpillDir = errors.New("a spilled tool body needs a directory")

// Bound is a tool result after bounding.
//
// It is the record, not the result: Visible is what a model reads, Body is the whole text when the whole
// text differs from Visible and is small enough to keep, and SpillPath is where the rest went when it was
// not. Keeping the two apart is what makes "maintenance never rewrites a stored body" a property rather
// than a promise — Bounding writes once and everything after it only reads.
type Bound struct {
	// Visible is what the model reads, at most MaxVisibleBytes.
	Visible string
	// Truncated says Visible is not the whole output, so the model does not conclude that something it
	// cannot see does not exist.
	Truncated bool
	// Body is the full text when it differs from Visible and was small enough to keep. Nil otherwise:
	// identical text is not worth holding twice, and a spilled body is on disk.
	Body *string
	// SpillPath is the file holding the whole body, when it was too large to keep.
	SpillPath string
	// OriginalBytes is the size before bounding, so a receipt can say how much was hidden.
	OriginalBytes int
}

// Boundary bounds tool output on its way into the transcript.
type Boundary struct {
	// Dir is where spilled bodies go — the session's own directory, so a spill is cleaned up with the
	// session rather than accumulating somewhere nobody looks.
	Dir string
	// Snip is the geometry to keep when a tool declares one. The zero value keeps the default split.
	Snip tool.Snip
}

// Bound bounds one result, once.
//
// Called once, at the point a result first enters the transcript. Calling it again on an already-bounded
// value is deliberately not special-cased: the type is what makes "once" true, because a Bound holds no
// reference the caller can rewrite and a spill file is written only when it does not already exist.
func (b Boundary) Bound(result tool.Result) (Bound, error) {
	full := result.Output
	bound := Bound{OriginalBytes: len(full)}
	if len(full) <= MaxVisibleBytes {
		bound.Visible = full
		bound.Truncated = result.Truncated
		return bound, nil
	}

	bound.Visible = shorten(full, b.Snip)
	bound.Truncated = true

	if len(full) > SpillAbove {
		if b.Dir == "" {
			return Bound{}, ErrNoSpillDir
		}
		path, err := b.spill(full)
		if err != nil {
			return Bound{}, err
		}
		bound.SpillPath = path
		return bound, nil
	}
	kept := full
	bound.Body = &kept
	return bound, nil
}

// spill writes a body to the session directory under a name derived from its content.
//
// Derived rather than random, and written only when absent: the same output spilled twice is the same
// file, and the second attempt leaves the first alone. That is the whole of "maintenance never rewrites a
// stored body" — there is no code path that opens an existing spill for writing.
func (b Boundary) spill(full string) (string, error) {
	sum := sha256.Sum256([]byte(full))
	path := filepath.Join(b.Dir, hex.EncodeToString(sum[:16])+".txt")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("checking the spill file: %w", err)
	}
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		return "", fmt.Errorf("making the spill directory: %w", err)
	}
	// Owner-only: tool output can contain anything the tool read, including a file the user did not expect
	// to leave the workspace.
	if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
		return "", fmt.Errorf("writing the spill file: %w", err)
	}
	return path, nil
}

// shorten keeps both ends of an oversize body, because the head says what it is and the tail says how it
// ended — and an error message lives at one end or the other, never in the middle.
func shorten(full string, snip tool.Snip) string {
	head := snip.KeepHead
	tail := snip.KeepTail
	if head <= 0 || tail <= 0 || head+tail >= MaxVisibleBytes {
		// The default split is the two thirds a truncated body usually wants: enough context to follow,
		// enough ending to know how it finished.
		head = MaxVisibleBytes * 2 / 3
		tail = MaxVisibleBytes - head
	}
	marker := fmt.Sprintf("\n… %d bytes not shown (use the full body if you need it) …\n", len(full)-head-tail)
	head, tail = clipHead(full, head), clipTail(full, tail)
	return full[:head] + marker + full[len(full)-tail:]
}

// clipHead backs a byte count off a rune boundary, so a cut never splits a character in half.
func clipHead(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return n
}

// clipTail is clipHead from the other end.
func clipTail(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	start := len(s) - n
	for start < len(s) && s[start]&0xC0 == 0x80 {
		start++
	}
	return len(s) - start
}
