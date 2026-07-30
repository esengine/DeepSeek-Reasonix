package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
	"reasonix/internal/tool"
)

// End-to-end: scripted provider fails verification, runs read-only diagnosis,
// then performs an external write without turning execution risk into a user
// decision. Also verifies recovery sidecar persistence and metrics.
func TestRecoveryCheckpointScriptedE2E(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")

	bash := &recoveryWriteTool{name: "bash", failOnce: true}
	read := &recoveryWriteTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(bash)
	reg.Add(read)

	prov := &recordingProvider{streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "2", Name: "read_file", Arguments: `{"path":"main.go"}`}}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "3", Name: "bash", Arguments: `{"command":"git push origin feature"}`}}},
		{{Type: provider.ChunkText, Text: "done"}},
	}}

	sess := agent.NewSession("You are a test agent.")
	ag := agent.New(prov, reg, sess, agent.Options{MaxSteps: 10}, event.Discard)
	// Leave SessionPath empty so autosave does not hold file locks on dir.
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.Policy{Mode: permission.Allow},
	})
	t.Cleanup(func() { c.Close() })
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.EnableInteractiveApproval()

	if err := c.Run(context.Background(), "test then fix"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if read.runs < 1 {
		t.Fatalf("expected read-only diagnosis, runs=%d", read.runs)
	}
	if bash.runs != 2 {
		t.Fatalf("bash runs = %d, want failed verification plus automatic push", bash.runs)
	}

	// Sidecar persistence (write a synthetic session path under temp dir).
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		gate.ObserveResult(context.Background(), agent.RecoveryObservation{
			Tool: "bash", Verification: true, ErrSummary: "exit 1",
			Args: json.RawMessage(`{"command":"go test"}`),
		})
		if err := recovery.SaveSnapshot(sessionPath, gate.Snapshot()); err != nil {
			t.Fatalf("SaveSnapshot: %v", err)
		}
	}
	if _, err := os.Stat(recovery.PathFor(sessionPath)); err != nil {
		t.Fatalf("recovery sidecar: %v", err)
	}
	snap, err := recovery.LoadSnapshot(sessionPath)
	if err != nil || len(snap.Tasks) == 0 {
		t.Fatalf("LoadSnapshot: err=%v tasks=%d", err, len(snap.Tasks))
	}

	m := c.RecoveryMetrics()
	if m.FailureEvents == 0 || m.HumanPrompts != 0 || m.HumanContinues != 0 {
		t.Fatalf("metrics = %+v", m)
	}
	delta := c.DrainRecoveryMetrics()
	if delta.FailureEvents == 0 || delta.HumanPrompts != 0 || delta.HumanContinues != 0 {
		t.Fatalf("drained metrics = %+v", delta)
	}
	if next := c.DrainRecoveryMetrics(); next != (recovery.Metrics{}) {
		t.Fatalf("second metrics drain = %+v, want zero delta", next)
	}
}
