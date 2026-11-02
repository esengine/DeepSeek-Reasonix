//go:build darwin || linux

package boot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
	sessionstore "reasonix/internal/state/store"
	"reasonix/internal/state/workspacelease"
)

func TestEffectContendedApprovalCannotProveMutationScope(t *testing.T) {
	if !sandbox.Available() || !sandbox.EgressSupported() {
		t.Skip("egress confinement not available on this host")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	for _, tc := range []struct {
		name       string
		foreign    bool
		write      bool
		unknown    bool
		optimistic bool
	}{
		{name: "foreign write", foreign: true, write: true},
		{name: "foreign claim without net change", foreign: true},
		{name: "unknown claim history", foreign: true, unknown: true},
		{name: "optimistic retained path", foreign: true, write: true, optimistic: true},
		{name: "uncontended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, build := revisionLeaseWorkspace(t)
			if tc.optimistic {
				if err := os.WriteFile(config.UserConfigPath(), []byte("[agent]\nwrite_lease = \"optimistic\"\nsubagent_claims = \"strict\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[sandbox]
bash = "enforce"
allowed_domains = ["example.invalid"]
[[providers]]
name = "test-model"
kind = "lease-revision-`+filepath.Base(root)+`"
model = "x"
`)
			commandText := `curl --noproxy '' -sS -o /dev/null http://127.0.0.1:9/; :`
			first := &postureProvider{calls: []provider.ToolCall{
				bashCall("observed", commandText),
				bashCall("fresh", "fixture=fresh; :"),
			}}
			if tc.optimistic {
				first.calls = append([]provider.ToolCall{writeCall("retained", "foreign.txt")}, first.calls...)
			}
			foreign := bashCall("foreign", "fixture=claim; :")
			if tc.write {
				foreign = writeCall("foreign", "foreign.txt")
			}
			second := &postureProvider{calls: []provider.ToolCall{foreign}}
			approvals := make(chan event.Approval, 4)
			a := build(first, event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					approvals <- e.Approval
				}
			}))
			a.EnableInteractiveApproval()
			b := build(second, event.Discard)
			sessionPath := filepath.Join(testenv.TempDir(t), "observation.jsonl")
			a.SetSessionPath(sessionPath)
			b.SetSessionPath(filepath.Join(testenv.TempDir(t), "foreign.jsonl"))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- a.Run(ctx, "observe the command") }()
			defer func() { cancel(); <-done }()
			var approval event.Approval
			select {
			case approval = <-approvals:
			case <-ctx.Done():
				t.Fatal("the command never reached its egress approval")
			}
			if approval.Tool != control.NetworkEgressApprovalTool {
				t.Fatalf("approval = %+v, want network egress", approval)
			}
			if a.WorkspaceLeaseState().Acquired {
				t.Fatal("the command retained its claim during approval")
			}
			if tc.unknown {
				canonical, err := workspacelease.CanonicalWorkspace(root)
				if err != nil {
					t.Fatal(err)
				}
				log := filepath.Join(config.WorkspaceLeaseDir(), fmt.Sprintf("%x.lock.log", sha256.Sum256([]byte(canonical))))
				if err := os.WriteFile(log, []byte("{broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.foreign {
				if err := b.Run(ctx, "claim while the command waits"); err != nil {
					t.Fatal(err)
				}
			}
			a.Approve(approval.ID, false, false, false)
			select {
			case err := <-done:
				done <- err
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("the resumed command never finished")
			}
			receipts := a.Executor().EvidenceSummary().Receipts
			var command, fresh *evidence.Receipt
			for i := range receipts {
				switch receipts[i].Command {
				case commandText:
					command = &receipts[i]
				case "fixture=fresh; :":
					fresh = &receipts[i]
				}
			}
			if command == nil || !command.Success {
				t.Fatalf("the observed command did not succeed: %+v", command)
			}
			if tc.foreign {
				if !command.Mutation || command.MutationEvidence != evidence.MutationUnknown || command.PathsComplete || len(command.Paths) != 0 {
					t.Fatalf("cross-owner observation proved the command's effect: %+v", command)
				}
			} else if command.Mutation {
				t.Fatalf("an uncontended unchanged command stayed a mutation: %+v", command)
			}
			if fresh == nil || !fresh.Success || fresh.Mutation {
				t.Fatalf("contention tainted a later observation: %+v", fresh)
			}
			observed := evidence.NewLedger()
			for _, receipt := range receipts {
				observed.Record(receipt)
			}
			hasDebt := false
			for _, obligation := range observed.Obligations(evidence.CheckContract{}) {
				hasDebt = hasDebt || obligation.Kind == evidence.ObligationUnprovenMutation
			}
			if hasDebt != tc.foreign {
				t.Fatalf("unproven-mutation obligation = %v, want %v", hasDebt, tc.foreign)
			}
			for _, metas := range [][]checkpoint.Meta{
				a.Checkpoints(),
				checkpoint.New(sessionstore.SessionCheckpointDir(sessionPath), root).List(),
			} {
				found := false
				for _, meta := range metas {
					for _, gap := range meta.CoverageGaps {
						found = found || gap.Reason == checkpoint.GapWorkspaceClaimYielded
					}
				}
				if found != tc.foreign {
					t.Fatalf("persisted contention coverage = %v, want %v: %+v", found, tc.foreign, metas)
				}
			}
		})
	}
}
