package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

type recoveryLineage struct {
	root, dir, lead string
	covered         string
	diverged        string
}

func sessionOf(msgs ...string) *sessionstore.Session {
	s := sessionstore.NewSession("sys")
	for i, m := range msgs {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		s.Add(provider.Message{Role: role, Content: m})
	}
	return s
}

// seedRecoveryLineage builds a lead with two recovery copies its content has
// grown past (hidden in the sidebar) and one copy holding turns it lacks.
func seedRecoveryLineage(t *testing.T) recoveryLineage {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	l := recoveryLineage{root: testenv.TempDir(t)}
	l.dir = SessionDirFor(l.root)
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l.lead = filepath.Join(l.dir, "20260803-140947-lead.jsonl")
	if err := sessionOf("first", "one", "disk").Save(l.lead); err != nil {
		t.Fatal(err)
	}
	fork := func(from string, s *sessionstore.Session) string {
		info, err := s.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: from})
		if err != nil {
			t.Fatal(err)
		}
		return info.Path
	}
	l.covered = fork(l.lead, sessionOf("first", "one", "local a"))
	l.diverged = fork(l.covered, sessionOf("first", "one", "only here"))
	grown, err := sessionstore.LoadSession(l.lead)
	if err != nil {
		t.Fatal(err)
	}
	grown.Replace(sessionOf("first", "one", "local a", "answered").Snapshot())
	if err := grown.SaveRewrite(l.lead); err != nil {
		t.Fatal(err)
	}
	if !sessionstore.RecoveryBranchCoveredByParent(l.covered, l.dir) {
		t.Fatal("seed: the covered copy should be covered by the lead")
	}
	if sessionstore.RecoveryBranchCoveredByParent(l.diverged, l.dir) {
		t.Fatal("seed: the diverged copy must not be covered")
	}
	return l
}

func removeLead(t *testing.T, h *Hub, path string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	return postRemoveSession(t, srv, path)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestRemoveRemovesCoveredRecoveryCopiesAndKeepsADivergedOne(t *testing.T) {
	l := seedRecoveryLineage(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, l.root)
	resp := removeLead(t, h, l.lead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", resp.StatusCode)
	}
	for name, p := range map[string]string{"lead": l.lead, "covered copy": l.covered} {
		if exists(p) {
			t.Errorf("%s survived the delete", name)
		}
	}
	if !exists(l.diverged) {
		t.Error("a copy holding turns its parent lacks was deleted with the lead")
	}
}

func TestRemoveKeepsACoveredSiblingAPaneHasOpen(t *testing.T) {
	l := seedRecoveryLineage(t)
	h := NewHub(HubOptions{})
	rt := hubRuntime(t, h, l.root)
	rt.Server.Controller().SetSessionPath(l.covered)
	resp := removeLead(t, h, l.lead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", resp.StatusCode)
	}
	if !exists(l.covered) {
		t.Error("open sibling was deleted with its lead")
	}
	if exists(l.lead) {
		t.Error("the lead survived")
	}
}

func TestRemoveRemovesTheLeadsVersions(t *testing.T) {
	l := seedRecoveryLineage(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, l.root)
	version := filepath.Join(l.dir, "20260803-150000-version.jsonl")
	writeSessionAt(t, version)
	if err := sessionstore.SaveBranchMeta(version, sessionstore.BranchMeta{
		Superseded: true, ParentID: sessionstore.BranchID(l.lead),
	}); err != nil {
		t.Fatal(err)
	}
	if got := sessionstore.SessionVersionPaths(l.lead); len(got) != 1 {
		t.Fatalf("seeded versions = %v, want the one cut from the lead", got)
	}
	resp := removeLead(t, h, l.lead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", resp.StatusCode)
	}
	if exists(version) {
		t.Error("the lead's version outlived its row")
	}
}

func TestRemoveRefusesWhenACoveredSiblingIsHeld(t *testing.T) {
	l := seedRecoveryLineage(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, l.root)
	lease, err := sessionstore.TryAcquireSessionLease(l.covered)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	resp := removeLead(t, h, l.lead)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove = %d, want 409 while a sibling is held", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "session.in_use" {
		t.Errorf("refusal code = %q, want session.in_use", body.Code)
	}
	for name, p := range map[string]string{"held copy": l.covered, "lead": l.lead, "diverged copy": l.diverged} {
		if !exists(p) {
			t.Errorf("%s erased despite the refusal", name)
		}
	}
}
