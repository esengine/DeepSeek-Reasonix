package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestProjectImageOffloadDoesNotRewriteUIHistory(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"message": provider.Message{ID: "u1", Role: provider.RoleUser, Content: "see", Images: []string{"data:image/png;base64,AA==", "data:image/png;base64,BB=="}},
	})
	offload, _ := json.Marshal(provider.ImageOffloadPayload{Targets: []provider.ImageOffloadTarget{{MessageID: "u1", ImageIndexes: []int{0}}}})
	proj, err := Project([]Commit{{
		Events: []Event{
			{Sequence: 1, Kind: "message/complete", Payload: payload},
			{Sequence: 2, Kind: EventImageOffload, Optional: true, Payload: offload},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Messages[0].Images[0] != "data:image/png;base64,AA==" {
		t.Fatalf("UI history mutated: %v", proj.Messages[0].Images)
	}
	if proj.ModelMessages[0].Images[0] != provider.ImageOffloadedRef {
		t.Fatalf("model projection = %v", proj.ModelMessages[0].Images)
	}
}

func TestImageOffloadSurvivesModelContextReplace(t *testing.T) {
	original := provider.Message{
		ID: "u1", Role: provider.RoleUser, Content: "see",
		Images: []string{"data:image/png;base64,AA==", "data:image/png;base64,BB=="},
	}
	message, _ := json.Marshal(map[string]any{"message": original})
	offload, _ := json.Marshal(provider.ImageOffloadPayload{Targets: []provider.ImageOffloadTarget{{MessageID: "u1", ImageIndexes: []int{0}}}})
	replace, _ := json.Marshal(map[string]any{"messages": []provider.Message{original}, "reason": "compaction", "sourceSequences": []uint64{1}})
	proj, err := Project([]Commit{{
		Events: []Event{
			{Sequence: 1, Kind: "message/complete", Payload: message},
			{Sequence: 2, Kind: EventImageOffload, Optional: true, Payload: offload},
			{Sequence: 3, Kind: "model/context-replace", Payload: replace},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Messages[0].Images[0] != "data:image/png;base64,AA==" {
		t.Fatalf("UI history mutated: %v", proj.Messages[0].Images)
	}
	if proj.ModelMessages[0].Images[0] != provider.ImageOffloadedRef {
		t.Fatalf("compaction restored omitted image: %v", proj.ModelMessages[0].Images)
	}
	if proj.ModelMessages[0].Images[1] != "data:image/png;base64,BB==" {
		t.Fatalf("kept image dropped: %v", proj.ModelMessages[0].Images)
	}
}

func TestUnknownOptionalOffloadDoesNotBlockOldShape(t *testing.T) {
	if !ProjectionKinds[EventImageOffload] {
		t.Fatal("image/offload must be a known projection kind")
	}
}

func TestImageOffloadPersistsAcrossResumeAndFork(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	parent, err := Open(parentDir, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close(context.Background()) })
	message, _ := json.Marshal(map[string]any{
		"message": provider.Message{
			ID: "u1", Role: provider.RoleUser, Content: "see",
			Images: []string{"data:image/png;base64,AA==", "data:image/png;base64,BB=="},
		},
	})
	offload, _ := json.Marshal(provider.ImageOffloadPayload{Targets: []provider.ImageOffloadTarget{{MessageID: "u1", ImageIndexes: []int{0}}}})
	commit, err := parent.Append(t.Context(), Batch{OperationID: "turn-1", TurnID: "t1", Events: []Event{
		{Kind: "turn/start"},
		{Kind: "message/complete", Payload: message},
		{Kind: EventImageOffload, Optional: true, Payload: offload},
		{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertOffloadProjection := func(t *testing.T, proj Projection) {
		t.Helper()
		if len(proj.Messages) != 1 || proj.Messages[0].Images[0] != "data:image/png;base64,AA==" {
			t.Fatalf("UI history = %+v", proj.Messages)
		}
		if len(proj.ModelMessages) != 1 || proj.ModelMessages[0].Images[0] != provider.ImageOffloadedRef {
			t.Fatalf("model projection = %v", proj.ModelMessages)
		}
		if proj.ModelMessages[0].Images[1] != "data:image/png;base64,BB==" {
			t.Fatalf("kept image dropped: %v", proj.ModelMessages[0].Images)
		}
	}
	assertOffloadProjection(t, parent.Snapshot().Projection)
	if _, err := parent.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := parent.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	commits, err := Replay(parentDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := Project(commits)
	if err != nil {
		t.Fatal(err)
	}
	assertOffloadProjection(t, replayed)

	reopened, err := Open(parentDir, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	model := reopened.DeriveMessages()
	if len(model) != 1 || model[0].Images[0] != provider.ImageOffloadedRef || model[0].Images[1] != "data:image/png;base64,BB==" {
		t.Fatalf("resumed model messages = %+v", model)
	}

	childDir := filepath.Join(root, "child")
	if _, err := reopened.Fork(t.Context(), childDir, "child", commit.LastSequence()); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	child, err := Open(childDir, "child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close(context.Background()) })
	assertOffloadProjection(t, child.Snapshot().Projection)
}
