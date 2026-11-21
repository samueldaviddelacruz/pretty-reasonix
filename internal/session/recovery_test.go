package session_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// logWith writes a session whose log is `lines`, verbatim, so a test can damage it precisely.
func logWith(t *testing.T, lines string) session.Layout {
	t.Helper()
	layout := session.NewLayout(t.TempDir(), "sess-dmg")
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := lines
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(layout.Events, []byte(body), session.ModeTranscript); err != nil {
		t.Fatalf("write: %v", err)
	}
	return layout
}

// healthy builds a valid log for two messages.
func healthy(t *testing.T) session.Layout {
	t.Helper()
	return logWith(t, strings.Join([]string{
		`{"schema_version":1,"type":"append","revision":1,"base_revision":0,"message_index":0,"messages":[{"ID":"1","Role":"user","Content":"first"}],"content_digest":"a"}`,
		`{"schema_version":1,"type":"append","revision":2,"base_revision":1,"message_index":1,"messages":[{"ID":"2","Role":"assistant","Content":"second"}],"content_digest":"b"}`,
	}, "\n"))
}

// TestAHealthyLogIsReadAndNothingIsWritten: the ordinary case must not touch the file at all.
func TestAHealthyLogIsReadAndNothingIsWritten(t *testing.T) {
	t.Parallel()
	layout := healthy(t)
	before, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	recovered, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered.Messages) != 2 {
		t.Fatalf("recovered %d messages, want 2", len(recovered.Messages))
	}
	if recovered.Dropped != 0 || recovered.Notice() != "" {
		t.Errorf("a healthy session reported %d dropped bytes and the notice %q", recovered.Dropped, recovered.Notice())
	}
	after, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a healthy log was rewritten")
	}
}

// TestATornTailIsSalvaged is the first clause: the valid prefix survives, the file is truncated to the last good
// boundary, and the discarded bytes are moved rather than deleted.
func TestATornTailIsSalvaged(t *testing.T) {
	t.Parallel()
	good := healthy(t)
	body, err := os.ReadFile(good.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	layout := logWith(t, string(body)+`{"schema_version":1,"type":"append","revision":3,"mess`)

	recovered, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered.Messages) != 2 {
		t.Fatalf("recovered %d messages, want the two before the tear", len(recovered.Messages))
	}
	if recovered.Dropped == 0 {
		t.Fatal("nothing was reported as dropped")
	}
	if !strings.HasSuffix(recovered.Damaged, ".damaged") {
		t.Errorf("the discarded bytes went to %q", recovered.Damaged)
	}

	// The bytes are beside the log, not gone.
	saved, err := os.ReadFile(recovered.Damaged)
	if err != nil {
		t.Fatalf("reading the damaged file: %v", err)
	}
	if !strings.Contains(string(saved), `"revision":3`) {
		t.Errorf("the damaged file does not hold the discarded bytes: %q", saved)
	}
	// And the log is truncated to the boundary, so the next append continues the chain.
	after, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(after) >= len(body)+10 {
		t.Errorf("the log was not truncated: %d bytes", len(after))
	}
	if _, err := session.ReadTranscript(layout.Events, layout.Canonical); err != nil {
		t.Errorf("the truncated log no longer replays: %v", err)
	}
	if recovered.Notice() == "" {
		t.Error("a salvage produced no notice")
	}
}

// TestAGapIsTheBoundary too: a record that does not continue the chain is damage, not a record to skip.
func TestAGapIsTheBoundary(t *testing.T) {
	t.Parallel()
	layout := logWith(t, strings.Join([]string{
		`{"schema_version":1,"type":"append","revision":1,"base_revision":0,"message_index":0,"messages":[{"ID":"1","Role":"user","Content":"first"}],"content_digest":"a"}`,
		`{"schema_version":1,"type":"append","revision":2,"base_revision":1,"message_index":5,"messages":[{"ID":"2","Role":"assistant","Content":"out of order"}],"content_digest":"b"}`,
	}, "\n"))

	recovered, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered.Messages) != 1 {
		t.Fatalf("recovered %d messages, want only the one before the gap", len(recovered.Messages))
	}
	if recovered.Dropped == 0 {
		t.Error("a gap was not reported")
	}
}

// TestANewerLogIsAHardError is the second clause, and the distinction it draws is the ticket: an unknown version is
// not damage, so nothing is salvaged and nothing is written.
func TestANewerLogIsAHardError(t *testing.T) {
	t.Parallel()
	layout := logWith(t, strings.Join([]string{
		`{"schema_version":1,"type":"append","revision":1,"base_revision":0,"message_index":0,"messages":[{"ID":"1","Role":"user","Content":"readable"}],"content_digest":"a"}`,
		`{"schema_version":99,"type":"append","revision":2,"base_revision":1,"message_index":1,"messages":[{"ID":"2","Role":"user","Content":"from the future"}],"content_digest":"b"}`,
	}, "\n"))
	before, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	_, err = session.Recover(layout)
	if !errors.Is(err, session.ErrNewerLog) {
		t.Fatalf("a newer log returned %v, want ErrNewerLog", err)
	}
	// The file is intact and newer; salvaging or truncating it would be the corruption.
	after, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a newer log was modified")
	}
	if _, err := os.Stat(layout.Events + ".damaged"); err == nil {
		t.Error("a newer log was treated as damaged")
	}
}

// TestAnUnknownRecordTypeIsAlsoHard, for the same reason: a newer writer owns the file.
func TestAnUnknownRecordTypeIsAlsoHard(t *testing.T) {
	t.Parallel()
	layout := logWith(t, `{"schema_version":1,"type":"fork","revision":1,"messages":[]}`)

	if _, err := session.Recover(layout); !errors.Is(err, session.ErrUnknownRecord) {
		t.Fatalf("an unknown record type returned %v, want ErrUnknownRecord", err)
	}
}

// TestASecondSalvageKeepsTheFirst: two salvages of one session must not destroy the evidence somebody may already be
// looking at.
func TestASecondSalvageKeepsTheFirst(t *testing.T) {
	t.Parallel()
	good := healthy(t)
	body, err := os.ReadFile(good.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	layout := logWith(t, string(body)+`{"torn`)

	first, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Damage it again, the way a second crash would.
	if writeErr := os.WriteFile(layout.Events, append(body, []byte(`{"torn again`)...), session.ModeTranscript); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}
	second, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Damaged == second.Damaged {
		t.Fatalf("the second salvage overwrote the first: %q and %q", first.Damaged, second.Damaged)
	}
	if !fileExists(first.Damaged) || !fileExists(second.Damaged) {
		t.Error("a damaged file went missing")
	}
}

// TestAnOversizedLogIsRefusedBeforeReading is the preflight: the size decides whether this process is willing to hold
// the file, and it must decide before the read rather than after.
func TestAnOversizedLogIsRefusedBeforeReading(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-big")
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.Truncate(layout.Events, session.MaxLogBytes+1); err != nil {
		// Truncating a sparse file is cheap, and the content does not matter: the size is the subject.
		t.Fatalf("truncate: %v", err)
	}
	if _, err := session.Recover(layout); !errors.Is(err, session.ErrLogTooLarge) {
		t.Fatalf("an oversized log returned %v, want ErrLogTooLarge", err)
	}
}

// TestAMissingLogIsAnEmptySession: a session created but never spoken in is ordinary.
func TestAMissingLogIsAnEmptySession(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-none")
	recovered, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered.Messages) != 0 || recovered.Dropped != 0 {
		t.Errorf("a missing log produced %+v", recovered)
	}
}

// TestAHostileRecordIsNotTrusted: a replace record claims the whole transcript, so its content must be messages and
// nothing else — the chain check applies to appends, and a replace is the only other way the prefix can change.
func TestAReplaceSupersedesThePrefix(t *testing.T) {
	t.Parallel()
	layout := logWith(t, strings.Join([]string{
		`{"schema_version":1,"type":"append","revision":1,"base_revision":0,"message_index":0,"messages":[{"ID":"1","Role":"user","Content":"before"}],"content_digest":"a"}`,
		`{"schema_version":1,"type":"replace","revision":2,"base_revision":1,"message_index":0,"messages":[{"ID":"9","Role":"user","Content":"a digest of the above"}],"reason":"compaction"}`,
	}, "\n"))

	recovered, err := session.Recover(layout)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered.Messages) != 1 || recovered.Messages[0].Content != "a digest of the above" {
		t.Fatalf("recovered %+v, want only the replacement", recovered.Messages)
	}
	if recovered.Messages[0].Role != provider.RoleUser {
		t.Errorf("the replacement lost its role: %v", recovered.Messages[0].Role)
	}
}

// fileExists keeps the assertion above readable.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
