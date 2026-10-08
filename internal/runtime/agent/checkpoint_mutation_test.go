package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

type checkpointWriteTool struct {
	path, checkpointDir      string
	executed, breakRecording bool
	editErr                  error
}

func (*checkpointWriteTool) Name() string        { return "checkpoint_test_write" }
func (*checkpointWriteTool) Description() string { return "write a test file" }
func (*checkpointWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (*checkpointWriteTool) ReadOnly() bool { return false }
func (w *checkpointWriteTool) Preview(context.Context, json.RawMessage) (diff.Change, error) {
	old, err := os.ReadFile(w.path)
	return diff.Change{Path: w.path, OldText: string(old), NewText: "new work"}, err
}
func (w *checkpointWriteTool) Execute(context.Context, json.RawMessage) (string, error) {
	w.executed = true
	if err := os.WriteFile(w.path, []byte("new work"), 0o644); err != nil {
		return "", err
	}
	if w.breakRecording {
		if err := obstructTransactions(w.checkpointDir); err != nil {
			return "", err
		}
		manifest := filepath.Join(w.checkpointDir, "turn-0.json")
		if err := os.Remove(manifest); err != nil {
			return "", err
		}
		if err := os.Mkdir(manifest, 0o755); err != nil {
			return "", err
		}
	}
	return "edit completed", w.editErr
}

func obstructTransactions(dir string) error {
	path := filepath.Join(dir, "transactions")
	if err := os.Rename(path, path+"-backup"); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("blocked"), 0o644)
}

func checkpointToolFixture(t *testing.T) (*checkpoint.Store, string, string) {
	t.Helper()
	root, dir := testenv.TempDir(t), testenv.TempDir(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := checkpoint.New(dir, root)
	if err := s.Begin(0, "edit", 0); err != nil {
		t.Fatal(err)
	}
	s.CaptureBefore(path, checkpoint.CaptureBeforeOpts{})
	if err := os.WriteFile(path, []byte("owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureAfter(path, checkpoint.CaptureAfterOpts{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PrepareRewind(0, checkpoint.RewindCode, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	return s, dir, path
}

func runCheckpointTool(t *testing.T, s *checkpoint.Store, writer *checkpointWriteTool) (batchExecution, []event.Event) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(writer)
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	obs := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: s})
	a := New(nil, reg, sessionstore.NewSession(""), Options{MutationObserver: obs, WriteWorkspaceRoot: filepath.Dir(writer.path)}, sink)
	result := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "edit", Name: writer.Name(), Arguments: `{}`}})
	return result, events
}

func TestCheckpointPreparationFailureDoesNotExecuteWriter(t *testing.T) {
	s, dir, path := checkpointToolFixture(t)
	if err := obstructTransactions(dir); err != nil {
		t.Fatal(err)
	}
	writer := &checkpointWriteTool{path: path, checkpointDir: dir}
	batch, _ := runCheckpointTool(t, s, writer)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if writer.executed || string(data) != "before" || !batch.outcomes[0].blocked {
		t.Fatalf("writer executed=%v, file=%q, outcome=%+v", writer.executed, data, batch.outcomes[0])
	}
	if s.Barrier().Busy() {
		t.Fatal("failed preparation leaked the write barrier")
	}
}

func TestCheckpointRecordingFailureKeepsToolResultTruthful(t *testing.T) {
	for _, failEdit := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful_edit", true: "failed_edit"}[failEdit], func(t *testing.T) {
			s, dir, path := checkpointToolFixture(t)
			writer := &checkpointWriteTool{path: path, checkpointDir: dir, breakRecording: true}
			if failEdit {
				writer.editErr = errors.New("tool edit failed")
			}
			batch, events := runCheckpointTool(t, s, writer)
			out := batch.outcomes[0]
			wantErr := ""
			if failEdit {
				wantErr = writer.editErr.Error()
			}
			if out.errMsg != wantErr {
				t.Fatalf("tool error=%q, want %q", out.errMsg, wantErr)
			}
			if !failEdit && !strings.HasPrefix(batch.results[0], "edit completed") {
				t.Fatalf("model result=%q", batch.results[0])
			}
			notices := 0
			for _, e := range events {
				if e.Kind == event.Notice && e.Code == "checkpoint_recording_failed" {
					notices++
					if e.Audience != event.NoticeAudienceOperator {
						t.Fatal("checkpoint warning must be separate from the model tool result")
					}
				}
				if e.Kind == event.ToolResult && e.Tool.Err != wantErr {
					t.Fatalf("ToolResult error=%q", e.Tool.Err)
				}
			}
			if notices != 1 {
				t.Fatalf("checkpoint warnings=%d, want 1", notices)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "new work" {
				t.Fatalf("file=%q, err=%v", data, err)
			}
			plan, err := s.PrepareRewind(0, checkpoint.RewindCode, 2, 0, true)
			if err != nil || !plan.CanFiles {
				t.Fatalf("after identity was not recorded: %+v, %v", plan, err)
			}
			txPath := filepath.Join(dir, "transactions")
			if err := os.Remove(txPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(txPath+"-backup", txPath); err != nil {
				t.Fatal(err)
			}
			recovered := checkpoint.New(dir, filepath.Dir(path))
			if undo, err := recovered.AvailableUndo(); err != nil || undo != nil {
				t.Fatalf("obsolete undo recovered: %+v, %v", undo, err)
			}
		})
	}
}
