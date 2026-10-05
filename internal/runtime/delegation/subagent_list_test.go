package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

func saveListedChild(t *testing.T, store *SubagentStore, spec SubagentSpec, save func(*SubagentStore, *SubagentRun) error) string {
	t.Helper()
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := save(store, run); err != nil {
		t.Fatalf("save child: %v", err)
	}
	run.Release()
	return run.Ref
}

func TestListForParentKeepsOwnedChildrenAndDropsOthers(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	spec := testSubagentSpec(t, "explore")

	mine := saveListedChild(t, store, spec, (*SubagentStore).SaveCompleted)
	failed := saveListedChild(t, store, spec, (*SubagentStore).SaveFailed)

	elsewhere := spec
	elsewhere.ParentSession = "another-session"
	stranger := saveListedChild(t, store, elsewhere, (*SubagentStore).SaveCompleted)

	otherWorkspace := spec
	otherWorkspace.WorkspaceRoot = testenv.TempDir(t)
	moved := saveListedChild(t, store, otherWorkspace, (*SubagentStore).SaveCompleted)

	got, err := store.ListForParent(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ListForParent: %v", err)
	}
	refs := map[string]sessionstore.SubagentStatus{}
	for _, a := range got {
		refs[a.Ref] = a.Meta.Status
	}
	if refs[mine] != sessionstore.SubagentCompleted || refs[failed] != sessionstore.SubagentFailed {
		t.Errorf("own children missing or misreported: %+v", refs)
	}
	if _, listed := refs[stranger]; listed {
		t.Error("a child of an unrelated parent session must not be listed")
	}
	if _, listed := refs[moved]; listed {
		t.Error("a child of another workspace must not be listed")
	}
}

func TestListForParentReturnsNewestFirst(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	spec := testSubagentSpec(t, "explore")
	var refs []string
	for range 3 {
		refs = append(refs, saveListedChild(t, store, spec, (*SubagentStore).SaveCompleted))
		time.Sleep(2 * time.Millisecond) // distinct UpdatedAt stamps
	}

	got, err := store.ListForParent(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ListForParent: %v", err)
	}
	if len(got) != len(refs) {
		t.Fatalf("listed %d children, want %d", len(got), len(refs))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Meta.UpdatedAt.Before(got[i].Meta.UpdatedAt) {
			t.Fatalf("listing is not newest-first: %v then %v", got[i-1].Meta.UpdatedAt, got[i].Meta.UpdatedAt)
		}
	}
}

func TestSubagentListingReportsStateAndRetrievalRoute(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	artifacts := []sessionstore.SubagentArtifact{
		{Ref: "sa_one", Meta: sessionstore.SubagentMeta{Status: sessionstore.SubagentCompleted, Name: "research", ParentToolCallID: "call_a/fleet-1", UpdatedAt: now}},
		{Ref: "sa_two", Meta: sessionstore.SubagentMeta{Status: sessionstore.SubagentInterrupted, Name: "task", ParentToolCallID: "call_a/fleet-2", UpdatedAt: now,
			Capsule: sessionstore.ContextCapsule{Inherited: sessionstore.InheritedContext{UpstreamFrom: []string{"research"}}}}},
	}

	all := formatSubagentListing(artifacts, "")
	for _, want := range []string{
		"sa_one", "status=completed", "name=research", "from=call_a/fleet-1",
		"sa_two", "status=interrupted", "started-from=upstream",
		"read_subagent_result",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("listing is missing %q:\n%s", want, all)
		}
	}
	// Only the fed child claims inherited context.
	if strings.Count(all, "started-from=upstream") != 1 {
		t.Errorf("upstream marker must name exactly the fed child:\n%s", all)
	}

	completed := formatSubagentListing(artifacts, sessionstore.SubagentCompleted)
	if strings.Contains(completed, "sa_two") {
		t.Errorf("status filter leaked a non-matching child:\n%s", completed)
	}
	if empty := formatSubagentListing(artifacts, sessionstore.SubagentRunning); !strings.Contains(empty, "no persisted sub-agents") {
		t.Errorf("an empty filter result must say so plainly: %s", empty)
	}
}

func TestSubagentListToolRejectsUnknownStatusAndRequiresSession(t *testing.T) {
	listTool := NewSubagentListTool(&TaskTool{transcripts: NewSubagentStore(testenv.TempDir(t))})
	if _, err := listTool.Execute(context.Background(), json.RawMessage(`{"status":"almost"}`)); err == nil ||
		!strings.Contains(err.Error(), "unknown status") {
		t.Errorf("err = %v, want one naming the accepted states", err)
	}
	if _, err := listTool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "persisted parent session") {
		t.Errorf("err = %v, want the missing-session refusal", err)
	}
}

// The recovery this exists for: a fleet's children stay reachable by reference
// after the aggregate that carried those references is gone.
func TestSubagentListingRecoversFleetChildrenByReference(t *testing.T) {
	root := testenv.TempDir(t)
	prov := &upstreamProbeProvider{answer: "ARTIFACT-42 lives in parse.go"}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	store := mustSubagentStore(t)
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4))
	ctx := agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	ctx = agent.WithParentSession(ctx, "listing-parent")

	if _, err := NewFleetTool(task).Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"research","prompt":"TASK-ALPHA survey the parser","read_only":true},
		{"id":"implement","prompt":"TASK-BETA rewrite the parser","depends_on":["research"],"write_paths":["parse.go"]}
	]}`)); err != nil {
		t.Fatalf("fleet: %v", err)
	}

	out, err := NewSubagentListTool(task).Execute(ctx, json.RawMessage(`{"status":"completed"}`))
	if err != nil {
		t.Fatalf("list_subagents: %v", err)
	}
	for _, want := range []string{"fleet-call/fleet-1", "fleet-call/fleet-2", "status=completed", "started-from=upstream"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing is missing %q:\n%s", want, out)
		}
	}

	// Every listed reference must actually resolve, or the route it advertises
	// is a dead end.
	for _, ref := range subagentRefsIn(strings.ReplaceAll(out, "- ", "Subagent reference: ")) {
		if _, _, err := store.ReadFinalAnswer(strings.Fields(ref)[0], "listing-parent", root); err != nil {
			t.Errorf("listed ref %q does not resolve: %v", ref, err)
		}
	}
}
