package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

// provedAgent is an agent in a workspace holding auth/login.go, with a pending
// change's proof recorded against the file's current content.
func provedAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	root := testenv.TempDir(t)
	file := filepath.Join(root, "auth", "login.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("package auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(&stepProvider{}, deliveryRegistry(), sessionstore.NewSession(""),
		Options{DeliveryProfile: true, WriteWorkspaceRoot: root}, event.Discard)
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{ScopeID: "goal-auth", PendingMutation: true, CriteriaEstablished: true,
		Proven: &evidence.ProvenMutation{
			Paths:    map[string]string{"auth/login.go": a.pathFingerprint("auth/login.go")},
			Verified: true, SignedOff: true, Inspected: true, ProjectChecks: true,
		}})
	enterScope(a, "goal-auth")
	return a, file
}

// Proof carried across a restart stands for the content it was earned on: the
// same file keeps it, and an edit made while no process watched voids it and
// sends the change back for verification.
func TestCarriedProofStandsOnlyForUnchangedContent(t *testing.T) {
	a, file := provedAgent(t)
	if a.carriedProof() == nil {
		t.Fatal("unchanged content lost its proof")
	}
	if got := a.finalReadinessCheckFor(); got.missingVerification != 0 || got.missingSignoff != 0 {
		t.Fatalf("carried proof not honoured: %+v", got)
	}
	if err := os.WriteFile(file, []byte("package auth // edited meanwhile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.carriedProof() != nil {
		t.Fatal("an edit made between processes kept the proof")
	}
	if got := a.finalReadinessCheckFor(); got.missingVerification == 0 || got.missingSignoff == 0 {
		t.Fatalf("an edited change was not sent back for proof: %+v", got)
	}
}

// A removed file is a state like any other: deleting it voids a proof earned
// on its content.
func TestCarriedProofSeesADeletion(t *testing.T) {
	a, file := provedAgent(t)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if a.carriedProof() != nil {
		t.Fatal("a deleted file kept its proof")
	}
}

// A change with a part the host cannot name carries no proof at all: that part
// could not be checked on resume.
func TestOpaqueMutationCarriesNoProof(t *testing.T) {
	a, _ := provedAgent(t)
	a.task.ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"auth/login.go"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "mcp__x__apply", Success: true, Mutation: true, MutationEvidence: evidence.MutationProven})
	a.turn.lastReadiness = &finalReadinessCheck{}
	if proven := a.provenMutation(); proven != nil {
		t.Fatalf("an opaque change was carried as proven: %+v", proven)
	}
}
