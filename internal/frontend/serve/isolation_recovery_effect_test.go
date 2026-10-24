package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

func recoveryHarness(t *testing.T) *unreadHarness {
	t.Helper()
	h := newUnreadHarness(t, "text")
	raw, err := os.ReadFile(filepath.Join(h.root, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	seed := strings.Replace(string(raw), "[agent]", "[agent]\ncollaboration = \"shared\"", 1)
	if err := os.WriteFile(config.RootsForHome(h.home).UserConfigPath(), []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+who+"_NAME", "t")
		t.Setenv("GIT_"+who+"_EMAIL", "t@t")
	}
	isolateGitGlobalConfig(t)
	for _, dir := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(h.root, dir), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.root, dir, "file.txt"), []byte("original "+dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h.root, "init", "-q")
	gitIn(t, h.root, "add", "-A")
	gitIn(t, h.root, "commit", "-qm", "base")
	return h
}

func interruptRecoveryApply(t *testing.T, root string) {
	t.Helper()
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := worktree.TakeSnapshot(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := worktree.CreateCandidate(t.Context(), snap, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worktree.RemoveCandidate(context.Background(), snap, candidate) })
	for _, dir := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(candidate.WorktreeRoot, dir, "file.txt"), []byte("candidate "+dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tree, changes, err := worktree.CandidateTree(t.Context(), snap, candidate)
	if err != nil {
		t.Fatal(err)
	}
	previous := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = previous }()
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == filepath.Join(snap.RepoRoot, "b", "file.txt") {
			panic("injected integration interruption")
		}
	}
	crashed := false
	func() {
		defer func() { crashed = recover() != nil }()
		_ = worktree.ApplyPaths(t.Context(), snap, tree, changes)
	}()
	if !crashed {
		t.Fatal("integration did not reach partial publication")
	}
}

func recoveryPane(t *testing.T, h *unreadHarness, root string, extras ...string) (*control.Controller, *Server) {
	t.Helper()
	bc := NewBroadcaster()
	ctrl, err := boot.Build(t.Context(), boot.Options{Home: h.home, WorkspaceRoot: root, AdditionalDirs: extras, Sink: bc})
	if err != nil {
		t.Fatal(err)
	}
	server := New(ctrl, bc, config.ServeConfig{})
	keeper := control.NewSessionLeaseKeeper()
	if err := server.SetSessionLeases(keeper); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctrl.Close(); keeper.Release() })
	return ctrl, server
}

func recoveryTarget(t *testing.T, c *control.Controller) isolation.RecoveryTarget {
	t.Helper()
	catalog, err := c.IsolationResults(t.Context())
	if err != nil || !catalog.RecoveryRequired || catalog.Recovery == nil {
		t.Fatalf("no target: %+v %v", catalog, err)
	}
	return *catalog.Recovery
}

func recoverThroughHTTP(t *testing.T, server *Server, target isolation.RecoveryTarget) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(target.Request())
	r := httptest.NewRequest(http.MethodPost, "/isolated-results/recover", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	server.recoverIsolatedIntegration(w, r)
	return w
}

func TestFreshPaneCanRecoverWithoutFirstSubmittingAModelTurn(t *testing.T) {
	h := recoveryHarness(t)
	interruptRecoveryApply(t, h.root)
	ctrl, server := recoveryPane(t, h, h.root)
	if ctrl.SessionPath() != "" {
		t.Fatal("fixture concealed fresh-pane admission")
	}
	w := recoverThroughHTTP(t, server, recoveryTarget(t, ctrl))
	if w.Code != http.StatusNoContent {
		t.Fatalf("fresh host recovery: %d %s", w.Code, w.Body.String())
	}
	if ctrl.SessionPath() == "" {
		t.Fatal("recovery failed to establish session writer authority")
	}
	if body, err := os.ReadFile(filepath.Join(h.root, "a", "file.txt")); err != nil || string(body) != "original a" {
		t.Fatalf("recovery: %q %v", body, err)
	}
}

func TestRecoveryTargetCannotBorrowReplacementPaneOrSiblingDirectoryAuthority(t *testing.T) {
	h := recoveryHarness(t)
	interruptRecoveryApply(t, h.root)
	first, _ := recoveryPane(t, h, h.root)
	target := recoveryTarget(t, first)
	second, server := recoveryPane(t, h, h.root)
	w := recoverThroughHTTP(t, server, target)
	if w.Code != http.StatusConflict {
		t.Fatalf("replacement accepted stale target: %d %s", w.Code, w.Body.String())
	}
	child, childServer := recoveryPane(t, h, filepath.Join(h.root, "a"))
	w = recoverThroughHTTP(t, childServer, recoveryTarget(t, child))
	if w.Code < 400 {
		t.Fatalf("subdirectory recovered ungranted sibling: %d %s", w.Code, w.Body.String())
	}
	if body, err := os.ReadFile(filepath.Join(h.root, "a", "file.txt")); err != nil || string(body) != "candidate a" {
		t.Fatalf("refused recovery wrote source: %q %v", body, err)
	}
	if err := worktree.CheckApplyRecovery(second.WorkspaceRepo()); !errors.Is(err, worktree.ErrApplyRecovery) {
		t.Fatalf("refused recovery lost journal: %v", err)
	}
	allowed, allowedServer := recoveryPane(t, h, filepath.Join(h.root, "a"), filepath.Join(h.root, "b"))
	w = recoverThroughHTTP(t, allowedServer, recoveryTarget(t, allowed))
	if w.Code != http.StatusNoContent {
		t.Fatalf("explicit extra root was not honored: %d %s", w.Code, w.Body.String())
	}
}
