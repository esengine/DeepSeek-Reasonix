package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCopyConversationPrefixKeepsBoundariesWithoutFileOrUndoState(t *testing.T) {
	root := testenv.TempDir(t)
	file := filepath.Join(root, "a.txt")
	sourceDir, childDir := filepath.Join(root, "parent.ckpt"), filepath.Join(root, "child.ckpt")
	write(t, file, "before")
	source := New(sourceDir, root)
	source.SetSessionID("parent")
	source.Begin(3, "first", 1)
	source.CaptureBefore(file, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, file, "after")
	source.CaptureAfter(file, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
	source.Begin(8, "second", 5)
	source.Begin(12, "later", 9)
	plan, err := source.PrepareRewind(3, RewindCode, 1, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	undone, err := source.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := source.PrepareRewind(12, RewindConversation, 1, 9, true)
	if err != nil {
		t.Fatal(err)
	}
	before := source.List()
	rawBefore := read(t, filepath.Join(sourceDir, "turn-3.json"))
	if err := source.CopyConversationPrefixTo(childDir, "child", 6); err != nil {
		t.Fatal(err)
	}
	child := New(childDir, root)
	copied := child.List()
	if len(copied) != 2 || child.NextTurn() != 9 {
		t.Fatalf("copied checkpoints: %+v", copied)
	}
	for i, checkpoint := range copied {
		original := before[i]
		if checkpoint.Turn != original.Turn || checkpoint.MsgIndex != original.MsgIndex ||
			checkpoint.Prompt != original.Prompt || !checkpoint.Time.Equal(original.Time) || len(checkpoint.Paths) != 0 || !checkpoint.ForkCopied {
			t.Fatalf("wrong conversation checkpoint: %+v", checkpoint)
		}
		var stored Checkpoint
		if err := json.Unmarshal([]byte(read(t, child.checkpointPath(&Checkpoint{Turn: checkpoint.Turn}))), &stored); err != nil {
			t.Fatal(err)
		}
		if stored.SessionID != "child" || len(stored.Files) != 0 || len(stored.CoverageGaps) != 0 || len(stored.ActiveWriters) != 0 || !stored.ForkCopied {
			t.Fatalf("inherited source state: %+v", stored)
		}
	}
	entries, err := os.ReadDir(childDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected fork artifacts: %v", entries)
	}
	if err := child.ValidatePlanSessionRevision(pending.PlanID, 1); err == nil {
		t.Fatal("copied a prepared rewind plan")
	}
	if _, err := child.UndoRewind(undone.TransactionID, nil); err == nil {
		t.Fatal("copied the parent's undo transaction")
	}
	if !reflect.DeepEqual(before, source.List()) || rawBefore != read(t, filepath.Join(sourceDir, "turn-3.json")) {
		t.Fatal("copy changed source checkpoints")
	}
}
