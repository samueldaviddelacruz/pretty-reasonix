package tool_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/tool"
	registry "pretty-reasonix/internal/tool"
)

func body(n int, fill string) string { return strings.Repeat(fill, n) }

// TestASmallResultIsItsOwnBody: bounding must be free when there is nothing to bound, and must not copy a
// string it is going to return unchanged.
func TestASmallResultIsItsOwnBody(t *testing.T) {
	t.Parallel()
	bound, err := registry.Boundary{}.Bound(tool.Result{Output: "module pretty-reasonix"})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if bound.Visible != "module pretty-reasonix" || bound.Truncated {
		t.Errorf("a short result was altered: %+v", bound)
	}
	if bound.Body != nil || bound.SpillPath != "" {
		t.Errorf("a short result kept a second copy: %+v", bound)
	}
}

// TestAnOversizeResultKeepsBothEnds is the criterion's first clause: bounded once, at ≤ 32 KiB visible.
func TestAnOversizeResultKeepsBothEnds(t *testing.T) {
	t.Parallel()
	full := "HEAD-" + body(registry.MaxVisibleBytes, "x") + "-TAIL"
	bound, err := registry.Boundary{}.Bound(tool.Result{Output: full})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if !bound.Truncated {
		t.Error("an oversize result was not marked truncated")
	}
	if len(bound.Visible) > registry.MaxVisibleBytes+200 {
		t.Errorf("visible is %d bytes, above the budget", len(bound.Visible))
	}
	// Both ends, because an error message lives at one end or the other and never in the middle.
	if !strings.HasPrefix(bound.Visible, "HEAD-") {
		t.Error("the visible copy lost the start of the output")
	}
	if !strings.HasSuffix(bound.Visible, "-TAIL") {
		t.Error("the visible copy lost the end of the output")
	}
	if !strings.Contains(bound.Visible, "bytes not shown") {
		t.Error("the visible copy does not say that something was left out")
	}
	if bound.Body == nil || *bound.Body != full {
		t.Error("the full body was not kept, and it was small enough to keep")
	}
	if bound.OriginalBytes != len(full) {
		t.Errorf("OriginalBytes is %d, want %d", bound.OriginalBytes, len(full))
	}
}

// TestAnIdenticalBodyIsNotKeptTwice: the criterion's "only when it differs". A body equal to what is
// visible is a duplicate, and a duplicate is the thing a transcript cannot afford.
func TestAnIdenticalBodyIsNotKeptTwice(t *testing.T) {
	t.Parallel()
	// Exactly at the budget: nothing is cut, so nothing differs, so nothing is kept.
	exact := body(registry.MaxVisibleBytes, "y")
	bound, err := registry.Boundary{}.Bound(tool.Result{Output: exact})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if bound.Truncated || bound.Body != nil || bound.Visible != exact {
		t.Errorf("a result at exactly the budget was treated as oversize: %+v", bound)
	}
}

// TestAHugeResultSpillsToDisk is the criterion's second clause, and the spill is named after its content.
func TestAHugeResultSpillsToDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	full := body(registry.SpillAbove+1024, "z")

	bound, err := registry.Boundary{Dir: dir}.Bound(tool.Result{Output: full})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if bound.SpillPath == "" {
		t.Fatal("an oversize result was neither kept nor spilled")
	}
	if bound.Body != nil {
		t.Error("a spilled result was also kept in memory, which defeats the spill")
	}
	if len(bound.Visible) > registry.MaxVisibleBytes+200 {
		t.Errorf("visible is %d bytes while spilled", len(bound.Visible))
	}
	onDisk, err := os.ReadFile(bound.SpillPath)
	if err != nil {
		t.Fatalf("reading the spill: %v", err)
	}
	if string(onDisk) != full {
		t.Error("the spill does not hold the whole body")
	}
	info, err := os.Stat(bound.SpillPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the spill is %v, want owner-only: tool output can be anything the tool read", info.Mode().Perm())
	}
}

// TestASpillIsNeverRewritten is E5-T3's second criterion, and it is the reason the file name is derived
// from the content: the same body spilled twice is the same file, and the second attempt leaves the first
// alone rather than replacing it.
func TestASpillIsNeverRewritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	full := body(registry.SpillAbove+2048, "q")

	first, err := registry.Boundary{Dir: dir}.Bound(tool.Result{Output: full})
	if err != nil {
		t.Fatalf("first Bound: %v", err)
	}
	// Backdate the file: if a second pass rewrote it, the modification time would move forward.
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if chtimesErr := os.Chtimes(first.SpillPath, old, old); chtimesErr != nil {
		t.Fatalf("chtimes: %v", chtimesErr)
	}
	before, err := os.ReadFile(first.SpillPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	second, err := registry.Boundary{Dir: dir}.Bound(tool.Result{Output: full})
	if err != nil {
		t.Fatalf("second Bound: %v", err)
	}
	if second.SpillPath != first.SpillPath {
		t.Errorf("the same body spilled to two paths: %q and %q", first.SpillPath, second.SpillPath)
	}
	after, err := os.ReadFile(second.SpillPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) {
		t.Error("the stored body changed")
	}
	info, err := os.Stat(second.SpillPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(old) {
		t.Error("the spill file was rewritten: its modification time moved")
	}
}

// TestASpillWithNowhereToGoIsAnError: silently truncating because a directory was missing would hide the
// whole body without saying so.
func TestASpillWithNowhereToGoIsAnError(t *testing.T) {
	t.Parallel()
	_, err := registry.Boundary{}.Bound(tool.Result{Output: body(registry.SpillAbove+1, "w")})
	if err == nil {
		t.Fatal("a spill with no directory was accepted")
	}
}

// TestAToolsSnipGeometryIsHonoured: a tool that knows its own output decides where the cut goes.
func TestAToolsSnipGeometryIsHonoured(t *testing.T) {
	t.Parallel()
	full := "FIRST-" + body(registry.MaxVisibleBytes, "m") + "-LAST"
	bound, err := registry.Boundary{
		Snip: tool.Snip{KeepHead: 6, KeepTail: 5},
	}.Bound(tool.Result{Output: full})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if !strings.HasPrefix(bound.Visible, "FIRST-") || !strings.HasSuffix(bound.Visible, "-LAST") {
		t.Errorf("the declared geometry was not honoured: %q…%q",
			bound.Visible[:min(20, len(bound.Visible))], bound.Visible[max(0, len(bound.Visible)-20):])
	}
	if len(bound.Visible) > 200 {
		t.Errorf("a small declared geometry produced %d bytes of visible output", len(bound.Visible))
	}
}

// TestTheSpillDirectoryIsCreatedWhenMissing, because a session directory is made when the session starts
// and a tool should not have to care about the ordering.
func TestTheSpillDirectoryIsCreatedWhenMissing(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "session")
	bound, err := registry.Boundary{Dir: dir}.Bound(tool.Result{Output: body(registry.SpillAbove+1, "n")})
	if err != nil {
		t.Fatalf("Bound: %v", err)
	}
	if _, err := os.Stat(bound.SpillPath); err != nil {
		t.Errorf("the spill directory was not created: %v", err)
	}
}
