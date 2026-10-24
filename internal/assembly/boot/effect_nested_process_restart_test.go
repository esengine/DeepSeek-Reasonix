package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type nestedCrashWitness struct {
	SessionPath string `json:"sessionPath"`
	ParentID    string `json:"parentId"`
	ChildID     string `json:"childId"`
	Candidate   string `json:"candidate"`
	Generation  string `json:"generation"`
}

type nestedProcessCrashProvider struct {
	nestedRestartProvider
	crash func() error
}

func (p *nestedProcessCrashProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if _, written := callResult(req, "create-file"); written && strings.Contains(firstUser(req), "NEST-CONT-LEAF") {
		return nil, p.crash()
	}
	return p.nestedRestartProvider.Stream(ctx, req)
}

func TestNestedProcessRestartCrashHelper(t *testing.T) {
	witness := os.Getenv("REASONIX_NESTED_PROCESS_WITNESS")
	if witness == "" {
		return
	}
	home, root, kind := os.Getenv("REASONIX_NESTED_PROCESS_HOME"), os.Getenv("REASONIX_NESTED_PROCESS_ROOT"), os.Getenv("REASONIX_NESTED_PROCESS_KIND")
	p := &nestedProcessCrashProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	bindPresenceSession(t, ctrl)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	p.crash = func() error {
		catalog, err := ctrl.IsolationResults(t.Context())
		if err != nil {
			return err
		}
		record := nestedCrashWitness{SessionPath: ctrl.SessionPath()}
		for _, row := range catalog.Results {
			if row.Kind != "result" {
				continue
			}
			if row.ParentID == "" {
				record.ParentID, record.Candidate, record.Generation = row.ID, row.CandidateRoot, row.Generation
			} else {
				record.ChildID = row.ID
			}
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := os.WriteFile(witness, raw, 0600); err != nil {
			return err
		}
		os.Exit(77)
		return nil
	}
	_ = ctrl.Run(t.Context(), "retain nested work across process death")
	t.Fatal("nested worker did not reach the process crash boundary")
}

func TestEffectNestedProcessDeathRetainsAndContinuesTheOriginalParent(t *testing.T) {
	p := &nestedRestartProvider{}
	opts := nestedExecutionOptions(t, p, "", "", event.Discard)
	witness := filepath.Join(t.TempDir(), "witness.json")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNestedProcessRestartCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_NESTED_PROCESS_WITNESS="+witness, "REASONIX_NESTED_PROCESS_HOME="+opts.Home,
		"REASONIX_NESTED_PROCESS_ROOT="+opts.WorkspaceRoot, "REASONIX_NESTED_PROCESS_KIND=nested-execution-"+strings.ToLower(t.Name()))
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("real nested process did not exit at its crash boundary: %v %s", err, out)
	}
	raw, err := os.ReadFile(witness)
	if err != nil {
		t.Fatal(err)
	}
	var record nestedCrashWitness
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	session, err := sessionstore.LoadSession(record.SessionPath)
	if err != nil {
		t.Fatalf("root admission was not durable before nested execution: %v", err)
	}
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Resume(session, record.SessionPath); err != nil {
		ctrl.Close()
		t.Fatal(err)
	}
	bindPresenceSession(t, ctrl)
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Results {
		if row.Kind == "result" && (row.State != isolation.Interrupted || row.Activity != "inactive") {
			t.Fatalf("a dead process still appears to own execution: %+v", row)
		}
	}
	preview, err := ctrl.PreviewIsolatedDiscard(t.Context(), record.ChildID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.DiscardIsolated(t.Context(), control.IsolatedDiscardRequest{ID: preview.ID, Revision: preview.Revision}); err != nil {
		t.Fatal(err)
	}
	store := delegation.NewSubagentStore(filepath.Join(ctrl.SessionDir(), "subagents"))
	artifacts, err := store.ListForParent(sessionstore.BranchID(ctrl.SessionPath()), opts.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Meta.EnvironmentID == record.ParentID {
			p.resume = artifact.Ref
			retained, err := sessionstore.LoadSession(filepath.Join(ctrl.SessionDir(), "subagents", artifact.Ref+".jsonl"))
			if err != nil {
				t.Fatalf("crashed parent lost its admitted transcript: %v", err)
			}
			if !slices.ContainsFunc(retained.Snapshot(), func(m provider.Message) bool {
				return m.Role == provider.RoleUser && strings.Contains(m.Content, "NEST-CONT-PARENT")
			}) {
				t.Fatal("crashed parent lost its original task prompt")
			}
		}
	}
	if p.resume == "" {
		t.Fatal("crashed parent's transcript was not discoverable")
	}
	if err := ctrl.Run(t.Context(), "continue the retained parent after process restart"); err != nil {
		t.Fatal(err)
	}
	catalog, err = ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range catalog.Results {
		if row.ID == record.ParentID {
			found = row.CandidateRoot == record.Candidate && row.Generation != record.Generation && row.State == isolation.Pending
		}
	}
	if !found {
		t.Fatalf("process restart replaced its original candidate or reused the dead generation: %+v; result=%s", catalog, p.last)
	}
	for _, name := range []string{"parent.txt", "resumed.txt"} {
		if _, err := os.Stat(filepath.Join(record.Candidate, name)); err != nil {
			t.Fatalf("retained parent lost %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(opts.WorkspaceRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("restarted unverified task wrote its source: %v", err)
		}
	}
}
