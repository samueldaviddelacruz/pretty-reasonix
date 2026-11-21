// White-box: the reducer, its rows and the settled rule are unexported.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"errors"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
)

// apply feeds a sequence to a fresh reducer and returns the rows.
func applyAll(events ...event.Event) []Row {
	reducer := NewReducer()
	for _, e := range events {
		reducer.Apply(e)
	}
	return reducer.Rows()
}

// TestDeltasFoldIntoOneGrowingRow: a burst of deltas is one answer, not fifty rows.
func TestDeltasFoldIntoOneGrowingRow(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.TurnStarted{TurnID: "t1", Input: "what is it"},
		event.TextDelta{TurnID: "t1", ItemID: "i1", Text: "it is "},
		event.TextDelta{TurnID: "t1", ItemID: "i1", Text: "a module"},
	)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the question and one answer", len(rows))
	}
	if rows[0].Kind != RowUser || rows[0].Text != "what is it" || !rows[0].Settled {
		t.Errorf("the question row is %+v", rows[0])
	}
	if rows[1].Kind != RowSay || rows[1].Text != "it is a module" {
		t.Errorf("the answer row is %+v", rows[1])
	}
	if rows[1].Settled {
		t.Error("a streaming answer is already settled")
	}
}

// TestCommittedWinsOverDeltas is the criterion's sharpest clause: if a delta was dropped, the committed text is the
// transcript, and anything else would leave a permanent gap in a settled answer.
func TestCommittedWinsOverDeltas(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.TurnStarted{TurnID: "t1", Input: "q"},
		event.TextDelta{TurnID: "t1", ItemID: "i1", Text: "a partial answer"},
		// The committed text is the truth, including the part a dropped delta never delivered.
		event.MessageCommitted{TurnID: "t1", ItemID: "i1", Message: provider.Message{
			Role: provider.RoleAssistant, Content: "the whole answer, restored",
		}},
	)
	answer := rows[len(rows)-1]
	if answer.Text != "the whole answer, restored" {
		t.Errorf("the committed message did not win: %q", answer.Text)
	}
	if !answer.Settled {
		t.Error("a committed message left the row unsettled")
	}
}

// TestAnEmptyCommittedMessageDoesNotErase: the same "empty never erases" rule the criterion states for tools applies
// to a role that carries no prose, which is what a tool-only round produces.
func TestAnEmptyCommittedMessageDoesNotErase(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.TextDelta{TurnID: "t1", ItemID: "i1", Text: "streamed text"},
		event.MessageCommitted{TurnID: "t1", ItemID: "i1", Message: provider.Message{
			Role: provider.RoleAssistant, Content: "",
		}},
	)
	if rows[0].Text != "streamed text" {
		t.Errorf("an empty commit erased the deltas: %q", rows[0].Text)
	}
	if !rows[0].Settled {
		t.Error("the row was not settled by the commit")
	}
}

// TestToolEventsMergeByCallID is the criterion's tool clause: dispatch, progress and result are one row.
func TestToolEventsMergeByCallID(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.ToolCallStarted{TurnID: "t1", CallID: "c1", Name: "read_file"},
		event.ToolProgress{CallID: "c1", Phase: "reading", Preview: "go.mod"},
		event.ToolResult{CallID: "c1", Output: "module pretty-reasonix"},
	)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want one row for one call: %+v", len(rows), rows)
	}
	if rows[0].Kind != RowTool || rows[0].Tool != "read_file" {
		t.Errorf("the row lost its identity: %+v", rows[0])
	}
	if rows[0].Text != "module pretty-reasonix" {
		t.Errorf("the result did not land in the row: %q", rows[0].Text)
	}
	if !rows[0].Settled || !rows[0].Foldable {
		t.Error("a finished tool row should be settled and foldable")
	}
}

// TestAnEmptyFieldNeverErases is stated in the criterion and is easy to get wrong in either direction.
func TestAnEmptyFieldNeverErases(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.ToolCallStarted{TurnID: "t1", CallID: "c1", Name: "grep"},
		event.ToolProgress{CallID: "c1", Phase: "searching", Preview: "three matches"},
		// A progress event with no preview and no phase: it says nothing, so it must change nothing.
		event.ToolProgress{CallID: "c1"},
	)
	if rows[0].Text != "three matches" {
		t.Errorf("an empty preview erased the previous one: %q", rows[0].Text)
	}
	if rows[0].Phase != "searching" {
		t.Errorf("an empty phase erased the previous one: %q", rows[0].Phase)
	}

	// And a result that reports nothing must not erase what the dispatch said either.
	rows = applyAll(
		event.ToolDispatched{TurnID: "t1", CallID: "c2", Name: "ls", Args: []byte(`{"path":"."}`)},
		event.ToolResult{CallID: "c2"},
	)
	if !strings.Contains(rows[0].Text, `"path":"."`) {
		t.Errorf("an empty result erased the dispatch arguments: %q", rows[0].Text)
	}
}

// TestAToolErrorBecomesAnErrorRow, because a failed tool and a successful one read differently.
func TestAToolErrorBecomesAnErrorRow(t *testing.T) {
	t.Parallel()
	rows := applyAll(event.ToolResult{CallID: "c1", Err: errNoFile})
	if rows[0].Kind != RowError {
		t.Errorf("a failed tool produced a %s row", rows[0].Kind)
	}
	if !strings.Contains(rows[0].Text, "no such file") {
		t.Errorf("the row does not carry the reason: %q", rows[0].Text)
	}
}

// TestTurnDoneSettlesEverything, which is what lets a frontend print the transcript and forget it.
func TestTurnDoneSettlesEverything(t *testing.T) {
	t.Parallel()
	rows := applyAll(
		event.TextDelta{TurnID: "t1", ItemID: "i1", Text: "an answer"},
		event.UsageReported{TurnID: "t1", Usage: provider.Usage{InputTokens: 10, OutputTokens: 2}},
		event.TurnDone{TurnID: "t1", Status: event.StatusDone},
	)
	for _, row := range rows {
		if !row.Settled {
			t.Errorf("%s row is still unsettled after the turn ended", row.Kind)
		}
	}
}

// TestFencedAnswerSettlesOnlyOutsideTheFence is the criterion's second clause, and the reason it exists: inline mode
// cannot repaint, so a split inside a code block would print half a listing and then render the rest where it can never
// be corrected.
func TestFencedAnswerSettlesOnlyOutsideTheFence(t *testing.T) {
	t.Parallel()
	answer := strings.Join([]string{
		"Here is the file:",
		"",
		"```go",
		"package main",
		"",
		"func main() {}",
		"```",
		"",
		"and that is all.",
		"",
		"one more paragraph is still arriving",
	}, "\n")

	settled, live := settledPrefix(answer)

	// The last blank line outside a fence is after "and that is all."
	if !strings.Contains(settled, "and that is all.") {
		t.Errorf("the settled prefix stops too early:\n%q", settled)
	}
	if !strings.Contains(live, "one more paragraph") {
		t.Errorf("the live part is wrong:\n%q", live)
	}
	// The critical property: the boundary is never inside the block.
	if strings.Count(settled, "```")%2 != 0 {
		t.Errorf("the settled prefix ends inside a fenced block:\n%q", settled)
	}
	if !strings.HasSuffix(settled, "\n\n") {
		t.Errorf("the settled prefix does not end at a blank line: %q", settled[len(settled)-3:])
	}
}

// TestABlankLineInsideAFenceIsNotABoundary, stated on its own because it is the whole of the fence clause.
func TestABlankLineInsideAFenceIsNotABoundary(t *testing.T) {
	t.Parallel()
	answer := "intro\n\n```\nfirst\n\nsecond\n```\n"
	settled, live := settledPrefix(answer)

	if settled != "intro\n\n" {
		t.Errorf("settled = %q, want only the part before the fence", settled)
	}
	if !strings.HasPrefix(live, "```") {
		t.Errorf("live = %q, want the open fence", live)
	}
}

// TestTheSettledPrefixOfAFinishedAnswerIsAllOfIt, so a caller can treat the two halves uniformly.
func TestTheSettledPrefixOfAFinishedAnswerIsAllOfIt(t *testing.T) {
	t.Parallel()
	settled, live := settledPrefix("done.\n\n")
	if live != "" {
		t.Errorf("a finished answer left %q live", live)
	}
	if settled != "done.\n\n" {
		t.Errorf("settled = %q", settled)
	}
}

// TestReasoningIsFoldableAndFoldedByDefault: reasoning is context, not the answer, and it should not push the answer
// off the screen until somebody asks for it.
func TestReasoningIsFoldableAndFoldedByDefault(t *testing.T) {
	t.Parallel()
	rows := applyAll(event.ReasoningDelta{TurnID: "t1", ItemID: "r1", Text: "thinking about it"})
	if !rows[0].Foldable || !rows[0].Folded {
		t.Errorf("reasoning row is %+v, want foldable and folded", rows[0])
	}
}

// TestRowsAreACopy, because a caller holding the slice must not be able to reach into the transcript.
func TestRowsAreACopy(t *testing.T) {
	t.Parallel()
	reducer := NewReducer()
	reducer.Apply(event.TurnStarted{TurnID: "t1", Input: "q"})

	rows := reducer.Rows()
	rows[0].Text = "tampered"

	if reducer.Rows()[0].Text != "q" {
		t.Error("Rows returned the reducer's own slice")
	}
}

// errNoFile is the failure the tool-error case needs, named so the assertion can match it.
var errNoFile = errors.New("nope.txt: no such file or directory")
