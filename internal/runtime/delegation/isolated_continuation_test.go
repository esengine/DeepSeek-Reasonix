package delegation

import (
	"context"
	"encoding/json"
	"os"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	statestore "reasonix/internal/state/store"
	"strings"
	"testing"
)

func TestIsolatedTranscriptCannotResumeInSourceOrOlderRuntime(t *testing.T) {
	p := &mockProvider{name: "sub"}
	store := mustSubagentStore(t)
	root := t.TempDir()
	task := newTestTaskTool(t, p, tool.NewRegistry(), "sys", "", "", nil).WithTranscripts(store, root, "m", "")
	ctx := agent.WithParentSession(context.Background(), "parent")
	spec, err := task.buildTaskSpec(ctx, "first", "", "", nil, nil, 0, "", "", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	prior := testSubagentSpec(t, "task")
	prior.ParentSession = "parent"
	prior.WorkspaceRoot = root
	prior.IsolationRef = "iso_retained"
	prior.IsolationGrant = isolationGrant(spec)
	run := startedRun(t, store, prior, said(provider.RoleAssistant, "done"))
	if err := store.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	args, _ := json.Marshal(map[string]string{"prompt": "continue", "continue_from": run.Ref})
	if _, err := task.Execute(ctx, args); err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("unisolated continuation=%v", err)
	}
	if len(p.requests) != 0 {
		t.Fatal("source provider was invoked")
	}
	legacy := prior
	legacy.IsolationRef = ""
	legacy.IsolationGrant = ""
	if err := validateMeta(run.Meta, legacy); err == nil {
		t.Fatal("previous-reader identity accepted isolated transcript")
	}
	spec.Context.ContinueFrom = run.Ref
	spec.Grant.WritePaths = writeclaim.WritePathSet{WholeWorkspace: true, WorkspaceRoot: root}
	if _, err := task.isolatedContinuation(ctx, &spec); err == nil {
		t.Fatal("write scope widened")
	}
}

func TestIsolatedStoppedTranscriptCanContinueOnlyWithItsBinding(t *testing.T) {
	for _, status := range []sessionstore.SubagentStatus{sessionstore.SubagentFailed, sessionstore.SubagentInterrupted} {
		t.Run(string(status), func(t *testing.T) {
			store := mustSubagentStore(t)
			spec := testSubagentSpec(t, "task")
			spec.IsolationRef = "iso_retained"
			spec.IsolationGrant = "original-scope"
			run, err := store.PrepareFresh(spec)
			if err != nil {
				t.Fatal(err)
			}
			run.Session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "pending", Name: "read_file", Arguments: `{"path":"x"}`}}})
			if err := store.saveTerminal(run, status, "fixture interruption"); err != nil {
				t.Fatal(err)
			}
			run.Release()
			unbound := spec
			unbound.IsolationRef = ""
			unbound.IsolationGrant = ""
			if _, err := store.PrepareContinue(run.Ref, unbound); err == nil {
				t.Fatal("unbound runtime resumed stopped work")
			}
			continued, err := store.PrepareContinue(run.Ref, spec)
			if err != nil {
				t.Fatal(err)
			}
			defer continued.Release()
			messages := continued.Session.Snapshot()
			if len(messages) < 3 || messages[len(messages)-1].Role != provider.RoleTool {
				t.Fatalf("interrupted tool call was not repaired: %+v", messages)
			}
		})
	}
}

func TestIsolationBindingSurvivesPrecedingMetadataWriter(t *testing.T) {
	store := mustSubagentStore(t)
	spec := testSubagentSpec(t, "task")
	spec.IsolationRef = "iso_retained"
	spec.IsolationGrant = "scope"
	run := startedRun(t, store, spec, said(provider.RoleAssistant, "done"))
	if err := store.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	raw, err := os.ReadFile(store.metaPath(run.Ref))
	if err != nil {
		t.Fatal(err)
	}
	var prior map[string]any
	if err := json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	delete(prior, "isolationRef")
	delete(prior, "isolationGrant")
	prior["status"] = "interrupted"
	raw, err = json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.metaPath(run.Ref), raw, 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := store.LoadMeta(run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if meta.IsolationRef != spec.IsolationRef || meta.IsolationGrant != spec.IsolationGrant {
		t.Fatal("preceding writer erased environment binding")
	}
	continued, err := store.PrepareContinue(run.Ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	continued.Release()
	bindingPath := statestore.SessionIsolationBinding(store.sessionPath(run.Ref))
	bindingData, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	var future map[string]any
	if err := json.Unmarshal(bindingData, &future); err != nil {
		t.Fatal(err)
	}
	future["version"] = 999
	futureBytes, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bindingPath, futureBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadMeta(run.Ref); err == nil {
		t.Fatal("future binding accepted")
	}
	if err := store.saveMeta(meta); err == nil {
		t.Fatal("future binding overwritten")
	}
	if got, err := os.ReadFile(bindingPath); err != nil || string(got) != string(futureBytes) {
		t.Fatal("future binding changed")
	}
}

func TestIsolatedNestedScopeIsRecordedSeparatelyAndSurvivesBackground(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	for _, paths := range [][]string{{"child.txt"}, nil} {
		scope, err := writeclaim.NormalizeWritePaths(target, paths)
		if err != nil {
			t.Fatal(err)
		}
		if paths == nil {
			scope = writeclaim.WritePathSet{WholeWorkspace: true, WorkspaceRoot: target}
		}
		spec := ProfileExecSpec{Grant: CapabilityGrant{WritePaths: scope}}
		ctx := context.WithValue(context.Background(), isolatedRunKey{}, isolatedRunBinding{ID: "iso_parent", Grant: "parent-grant", Source: source, Worktree: target})
		task := &TaskTool{}
		ctx, err = task.isolatedSpecContext(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		want, err := relocateClaim(scope, target, source)
		if err != nil {
			t.Fatal(err)
		}
		if isolatedBinding(ctx).Grant != isolationGrant(ProfileExecSpec{Grant: CapabilityGrant{WritePaths: want}}) || isolatedBinding(ctx).Grant == "parent-grant" {
			t.Fatal("nested write scope inherited parent grant")
		}
		bg := task.backgroundContext(context.Background(), ctx, "parent")
		if isolatedBinding(bg) != isolatedBinding(ctx) {
			t.Fatal("background lost isolation binding")
		}
	}
}
