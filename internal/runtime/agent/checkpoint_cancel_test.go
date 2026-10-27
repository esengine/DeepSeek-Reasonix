package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/workspacelease"
)

func TestCancelledCheckpointWaitNeverEntersToolOrHook(t *testing.T) {
	for _, reader := range []bool{false, true} {
		for _, proxy := range []bool{false, true} {
			name := "writer"
			if reader {
				name = "reader_hook"
			}
			if proxy {
				name += "/capability"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "note.txt")
				if err := os.WriteFile(path, []byte("user bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				synctest.Test(t, func(t *testing.T) {
					store := checkpoint.New("", root)
					if err := store.Barrier().EnterExclusive(); err != nil {
						t.Fatal(err)
					}
					var target tool.Tool = &checkpointWriteTool{path: path}
					var hooks ToolHooks
					hookCalls := 0
					if reader {
						target = fakeTool{name: "probe", readOnly: true}
						hooks = &checkpointToolHooks{pre: func() {
							hookCalls++
							if err := os.WriteFile(path, []byte("hook bytes"), 0o600); err != nil {
								t.Error(err)
							}
						}}
					}
					reg := tool.NewRegistry()
					reg.Add(target)
					call := provider.ToolCall{ID: "cancelled", Name: target.Name(), Arguments: `{}`}
					if proxy {
						reg.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{Target: target, TargetName: target.Name(), ReadOnly: reader, Args: json.RawMessage(`{}`)}})
						call.Name = "use_capability"
					}
					a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: hooks}, event.Discard)
					a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done := make(chan toolOutcome, 1)
					go func() { done <- a.executeOne(ctx, &a.turn, call) }()
					synctest.Wait()
					select {
					case out := <-done:
						t.Fatalf("call did not wait for exclusive owner: %+v", out)
					default:
					}
					cancel()
					synctest.Wait()
					var out toolOutcome
					select {
					case out = <-done:
						if !store.Barrier().Busy() || store.Barrier().HasWriters() {
							t.Error("cancelled waiter changed the exclusive owner's admission")
						}
						store.Barrier().ExitExclusive()
					default:
						t.Error("cancelled call still waits for checkpoint admission")
						store.Barrier().ExitExclusive()
						out = <-done
					}
					if !out.blocked || out.executed || hookCalls != 0 {
						t.Errorf("cancelled invocation entered user code: outcome=%+v, hooks=%d", out, hookCalls)
					}
					if store.Barrier().Busy() {
						t.Error("cancelled invocation leaked writer admission")
					}
				})
				if data, err := os.ReadFile(path); err != nil || string(data) != "user bytes" {
					t.Fatalf("cancelled invocation changed bytes: %q %v", data, err)
				}
			})
		}
	}
}

func TestCancelledCheckpointWaitReleasesProvisionalWorkspacePaths(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()
	probe.BeginRun()
	defer probe.EndRun()
	path := filepath.Join(root, "pending.txt")
	synctest.Test(t, func(t *testing.T) {
		store := checkpoint.New("", root)
		if err := store.Barrier().EnterExclusive(); err != nil {
			t.Fatal(err)
		}
		writer := &partialLeaseWriter{workspaceLeaseTestTool: workspaceLeaseTestTool{name: "pending_writer"}, path: path}
		a := deliveryLeaseTestAgent(t, owner, writer)
		a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store}))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan toolOutcome, 1)
		go func() { done <- a.executeOne(ctx, &a.turn, providerToolCall("pending", writer.Name())) }()
		synctest.Wait()
		select {
		case out := <-done:
			store.Barrier().ExitExclusive()
			t.Fatalf("call did not wait at checkpoint admission: %+v", out)
		default:
		}
		cancel()
		synctest.Wait()
		select {
		case out := <-done:
			if !out.blocked || out.executed {
				t.Errorf("cancelled invocation = %+v", out)
			}
			store.Barrier().ExitExclusive()
		default:
			t.Error("cancelled call retained its provisional workspace admission")
			store.Barrier().ExitExclusive()
			<-done
		}
		if writer.calls.Load() != 0 {
			t.Error("cancelled writer entered execution")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := probe.AcquirePaths(ctx, []string{path}); err != nil {
		t.Fatalf("unfinished actor retained aborted call paths: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled call changed the workspace: %v", err)
	}
}
