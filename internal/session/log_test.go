package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// paths gives a pair of file paths in a temporary directory.
func paths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "events.jsonl"), filepath.Join(dir, "transcript.jsonl")
}

// TestAppendThenReplay is the round trip, and the only thing worth asserting first: what went in comes out.
func TestAppendThenReplay(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	log, createErr := session.CreateLog(events, canonical)
	if createErr != nil {
		t.Fatalf("CreateLog: %v", createErr)
	}
	for _, text := range []string{"hello", "a second thing"} {
		if err := log.Append(session.Message{Role: provider.RoleUser, Content: text}); err != nil {
			t.Fatalf("Append %q: %v", text, err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	messages, err := session.ReadTranscript(events, canonical)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "a second thing" {
		t.Fatalf("replayed %+v, want the two messages in order", messages)
	}
}

// TestTheChainCheckRefusesAGap is the property that makes a log different from a list of lines.
func TestTheChainCheckRefusesAGap(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	log, createErr := session.CreateLog(events, canonical)
	if createErr != nil {
		t.Fatalf("CreateLog: %v", createErr)
	}
	if err := log.Append(session.Message{Role: provider.RoleUser, Content: "first"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Rewrite the log with the middle index removed, which is what a lost record looks like.
	body, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	second := strings.Replace(string(body), `"message_index":0`, `"message_index":2`, 1)
	if err := os.WriteFile(events, []byte(second), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := session.ReadTranscript(events, canonical); !errors.Is(err, session.ErrChainBroken) {
		t.Fatalf("a gap replayed without complaint: %v", err)
	}
}

// TestEveryMessageIsFlushed: fsync per record is the point, so the assertion is that the bytes are on disk before
// Close. Reading while the handle is open is the only way to see that from outside.
func TestEveryMessageIsFlushed(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	log, createErr := session.CreateLog(events, canonical)
	if createErr != nil {
		t.Fatalf("CreateLog: %v", createErr)
	}
	defer func() { _ = log.Close() }()

	if err := log.Append(session.Message{Role: provider.RoleUser, Content: "written"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	body, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "written") {
		t.Fatalf("the record was not on disk before Close: %q", body)
	}
}

// TestTheConversationIsOwnerOnly: a transcript holds what a person typed, so nobody else may read it.
func TestTheConversationIsOwnerOnly(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	log, createErr := session.CreateLog(events, canonical)
	if createErr != nil {
		t.Fatalf("CreateLog: %v", createErr)
	}
	if err := log.Append(session.Message{Role: provider.RoleUser, Content: "secret"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, path := range []string{events, canonical} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s is %04o, want 0600", path, mode)
		}
	}
}

// TestTheEventLogIsPreferred: the canonical file is a view, so a replacement it cannot express must still win.
func TestTheEventLogIsPreferred(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	log, createErr := session.CreateLog(events, canonical)
	if createErr != nil {
		t.Fatalf("CreateLog: %v", createErr)
	}
	if err := log.Append(session.Message{Role: provider.RoleUser, Content: "before compaction"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Replace([]session.Message{{Role: provider.RoleUser, Content: "a digest of the above"}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	messages, err := session.ReadTranscript(events, canonical)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "a digest of the above" {
		t.Fatalf("replayed %+v; the canonical view won over the record", messages)
	}

	// And the canonical file genuinely is only a view, so it still holds the pre-compaction line.
	body, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	if !strings.Contains(string(body), "before compaction") {
		t.Errorf("the canonical view lost the append it cannot replace: %q", body)
	}
}

// TestTheLegacyCanonicalFileIsReadable on its own, for a transcript written before the event log existed.
func TestTheLegacyCanonicalFileIsReadable(t *testing.T) {
	t.Parallel()
	events, canonical := paths(t)
	line := `{"role":"user","content":"from the old format"}` + "\n"
	if err := os.WriteFile(canonical, []byte(line), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	messages, err := session.ReadTranscript(events, canonical)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "from the old format" {
		t.Fatalf("replayed %+v", messages)
	}
}
