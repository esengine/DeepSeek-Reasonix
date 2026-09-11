package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type crashAfterEffectTool struct{ path string }

func (crashAfterEffectTool) Name() string            { return "crash_after_effect" }
func (crashAfterEffectTool) Description() string     { return "test fixture" }
func (crashAfterEffectTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (crashAfterEffectTool) ReadOnly() bool          { return false }
func (t crashAfterEffectTool) Execute(context.Context, json.RawMessage) (string, error) {
	f, err := os.OpenFile(t.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", err
	}
	if _, err = f.WriteString("effect\n"); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	_ = f.Close()
	os.Exit(73) // A committed external effect, with no local result receipt.
	return "", nil
}

func TestToolRecoveryCrashAfterEffect(t *testing.T) {
	if root := os.Getenv("REASONIX_RECOVERY_CRASH_FIXTURE"); root != "" {
		reg := tool.NewRegistry()
		reg.Add(crashAfterEffectTool{path: filepath.Join(root, "effects")})
		p := &recordingProvider{streams: [][]provider.Chunk{{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "crash", Name: "crash_after_effect", Arguments: `{}`}}, {Type: provider.ChunkDone}}}}
		a := agent.New(p, reg, agent.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Executor: a, Runner: a, SessionPath: filepath.Join(root, "session.jsonl"), SessionDir: root, Sink: event.Discard})
		if err := c.RunTurn(context.Background(), "perform effect"); err != nil {
			t.Fatal(err)
		}
		t.Fatal("fixture failed to crash")
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestToolRecoveryCrashAfterEffect$")
	cmd.Env = append(os.Environ(), "REASONIX_RECOVERY_CRASH_FIXTURE="+root)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("crash helper: %v %s", err, out)
	}
	path := filepath.Join(root, "session.jsonl")
	s, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(nil, tool.NewRegistry(), s, agent.Options{}, event.Discard)
	c := New(Options{Executor: a, SessionPath: path, SessionDir: root, Sink: event.Discard})
	defer c.Close()
	c.recoverInterruptedTurn(path)
	view := c.ToolRecoverySnapshot()
	if len(view.Calls) != 1 {
		t.Fatalf("unresolved crash effects=%+v", view)
	}
	call := view.Calls[0]
	req := ToolRecoveryRequest{SessionPath: view.SessionPath, RuntimeEpoch: view.RuntimeEpoch, Revision: view.Revision, AttemptID: call.Identity.AttemptID, Action: "inspect"}
	view, err = c.ResolveToolRecovery(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Revision = view.Revision
	req.InspectionID = view.Calls[0].InspectionID
	req.Action = "confirm"
	// Two UI requests with the same snapshot can confirm at most once.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := c.ResolveToolRecovery(context.Background(), req); errs <- e })
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("confirmation successes=%d", success)
	}
	reopened, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	other := agent.New(nil, tool.NewRegistry(), reopened, agent.Options{}, event.Discard)
	if len(other.PendingToolRecovery()) != 0 {
		t.Fatal("confirmation did not survive restart")
	}
	effects, err := os.ReadFile(filepath.Join(root, "effects"))
	if err != nil || string(effects) != "effect\n" {
		t.Fatalf("effect repeated: %q %v", effects, err)
	}
}
