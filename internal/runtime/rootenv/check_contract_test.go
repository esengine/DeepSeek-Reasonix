package rootenv

import (
	"path/filepath"
	"testing"

	"reasonix/internal/state/instruction"
	"reasonix/internal/state/sessionstore"
)

func TestVerificationContractSurvivesSceneEditsRestartAndRecordCopies(t *testing.T) {
	s, _ := fixture(t)
	checks := []instruction.VerifyCheck{{Command: "original check", SourcePath: "REASONIX.md", Line: 3}}
	s.WithVerificationChecks(checks)
	checks[0].Command = "changed caller"
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	write(t, filepath.Join(r.Candidate.WorkspaceRoot, "REASONIX.md"), "## Reasonix host checks\n\n- verify: true\n")
	r.CheckContract.Checks[0].Command = "changed record"
	e.Close()
	e, err = s.Open(t.Context(), r.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if got := e.Record(); got.Version != 2 || got.CheckContract.Checks[0].Command != "original check" {
		t.Fatalf("contract changed: %+v", got.CheckContract)
	}
	before := e.Record()
	after := before
	after.CheckContract = &CheckContract{Version: 1, Checks: []instruction.VerifyCheck{{Command: "weaker"}}}
	if err := s.save(after); err == nil {
		t.Fatal("execution replaced captured contract")
	}
}
