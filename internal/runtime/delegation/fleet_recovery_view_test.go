package delegation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/execjournal"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
	"testing"
)

func TestLegacyFleetPreviewRebuildsOnlyProvenArgumentsAndResults(t *testing.T) {
	task, ctx, _, _ := steerTask(t, SteerLimits{})
	path := filepath.Join(t.TempDir(), "parent.jsonl")
	parent, workspace := agent.ParentSession(ctx), task.workspaceRoot
	group := "original-fleet"
	spec := testSubagentSpec(t, "source")
	spec.ParentSession, spec.WorkspaceRoot, spec.ExecutionID = parent, workspace, fleetNodeID(group, 0)
	run := startedRun(t, task.transcripts, spec, said(provider.RoleAssistant, "original answer"))
	if err := task.transcripts.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	if err := execjournal.Open(path, execjournal.Opening{ID: spec.ExecutionID, Group: group, Kind: "fleet"}); err != nil {
		t.Fatal(err)
	}
	execjournal.Settle(path, spec.ExecutionID)
	args := `{"tasks":[{"id":"one","prompt":"research","read_only":true},{"id":"two","prompt":"continue","depends_on":["one"],"read_only":true}]}`
	call := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: group, Name: "fleet", Arguments: args}}}
	sess := sessionstore.NewSession("sys")
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	plans, err := task.transcripts.FleetRecoveryPlans(path, parent, workspace, run.Ref)
	if err != nil || len(plans) != 1 || plans[0].Ready {
		t.Fatalf("missing arguments inferred: %+v %v", plans, err)
	}
	sess.Add(call)
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	plans, err = task.transcripts.FleetRecoveryPlans(path, parent, workspace, run.Ref)
	if err != nil || len(plans) != 1 || !plans[0].Ready || !plans[0].Legacy || plans[0].Tasks[0].Ref != run.Ref || plans[0].Tasks[1].State != "unfinished" {
		t.Fatalf("legacy preview=%+v %v", plans, err)
	}
	first := plans[0].Snapshot
	next, err := task.transcripts.PrepareContinue(run.Ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	next.Session.Add(said(provider.RoleAssistant, "changed answer"))
	if err := task.transcripts.SaveCompleted(next); err != nil {
		t.Fatal(err)
	}
	next.Release()
	plans, err = task.transcripts.FleetRecoveryPlans(path, parent, workspace, run.Ref)
	if err != nil || plans[0].Snapshot == first {
		t.Fatal("changed result kept its snapshot", err)
	}
	if _, err := task.transcripts.FleetRecoveryPlans(path, "foreign", workspace, run.Ref); !errors.Is(err, ErrSubagentNotOwned) {
		t.Fatal(err)
	}
	if _, err := task.transcripts.FleetRecoveryPlans(path, parent, t.TempDir(), run.Ref); !errors.Is(err, ErrSubagentNotOwned) {
		t.Fatal(err)
	}
	sess.Add(call)
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	plans, err = task.transcripts.FleetRecoveryPlans(path, parent, workspace, run.Ref)
	if err != nil || plans[0].Ready {
		t.Fatal("ambiguous original call inferred", err)
	}
	dir := store.SessionFleetRecoveryDir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":999,"id":"fr_future"}`)
	name := filepath.Join(dir, "fr_future.json")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := task.transcripts.FleetRecoveryPlans(path, parent, workspace, run.Ref); err == nil {
		t.Fatal("unknown recipe fell back to legacy")
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != string(data) {
		t.Fatal("future recipe changed")
	}
}

func TestBudgetBindingSurvivesOlderMetadataWriterAndRefusesFutureVersion(t *testing.T) {
	s := mustSubagentStore(t)
	spec := testSubagentSpec(t, "budgeted")
	spec.BudgetScope = "root-budget"
	run := startedRun(t, s, spec, said(provider.RoleAssistant, "done"))
	if err := s.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	meta, err := s.LoadMeta(run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	meta.BudgetScope = ""
	oldData, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.metaPath(run.Ref), oldData, 0600); err != nil {
		t.Fatal(err)
	}
	meta, err = s.LoadMeta(run.Ref)
	if err != nil || meta.BudgetScope != spec.BudgetScope {
		t.Fatalf("binding lost: %+v %v", meta, err)
	}
	path := store.SessionBudgetBinding(s.sessionPath(run.Ref))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["version"] = 99
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMeta(run.Ref); err == nil {
		t.Fatal("future budget binding accepted")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Fatal("future binding changed")
	}
}
