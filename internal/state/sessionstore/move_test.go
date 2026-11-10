package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func movableSession(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "20260101-000000.000000000-src.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "edit /old/project/main.go please"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done, wrote /old/project/main.go"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, false, func(m *BranchMeta) error {
		m.CustomTitle, m.Pinned, m.Archived = "Keep me", true, true
		m.ToolApprovalMode, m.Mode, m.Goal, m.AgentPreset, m.TokenMode = "yolo", "plan", "ship it", "delivery", "full"
		m.WorkspaceRoot = "/old/project"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{store.SessionOutputsDir(path), store.SessionCheckpointDir(path), store.SessionJobsDir(path), store.SessionInboxDir(path)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestMoveSessionCarriesTheConversationAndNoAuthorization(t *testing.T) {
	from, to := testenv.TempDir(t), testenv.TempDir(t)
	src := movableSession(t, from)

	dst, err := MoveSession(src, to, "/new/project")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dst) != to {
		t.Fatalf("moved to %s, want a file in %s", dst, to)
	}
	msgs, _, intact, err := LoadSessionDisplayMessages(dst)
	if err != nil || !intact || len(msgs) != 2 || msgs[0].Role == provider.RoleSystem || msgs[0].Content != "edit /old/project/main.go please" {
		t.Fatalf("the conversation did not arrive whole: %v %v %v", msgs, intact, err)
	}
	meta, ok, err := LoadBranchMeta(dst)
	if err != nil || !ok {
		t.Fatalf("meta: %v %v", ok, err)
	}
	if meta.CustomTitle != "Keep me" || !meta.Pinned || !meta.Archived {
		t.Fatalf("title, pin and archive state belong to the person and travel: %+v", meta)
	}
	if meta.ToolApprovalMode != "" || meta.Mode != "" || meta.Goal != "" || meta.AgentPreset != "" || meta.TokenMode != "" {
		t.Fatalf("what the old project allowed must not follow the conversation: %+v", meta)
	}
	if meta.WorkspaceRoot != "/new/project" {
		t.Fatalf("workspace root = %q, want the target's", meta.WorkspaceRoot)
	}
	if meta.Adopted == nil || meta.Adopted.Kind != "move" || meta.Adopted.From != src || meta.Adopted.FromRoot != "/old/project" {
		t.Fatalf("the move is not recorded where a move back can read it: %+v", meta.Adopted)
	}
}

func TestMoveSessionLeavesNothingBehindAtTheSource(t *testing.T) {
	from, to := testenv.TempDir(t), testenv.TempDir(t)
	src := movableSession(t, from)
	if _, err := MoveSession(src, to, "/new/project"); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(from, "*"))
	for _, p := range left {
		if name := filepath.Base(p); name != ".list-index.json" && !strings.HasSuffix(name, ".meta.lock") {
			t.Errorf("%s was left behind", p)
		}
	}
}

func TestMoveSessionRemovesCheckpointsJobsAndInboxAndMovesNone(t *testing.T) {
	from, to := testenv.TempDir(t), testenv.TempDir(t)
	src := movableSession(t, from)
	dirs := []string{store.SessionCheckpointDir(src), store.SessionJobsDir(src), store.SessionInboxDir(src)}
	dst, err := MoveSession(src, to, "/new/project")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s survived the move (stat err %v)", d, err)
		}
	}
	for _, d := range []string{store.SessionCheckpointDir(dst), store.SessionJobsDir(dst), store.SessionInboxDir(dst)} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s arrived at the target: checkpoints snapshot the old project's files and stay behind (stat err %v)", d, err)
		}
	}
}

func TestMoveSessionRefusesASessionSomeoneHolds(t *testing.T) {
	from, to := testenv.TempDir(t), testenv.TempDir(t)
	src := movableSession(t, from)
	lease, err := TryAcquireSessionLease(src)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	_, err = MoveSession(src, to, "/new/project")
	var busy *SessionLeaseError
	if !errors.Is(err, ErrSessionBusy) && !errors.As(err, &busy) {
		t.Fatalf("a held session must be refused as busy, got %v", err)
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("a refused move touched the source: %v", statErr)
	}
	if left, _ := filepath.Glob(filepath.Join(to, "*.jsonl")); len(left) != 0 {
		t.Fatalf("a refused move left a copy: %v", left)
	}
}

func TestMoveSessionRefusesTheSameFolderAndAMissingSource(t *testing.T) {
	dir := testenv.TempDir(t)
	src := movableSession(t, dir)
	if _, err := MoveSession(src, dir, "/old/project"); !errors.Is(err, ErrSameFolder) {
		t.Fatalf("moving into its own folder is %v, want ErrSameFolder", err)
	}
	if _, err := MoveSession(filepath.Join(dir, "nope.jsonl"), testenv.TempDir(t), "/x"); err == nil {
		t.Fatal("a missing source cannot be moved")
	}
}
