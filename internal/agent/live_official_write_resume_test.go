//go:build live

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// A real model prepares the built-in write. We checkpoint its actual intent,
// interrupt after the disk effect, and reopen only the pre-result checkpoint.
// The only model tool is a writer confined to an empty disposable directory.
func TestLiveOfficialWriteAfterEffectResume(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			p := officialMatrixProvider(t, key, "deepseek-v4-flash", protocol, "high", "")
			runLiveWriteAfterEffectResume(t, p, protocol)
		})
	}
}

func runLiveWriteAfterEffectResume(t *testing.T, p provider.Provider, label string) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "marker.txt")
	state := filepath.Join(t.TempDir(), "session.jsonl")
	lease, err := TryAcquireSessionLease(state)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var writes atomic.Int32
	reg := tool.NewRegistry()
	for _, w := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		if w.Name() == "write_file" {
			reg.Add(builtin.BindFileWriteReceipt(w, func(string, bool, []byte) { writes.Add(1); cancel() }))
		}
	}
	sess := NewSession("Follow the user's precise file request. Never repeat a file write whose expected contents are already satisfied.")
	sink := &liveWriteCheckpointSink{session: sess, path: state}
	a := New(p, reg, sess, Options{MaxSteps: 4, MaxOutputTokens: 2048, MissingReasoningWarnStateDir: t.TempDir()}, sink)
	a.SetSessionPath(state)
	if err := a.Run(ctx, fmt.Sprintf("Use write_file once to write exactly the text live-write-marker (no newline) to %s. Then report completion.", target)); err == nil {
		t.Fatal("expected interrupted run")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "live-write-marker" {
		t.Fatalf("actual file mismatch, err=%v bytes=%d", err, len(data))
	}
	if writes.Load() != 1 || sink.checkpoints.Load() != 1 {
		t.Fatalf("writes=%d checkpoints=%d", writes.Load(), sink.checkpoints.Load())
	}
	reopened, err := LoadSession(state)
	if err != nil {
		t.Fatal(err)
	}
	intents, results, unknown := 0, 0, 0
	for _, m := range reopened.Snapshot() {
		for _, c := range m.ToolCalls {
			intents += len(c.WriteIntents)
		}
		if m.Role == provider.RoleTool {
			if provider.ToolResultRunState(m) == provider.ToolRunUnknown {
				unknown++
			} else {
				results++
			}
		}
	}
	if intents != 1 || results != 0 || unknown != 1 {
		t.Fatalf("pre-result checkpoint intents=%d results=%d unknown=%d", intents, results, unknown)
	}
	resume := New(p, reg, reopened, Options{MaxSteps: 4, MaxOutputTokens: 2048, MissingReasoningWarnStateDir: t.TempDir()}, event.Discard)
	resume.SetSessionPath(state)
	next, done := context.WithTimeout(context.Background(), 90*time.Second)
	defer done()
	if err := resume.Run(next, "Continue from the interrupted operation. Use the verified write postconditions. Do not rewrite satisfied content; just report whether the target is satisfied."); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(target)
	if err != nil || string(data) != "live-write-marker" || writes.Load() != 1 {
		t.Fatalf("resume changed file or repeated write: writes=%d err=%v", writes.Load(), err)
	}
	t.Logf("protocol=%s intent_checkpoints=%d disk_writes=%d original_results=%d reopened_messages=%d", label, sink.checkpoints.Load(), writes.Load(), results, len(reopened.Snapshot()))
}

type liveWriteCheckpointSink struct {
	session     *Session
	path        string
	checkpoints atomic.Int32
}

func (s *liveWriteCheckpointSink) Emit(event.Event) {}
func (s *liveWriteCheckpointSink) EmitChecked(e event.Event) error {
	if e.WriteIntent {
		if err := s.session.Save(s.path); err != nil {
			return err
		}
		s.checkpoints.Add(1)
	}
	return nil
}
