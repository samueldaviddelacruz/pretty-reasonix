// The printer is unexported and its rows are the unit under test. Testing it through Run would need a
// configured provider and a real turn, which is a different test at a different level.
//
//nolint:testpackage // white-box on purpose: the rows are the subject
package run

import (
	"bytes"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
)

// render feeds events to a printer and returns what each stream received.
func render(events ...event.Event) (string, string, bool) {
	var stdout, stderr bytes.Buffer
	p := &printer{stdout: &stdout, stderr: &stderr}
	for _, e := range events {
		p.Emit(e)
	}
	return stdout.String(), stderr.String(), p.failed
}

// TestDeltasAreWrittenAsTheyArrive is E14-T2's first clause. The sequence matters: three deltas must produce one
// line, not three.
func TestDeltasAreWrittenAsTheyArrive(t *testing.T) {
	t.Parallel()
	stdout, stderr, failed := render(
		event.TextDelta{TurnID: "t", ItemID: "i", Text: "the module "},
		event.TextDelta{TurnID: "t", ItemID: "i", Text: "is "},
		event.TextDelta{TurnID: "t", ItemID: "i", Text: "pretty-reasonix"},
		event.TurnDone{TurnID: "t", Status: event.StatusDone},
	)
	if stdout != "the module is pretty-reasonix\n" {
		t.Errorf("stdout is %q, want the deltas joined on one line", stdout)
	}
	if stderr != "" {
		t.Errorf("a clean run wrote %q to stderr", stderr)
	}
	if failed {
		t.Error("a clean run reported failure")
	}
}

// TestTheCommittedAnswerIsNotRepeated: a frontend that streamed a token and then printed the whole message would
// say everything twice.
func TestTheCommittedAnswerIsNotRepeated(t *testing.T) {
	t.Parallel()
	stdout, _, _ := render(
		event.TextDelta{TurnID: "t", ItemID: "i", Text: "answer"},
		event.MessageCommitted{TurnID: "t", ItemID: "i", Message: provider.Message{
			Role: provider.RoleAssistant, Content: "answer",
		}},
		event.TurnDone{TurnID: "t", Status: event.StatusDone},
	)
	if strings.Count(stdout, "answer") != 1 {
		t.Errorf("the answer appears %d times:\n%s", strings.Count(stdout, "answer"), stdout)
	}
}

// TestAnUnstreamedAnswerIsStillPrinted, which is what happens for a turn whose text arrived in one chunk.
func TestAnUnstreamedAnswerIsStillPrinted(t *testing.T) {
	t.Parallel()
	stdout, _, _ := render(
		event.MessageCommitted{TurnID: "t", ItemID: "i", Message: provider.Message{
			Role: provider.RoleAssistant, Content: "a whole answer",
		}},
	)
	if !strings.Contains(stdout, "a whole answer") {
		t.Errorf("stdout is %q, want the answer", stdout)
	}
}

// TestInformationalNoticesGoToStdout is the second clause: stderr is for what a person must act on, and an
// informational notice on the error stream makes `prx run > answer.txt` produce an empty file.
func TestInformationalNoticesGoToStdout(t *testing.T) {
	t.Parallel()
	stdout, stderr, failed := render(
		event.Notice{Level: event.NoticeInfo, Code: "turn.recovered", Text: "turn-0001"},
		event.TurnDone{TurnID: "t", Status: event.StatusDone},
	)
	if strings.Contains(stderr, "turn.recovered") {
		t.Errorf("an informational notice went to stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "turn.recovered") {
		t.Errorf("stdout is %q, want the notice as a row", stdout)
	}
	if failed {
		t.Error("an informational notice was treated as a failure")
	}
}

// TestAWarningGoesToStderrAndFailsTheTurn: the notices a person must act on are the ones that belong there.
func TestAWarningGoesToStderrAndFailsTheTurn(t *testing.T) {
	t.Parallel()
	stdout, stderr, failed := render(
		event.Notice{Level: event.NoticeError, Code: "turn.failed", Text: "the vendor said no"},
	)
	if !strings.Contains(stderr, "the vendor said no") {
		t.Errorf("stderr is %q, want the error", stderr)
	}
	if strings.Contains(stdout, "the vendor said no") {
		t.Errorf("an error was printed as a row on stdout: %q", stdout)
	}
	if !failed {
		t.Error("an error notice did not fail the run")
	}
}

// TestAToolRowDoesNotRunIntoTheAnswer: a streamed line has no ending yet, so a row printed after it must start
// fresh rather than continue the sentence.
func TestAToolRowDoesNotRunIntoTheAnswer(t *testing.T) {
	t.Parallel()
	stdout, _, _ := render(
		event.TextDelta{TurnID: "t", ItemID: "i", Text: "let me look"},
		event.ToolResult{CallID: "c1", Output: "module pretty-reasonix"},
		event.TextDelta{TurnID: "t", ItemID: "j", Text: "done"},
		event.TurnDone{TurnID: "t", Status: event.StatusDone},
	)
	want := "let me look\n[tool] ok\ndone\n"
	if stdout != want {
		t.Errorf("stdout is %q, want %q", stdout, want)
	}
}
