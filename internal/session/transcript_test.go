package session_test

import (
	"reflect"
	"testing"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/session"
)

func TestAppendThenAllKeepsOrder(t *testing.T) {
	t.Parallel()

	tr := &session.Transcript{}
	tr.Append(session.Message{ID: "m1", Role: provider.RoleSystem, Content: "you are terse"})
	tr.Append(session.Message{ID: "m2", Role: provider.RoleUser, Content: "hello"})
	tr.Append(session.Message{ID: "m3", Role: provider.RoleAssistant, Content: "hi"})

	got := tr.All()

	want := []session.Message{
		{ID: "m1", Role: provider.RoleSystem, Content: "you are terse"},
		{ID: "m2", Role: provider.RoleUser, Content: "hello"},
		{ID: "m3", Role: provider.RoleAssistant, Content: "hi"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("All() = %+v, want %+v", got, want)
	}
	if len(got) != 3 {
		t.Fatalf("len(All()) = %d, want 3", len(got))
	}
}

func TestAllReturnsCopySoAppendOnlyHolds(t *testing.T) {
	t.Parallel()

	tr := &session.Transcript{}
	tr.Append(session.Message{ID: "m1", Role: provider.RoleUser, Content: "original"})
	tr.Append(session.Message{ID: "m2", Role: provider.RoleAssistant, Content: "reply"})

	first := tr.All()
	first[0].Content = "tampered"
	first[1].Role = provider.RoleSystem
	first = append(first, session.Message{ID: "m3", Role: provider.RoleUser, Content: "injected"})
	if len(first) != 3 {
		t.Fatalf("len after local append = %d, want 3", len(first))
	}

	again := tr.All()
	if len(again) != 2 {
		t.Fatalf("len(All()) after external append = %d, want 2", len(again))
	}
	if again[0].Content != "original" {
		t.Errorf("All()[0].Content = %q, want %q", again[0].Content, "original")
	}
	if again[1].Role != provider.RoleAssistant {
		t.Errorf("All()[1].Role = %q, want %q", again[1].Role, provider.RoleAssistant)
	}
	if again[1].Content != "reply" {
		t.Errorf("All()[1].Content = %q, want %q", again[1].Content, "reply")
	}
}

func TestFoldSkipsHiddenAndKeepsOrder(t *testing.T) {
	t.Parallel()

	tr := &session.Transcript{}
	tr.Append(session.Message{ID: "m1", Role: provider.RoleSystem, Content: "system prompt"})
	tr.Append(session.Message{ID: "m2", Role: provider.RoleUser, Content: "compaction digest", Hidden: true})
	tr.Append(session.Message{ID: "m3", Role: provider.RoleUser, Content: "hello"})
	tr.Append(session.Message{ID: "m4", Role: provider.RoleAssistant, Content: "note to self", Hidden: true})
	tr.Append(session.Message{ID: "m5", Role: provider.RoleAssistant, Content: "hi"})

	got := tr.Fold()

	want := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fold() = %+v, want %+v", got, want)
	}
	if len(got) != 3 {
		t.Fatalf("len(Fold()) = %d, want 3", len(got))
	}
}

func TestFoldMapsToolFieldsAndDropsRawContent(t *testing.T) {
	t.Parallel()

	tr := &session.Transcript{}
	tr.Append(session.Message{
		ID:         "m1",
		Role:       provider.RoleAssistant,
		Content:    "reading the file",
		RawContent: `{"vendor_envelope":{"raw":true}}`,
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		},
	})
	tr.Append(session.Message{
		ID:         "m2",
		Role:       provider.RoleTool,
		Content:    "file body",
		RawContent: "<raw tool blob>",
		ToolCallID: "call_1",
	})

	got := tr.Fold()

	want := []provider.Message{
		{
			Role:    provider.RoleAssistant,
			Content: "reading the file",
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
			},
		},
		{Role: provider.RoleTool, Content: "file body", ToolCallID: "call_1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fold() = %+v, want %+v", got, want)
	}

	// provider.Message has no field for RawContent, so the only observable
	// requirement is that Content survives unchanged while the raw payload differs.
	if got[0].Content != "reading the file" {
		t.Errorf("Fold()[0].Content = %q, want %q", got[0].Content, "reading the file")
	}
	if got[0].Content == `{"vendor_envelope":{"raw":true}}` {
		t.Error("Fold() carried RawContent through to the wire message")
	}
	if len(got[0].ToolCalls) != 1 {
		t.Fatalf("len(Fold()[0].ToolCalls) = %d, want 1", len(got[0].ToolCalls))
	}
	if got[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("Fold()[0].ToolCalls[0].ID = %q, want %q", got[0].ToolCalls[0].ID, "call_1")
	}
	if got[0].ToolCalls[0].Name != "read_file" {
		t.Errorf("Fold()[0].ToolCalls[0].Name = %q, want %q", got[0].ToolCalls[0].Name, "read_file")
	}
	if got[0].ToolCalls[0].Arguments != `{"path":"a.go"}` {
		t.Errorf("Fold()[0].ToolCalls[0].Arguments = %q, want %q", got[0].ToolCalls[0].Arguments, `{"path":"a.go"}`)
	}
	if got[1].ToolCallID != "call_1" {
		t.Errorf("Fold()[1].ToolCallID = %q, want %q", got[1].ToolCallID, "call_1")
	}
}

func TestFoldOfEmptyTranscriptIsEmptyNotNil(t *testing.T) {
	t.Parallel()

	tr := &session.Transcript{}

	got := tr.Fold()

	if got == nil {
		t.Fatal("Fold() on an empty transcript returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(Fold()) on an empty transcript = %d, want 0", len(got))
	}
}
