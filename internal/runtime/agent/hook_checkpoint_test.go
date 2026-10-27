package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/hook"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

type checkpointToolHooks struct {
	typedNilHooks
	pre     func()
	post    func()
	failure func()
	deny    bool
}

func (h *checkpointToolHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	if h.pre != nil {
		h.pre()
	}
	return h.deny, "hook denied"
}
func (h *checkpointToolHooks) PostToolUse(context.Context, string, json.RawMessage, string) {
	if h.post != nil {
		h.post()
	}
}
func (h *checkpointToolHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
	if h.failure != nil {
		h.failure()
	}
}

func TestReadOnlyToolHooksHoldCheckpointWriteAdmission(t *testing.T) {
	for _, stage := range []string{"pre", "deny", "post", "failure"} {
		for _, proxy := range []bool{false, true} {
			name := stage + "/direct"
			if proxy {
				name = stage + "/capability"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "hook.txt")
				store := checkpoint.New("", root)
				if err := store.Begin(0, "fixture", 0); err != nil {
					t.Fatal(err)
				}
				entered := 0
				write := func() {
					entered++
					if !store.Barrier().HasWriters() {
						t.Error("hook entered without checkpoint write admission")
					}
					if store.Barrier().TryEnterExclusive() {
						store.Barrier().ExitExclusive()
						t.Error("rewind could enter during hook write")
					}
					if err := os.WriteFile(path, []byte("hook work"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				hooks := &checkpointToolHooks{deny: stage == "deny"}
				switch stage {
				case "pre", "deny":
					hooks.pre = write
				case "post":
					hooks.post = write
				case "failure":
					hooks.failure = write
				}
				var err error
				if stage == "failure" {
					err = errors.New("reader failed")
				}
				target := fakeTool{name: "probe", readOnly: true, err: err}
				reg := tool.NewRegistry()
				reg.Add(target)
				call := provider.ToolCall{ID: "hook", Name: "probe", Arguments: `{}`}
				if proxy {
					reg.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{Target: target, TargetName: "probe", ReadOnly: true, Args: json.RawMessage(`{}`)}})
					call.Name = "use_capability"
				}
				a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: hooks}, event.Discard)
				a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
				out := a.executeOne(context.Background(), &a.turn, call)
				if out.executed == (stage == "deny") {
					t.Fatalf("execution flag = %v", out.executed)
				}
				if entered != 1 {
					t.Fatalf("hook calls = %d", entered)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "hook work" {
					t.Fatalf("hook bytes = %q %v", data, err)
				}
				if store.Barrier().Busy() {
					t.Fatal("hook completion leaked checkpoint admission")
				}
				metas := store.List()
				found := false
				for _, gap := range metas[0].CoverageGaps {
					if gap.Reason == checkpoint.GapHookWrite {
						found = true
					}
				}
				if !found {
					t.Fatal("hook write omitted checkpoint coverage gap")
				}
			})
		}
	}
}

func TestReadHookStopsBeforeCallbackWhenCheckpointAdmissionFails(t *testing.T) {
	root := t.TempDir()
	store := checkpoint.New("", root)
	if err := store.Begin(0, "fixture", 0); err != nil {
		t.Fatal(err)
	}
	store.Barrier().Close()
	hooks := &checkpointToolHooks{pre: func() { t.Error("hook ran after checkpoint admission failed") }}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "probe", readOnly: true})
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: hooks}, event.Discard)
	a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "closed", Name: "probe", Arguments: `{}`})
	if !out.blocked || out.executed {
		t.Fatalf("failed admission did not stop call: %+v", out)
	}
	if store.Barrier().HasWriters() {
		t.Fatal("failed admission retained writer")
	}
}

func TestReadOnlyWithoutToolMutationHooksDoesNotNeedCheckpointWriteAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hooks ToolHooks
	}{
		{name: "nil"},
		{name: "empty", hooks: hook.NewRunner(nil, t.TempDir(), nil, nil)},
		{name: "session-only", hooks: hook.NewRunner([]hook.ResolvedHook{{Event: hook.SessionStart}}, t.TempDir(), nil, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := checkpoint.New("", t.TempDir())
			store.Barrier().Close()
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "probe", readOnly: true})
			a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: tc.hooks}, event.Discard)
			a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "probe", Arguments: `{}`})
			if out.blocked || !out.executed || out.errMsg != "" {
				t.Fatalf("reader unnecessarily needed writer admission: %+v", out)
			}
		})
	}
}

func checkpointUndoForReadHook(t *testing.T) (*checkpoint.Store, string, string, string, checkpoint.RewindResult) {
	t.Helper()
	root, dir := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := checkpoint.New(dir, root)
	if err := store.Begin(0, "edit", 0); err != nil {
		t.Fatal(err)
	}
	store.CaptureBefore(path, checkpoint.CaptureBeforeOpts{})
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.CaptureAfter(path, checkpoint.CaptureAfterOpts{Seq: 1})
	plan, err := store.PrepareRewind(0, checkpoint.RewindCode, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if offer, err := store.AvailableUndo(); err != nil || offer == nil {
		t.Fatalf("undo fixture: %+v %v", offer, err)
	}
	return store, dir, root, path, result
}

func TestReadHookRetiresUndoBeforeCallbackAndAfterReload(t *testing.T) {
	for _, deny := range []bool{false, true} {
		name := "allowed"
		if deny {
			name = "denied"
		}
		t.Run(name, func(t *testing.T) {
			store, dir, root, path, result := checkpointUndoForReadHook(t)
			hooks := &checkpointToolHooks{deny: deny, pre: func() {
				if offer, err := store.AvailableUndo(); err != nil || offer != nil {
					t.Errorf("hook entered before undo was retired: %+v %v", offer, err)
				}
				if err := os.WriteFile(path, []byte("hook work"), 0o600); err != nil {
					t.Fatal(err)
				}
			}}
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "probe", readOnly: true})
			a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: hooks}, event.Discard)
			a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "probe", Arguments: `{}`})
			if out.executed == deny {
				t.Fatalf("tool execution = %v", out.executed)
			}
			reloaded := checkpoint.New(dir, root)
			if offer, err := reloaded.AvailableUndo(); err != nil || offer != nil {
				t.Errorf("undo retirement was not durable: %+v %v", offer, err)
			}
			if _, err := reloaded.UndoRewind(result.TransactionID, nil); err == nil {
				t.Fatal("retired undo was accepted")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "hook work" {
				t.Fatalf("hook work changed: %q %v", data, err)
			}
		})
	}
}

func TestReadHookRefusesWhenUndoRetirementCannotPersist(t *testing.T) {
	store, dir, _, path, _ := checkpointUndoForReadHook(t)
	txDir := filepath.Join(dir, "transactions")
	if err := os.Rename(txDir, txDir+"-backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txDir, []byte("unavailable directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	hooks := &checkpointToolHooks{pre: func() { t.Error("hook entered after failed durable retirement") }}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "probe", readOnly: true})
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: hooks}, event.Discard)
	a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "probe", Arguments: `{}`})
	if !out.blocked || out.executed {
		t.Fatalf("reader entered despite failed preparation: %+v", out)
	}
	if store.Barrier().HasWriters() {
		t.Fatal("failed preparation retained writer")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "before" {
		t.Fatalf("failed preparation changed bytes: %q %v", data, err)
	}
}
