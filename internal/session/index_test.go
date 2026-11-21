package session_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

// seeded creates a session with three messages and returns its layout and log.
func seeded(t *testing.T) (session.Layout, *session.Log) {
	t.Helper()
	layout := session.NewLayout(t.TempDir(), "sess-0002")
	if err := session.Create(layout); err != nil {
		t.Fatalf("Create: %v", err)
	}
	log, err := session.CreateLog(layout.Events, layout.Canonical)
	if err != nil {
		t.Fatalf("CreateLog: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	entries := []session.Message{
		{ID: "1", Role: provider.RoleUser, Content: "why is the sky blue\nand not green"},
		{ID: "2", Role: provider.RoleAssistant, Content: "rayleigh scattering"},
		{ID: "3", Role: provider.RoleUser, Content: "go on"},
	}
	for _, message := range entries {
		if err := log.Append(message); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	return layout, log
}

// TestTheIndexDescribesTheConversation is E9-T4's first clause: the fields a picker needs, without decoding anything.
func TestTheIndexDescribesTheConversation(t *testing.T) {
	t.Parallel()
	layout, _ := seeded(t)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	index, repaired, err := session.LoadOrRebuild(layout, "test-model", now)
	if err != nil {
		t.Fatalf("LoadOrRebuild: %v", err)
	}
	if !repaired {
		t.Error("a session with no index reported a hit")
	}
	if index.Turns != 2 {
		t.Errorf("turns is %d, want 2 — the person spoke twice", index.Turns)
	}
	if index.Messages != 3 {
		t.Errorf("messages is %d, want 3", index.Messages)
	}
	if index.Title != "why is the sky blue" {
		t.Errorf("title is %q, want the first line of what the person said", index.Title)
	}
	if index.Preview != "go on" {
		t.Errorf("preview is %q, want the last thing said", index.Preview)
	}
	if index.Model != "test-model" {
		t.Errorf("model is %q", index.Model)
	}
	if index.LogDigest == "" || index.LogBytes == 0 {
		t.Error("the index does not name the log it came from")
	}
	if !index.CreatedAt.Equal(now) || !index.UpdatedAt.Equal(now) {
		t.Errorf("timestamps are %v/%v, want the caller's clock", index.CreatedAt, index.UpdatedAt)
	}
}

// TestAFreshIndexIsTrusted: the second read is a hit, which is what makes the cache worth having.
func TestAFreshIndexIsTrusted(t *testing.T) {
	t.Parallel()
	layout, _ := seeded(t)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if _, _, err := session.LoadOrRebuild(layout, "m", now); err != nil {
		t.Fatalf("first: %v", err)
	}

	index, repaired, err := session.LoadOrRebuild(layout, "m", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if repaired {
		t.Error("a current index was rebuilt")
	}
	// The hit returns the stored index, so the timestamp is the one it was built with rather than this call's.
	if !index.UpdatedAt.Equal(now) {
		t.Errorf("the stored index was rewritten: %v", index.UpdatedAt)
	}
}

// TestAStaleIndexIsRepairedNotTrusted is E9-T4's second clause, and the direction matters: the log wins, always.
func TestAStaleIndexIsRepairedNotTrusted(t *testing.T) {
	t.Parallel()
	layout, log := seeded(t)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if _, _, err := session.LoadOrRebuild(layout, "m", now); err != nil {
		t.Fatalf("first: %v", err)
	}

	// One more message: the index still claims three, so it now describes a conversation that no longer exists.
	if err := log.Append(session.Message{ID: "4", Role: provider.RoleAssistant, Content: "shorter wavelengths scatter more"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	index, repaired, err := session.LoadOrRebuild(layout, "m", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("LoadOrRebuild: %v", err)
	}
	if !repaired {
		t.Fatal("a stale index was trusted over the log")
	}
	if index.Messages != 4 {
		t.Errorf("the repaired index says %d messages, want 4", index.Messages)
	}
	if index.Preview != "shorter wavelengths scatter more" {
		t.Errorf("the repaired preview is %q", index.Preview)
	}
}

// TestAnUnreadableIndexIsRepaired: a cache that cannot be parsed has no claim to make, and unlike a log it may be
// thrown away.
func TestAnUnreadableIndexIsRepaired(t *testing.T) {
	t.Parallel()
	layout, _ := seeded(t)
	if err := os.WriteFile(layout.Index, []byte("{not json"), session.ModeIndex); err != nil {
		t.Fatalf("write: %v", err)
	}

	index, repaired, err := session.LoadOrRebuild(layout, "m", time.Now())
	if err != nil {
		t.Fatalf("LoadOrRebuild: %v", err)
	}
	if !repaired || index.Messages != 3 {
		t.Fatalf("an unreadable index produced %+v (repaired=%v)", index, repaired)
	}
}

// TestAnIndexFromANewerWriterIsAlsoJustACache, which is where the index differs from the log: an unknown log version
// is a hard error, and an unknown index version is a rebuild.
func TestAnIndexFromANewerWriterIsAlsoJustACache(t *testing.T) {
	t.Parallel()
	layout, _ := seeded(t)
	future := session.Index{SchemaVersion: session.IndexSchemaVersion + 1, ID: "sess-0002"}
	if err := future.Write(layout.Index); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, repaired, err := session.LoadOrRebuild(layout, "m", time.Now()); err != nil || !repaired {
		t.Fatalf("a newer index was not rebuilt: repaired=%v err=%v", repaired, err)
	}
}

// TestAMissingIndexIsNotAnError: a session that has never been listed is ordinary.
func TestAMissingIndexIsNotAnError(t *testing.T) {
	t.Parallel()
	layout, _ := seeded(t)
	if err := os.Remove(layout.Index); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// LoadIndex first, because LoadOrRebuild writes a fresh one and the point of this test is the state before that.
	if _, err := session.LoadIndex(layout); !errors.Is(err, session.ErrIndexStale) {
		t.Errorf("LoadIndex on a missing file returned %v, want ErrIndexStale", err)
	}
	if _, repaired, err := session.LoadOrRebuild(layout, "m", time.Now()); err != nil || !repaired {
		t.Fatalf("a missing index was not rebuilt: repaired=%v err=%v", repaired, err)
	}
}

// TestAPreviewCutsOnARuneBoundary: half a character in a picker is a rendering bug with an obvious cause.
func TestAPreviewCutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	long := "«" + strings.Repeat("é", session.PreviewLength)
	index := session.Build("id", "m", []session.Message{{Role: provider.RoleUser, Content: long}},
		"digest", 1, time.Now())
	if !strings.HasSuffix(index.Preview, "…") {
		t.Fatalf("a long preview was not cut: %q", index.Preview)
	}
	if strings.ContainsRune(index.Preview, '\uFFFD') {
		t.Errorf("the preview cut mid-rune: %q", index.Preview)
	}
}

// TestHostAuthoredEntriesDoNotNameTheSession, because a policy block would title every session identically.
func TestHostAuthoredEntriesDoNotNameTheSession(t *testing.T) {
	t.Parallel()
	messages := []session.Message{
		{Role: provider.RoleUser, Content: "policy block", HostAuthored: true},
		{Role: provider.RoleUser, Content: "the real question"},
	}
	index := session.Build("id", "m", messages, "d", 1, time.Now())
	if index.Title != "the real question" {
		t.Errorf("the title came from a host-authored entry: %q", index.Title)
	}
	if index.Turns != 1 {
		t.Errorf("turns counts host-authored entries: %d", index.Turns)
	}
}
