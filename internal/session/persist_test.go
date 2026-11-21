package session_test

import (
	"os"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// TestAPersistentTranscriptReachesTheLog is E9-T10's first clause: what the controller appended is there afterwards,
// readable by a process that knows nothing about the run that wrote it.
func TestAPersistentTranscriptReachesTheLog(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-persist")

	transcript, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	for _, text := range []string{"what does go.mod declare", "it is pretty-reasonix"} {
		transcript.Append(session.Message{Role: provider.RoleUser, Content: text})
	}
	if writeErr := transcript.WriteError(); writeErr != nil {
		t.Fatalf("a message failed to persist: %v", writeErr)
	}
	if closeErr := transcript.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	// A second reader, as a later process would.
	messages, err := session.ReadTranscript(layout.Events, layout.Canonical)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(messages) != 2 || messages[0].Content != "what does go.mod declare" {
		t.Fatalf("the log holds %+v", messages)
	}
}

// TestTheInMemoryCopySurvivesAWriteFailure is the second clause: persistence is a safety net, not a gate. The answer
// the turn produced is still there when the disk is not cooperating.
func TestTheInMemoryCopySurvivesAWriteFailure(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-fail")
	transcript, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	transcript.Append(session.Message{Role: provider.RoleUser, Content: "before the disk went away"})
	if err := transcript.WriteError(); err != nil {
		t.Fatalf("the first message did not persist: %v", err)
	}

	// The log is closed underneath the transcript, which is what a revoked descriptor or a vanished mount looks like
	// from here. A permission change would not do it: the file is already open, and an open descriptor ignores the
	// mode — the first version of this test made that mistake and passed a write that should have failed.
	if err := transcript.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	transcript.Append(session.Message{Role: provider.RoleAssistant, Content: "the answer worth keeping"})

	if transcript.WriteError() == nil {
		t.Fatal("a failed write was not reported")
	}
	// And the conversation is still whole in memory: the answer the turn produced is not thrown away over a disk.
	all := transcript.All()
	if len(all) != 2 || all[1].Content != "the answer worth keeping" {
		t.Fatalf("the in-memory transcript lost a message: %+v", all)
	}
}

// TestLoadPersistentRestoresWhatWasSaid, and salvages what cannot be read, so a resumed session knows about both.
func TestLoadPersistentRestoresWhatWasSaid(t *testing.T) {
	t.Parallel()
	layout := session.NewLayout(t.TempDir(), "sess-resume")
	first, err := session.NewPersistent(layout)
	if err != nil {
		t.Fatalf("NewPersistent: %v", err)
	}
	first.Append(session.Message{Role: provider.RoleUser, Content: "the original question"})
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	// A crash leaves a partial line at the end.
	body, err := os.ReadFile(layout.Events)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if writeErr := os.WriteFile(layout.Events, append(body, []byte(`{"type":"app`)...), session.ModeTranscript); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	resumed, recovered, err := session.LoadPersistent(layout)
	if err != nil {
		t.Fatalf("LoadPersistent: %v", err)
	}
	defer func() { _ = resumed.Close() }()

	if len(resumed.All()) != 1 || resumed.All()[0].Content != "the original question" {
		t.Fatalf("the resumed transcript holds %+v", resumed.All())
	}
	if recovered.Dropped == 0 {
		t.Error("the torn tail was not reported to the caller that resumed")
	}
	if !strings.Contains(recovered.Notice(), "unreadable") {
		t.Errorf("the notice is %q", recovered.Notice())
	}
}
