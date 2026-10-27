package isolation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"reasonix/internal/contract/tool"
)

func publishSettlementFence(t *testing.T, s *Store, id string, raw []byte) string {
	t.Helper()
	dir := filepath.Join(s.managedRoot, ".settlements-v1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, strings.TrimSuffix(resultLockName(id), ".lock")+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireSettlementRefusal(t *testing.T, err error) {
	t.Helper()
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeUnavailable || !strings.Contains(err.Error(), "settlement") {
		t.Errorf("operation did not refuse the durable settlement fence: %v", err)
	}
}

func TestSettlementFenceStopsEveryDestructiveResultSibling(t *testing.T) {
	for _, operation := range []string{"apply", "discard", "paths", "empty_paths", "finish", "empty_finish", "abort", "sweep"} {
		t.Run(operation, func(t *testing.T) {
			s, e := finishedOperationResult(t)
			if strings.HasPrefix(operation, "empty_") {
				write(t, e.Cand.WorkspaceRoot, "a.txt", "one\n")
			}
			raw := []byte(`{"version":999,"phase":"future-recovery","unknown":{"keep":true}}`)
			fence := publishSettlementFence(t, s, e.ID, raw)
			peer := NewStore(s.managedRoot, s.workspace, s.repo)
			var err error
			switch operation {
			case "apply":
				_, err = NewApplyTool(peer).Execute(t.Context(), args(e.ID))
			case "discard":
				_, err = NewDiscardTool(peer).Execute(t.Context(), args(e.ID))
			case "paths", "empty_paths":
				_, err = NewApplyTool(peer).DeclaredWritePaths(t.Context(), args(e.ID))
			case "finish", "empty_finish":
				_, err = peer.Finish(t.Context(), e)
			case "abort":
				err = peer.Abort(t.Context(), e)
			case "sweep":
				peer.SweepExpired(t.Context(), -time.Second)
			}
			if operation != "sweep" {
				requireSettlementRefusal(t, err)
			}
			// A refused operation must release its newly acquired OS owner.
			if release, err := peer.acquireResultOperation(t.Context(), e.ID, true); release != nil {
				release()
				t.Error("fenced result was admitted on a second attempt")
			} else {
				requireSettlementRefusal(t, err)
			}
			if got := read(t, s.workspace, "a.txt"); got != "one\n" {
				t.Errorf("fenced operation wrote source: %q", got)
			}
			for _, path := range []string{e.Cand.WorktreeRoot, entryPath(e.Cand)} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("fenced operation removed %s: %v", filepath.Base(path), err)
				}
			}
			if actual, err := os.ReadFile(fence); err != nil || string(actual) != string(raw) {
				t.Errorf("unsupported settlement record was changed: %q %v", actual, err)
			}
			if !slices.Contains(NewStore(s.managedRoot, s.workspace, s.repo).Pending(), e.ID) {
				t.Error("fenced legacy publication disappeared across Store restart")
			}
		})
	}
}

func TestSettlementFenceIsRecheckedAfterWaitingForOperationOwnership(t *testing.T) {
	s, e := finishedOperationResult(t)
	release, err := s.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	unblock := sync.OnceFunc(release)
	defer unblock()
	synctest.Test(t, func(t *testing.T) {
		peer := NewStore(s.managedRoot, s.workspace, s.repo)
		done := make(chan error, 1)
		go func() {
			_, err := peer.Apply(t.Context(), e.ID)
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("queued operation passed an active owner: %v", err)
		default:
		}
		publishSettlementFence(t, s, e.ID, []byte(`{"version":1}`))
		unblock()
		requireSettlementRefusal(t, <-done)
	})
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("operation used its pre-wait view: %q", got)
	}
}

func TestSettlementFenceDoesNotBlockAnUnrelatedResultOrReadItsBytes(t *testing.T) {
	s, e := finishedOperationResult(t)
	report, err := s.Execute(t.Context(), writes(map[string]string{"b.txt": "other candidate\n"}), s.workspace, "change b", "")
	if err != nil {
		t.Fatal(err)
	}
	other := idOf(t, report)
	fence := publishSettlementFence(t, s, e.ID, []byte("not JSON; never interpreted by this writer"))
	peer := NewStore(s.managedRoot, s.workspace, s.repo)
	if _, err := peer.Apply(t.Context(), other); err != nil {
		t.Fatalf("unrelated result could not progress: %v", err)
	}
	if got := read(t, s.workspace, "b.txt"); got != "other candidate\n" {
		t.Fatal(got)
	}
	if _, err := peer.Apply(t.Context(), e.ID); err == nil {
		t.Error("malformed fence was ignored")
	} else {
		requireSettlementRefusal(t, err)
	}
	if got, err := os.ReadFile(fence); err != nil || string(got) != "not JSON; never interpreted by this writer" {
		t.Fatalf("fence bytes changed: %q %v", got, err)
	}
}

func TestSettlementFenceStillBlocksWhenTheCandidateHasGone(t *testing.T) {
	s, e := finishedOperationResult(t)
	path := publishSettlementFence(t, s, e.ID, []byte(`{"version":1,"phase":"cleanup-pending"}`))
	if err := os.RemoveAll(filepath.Dir(e.Cand.WorktreeRoot)); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(s.managedRoot, s.workspace, s.repo)
	_, err := peer.Apply(t.Context(), e.ID)
	requireSettlementRefusal(t, err)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("candidate removal lost external fence: %v", err)
	}
}

func TestSettlementFenceRejectsAChangedNamespaceAndPreservesItsOwner(t *testing.T) {
	s, e := finishedOperationResult(t)
	path := filepath.Join(s.managedRoot, ".settlements-v1")
	if err := os.WriteFile(path, []byte("unexpected namespace"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Apply(t.Context(), e.ID)
	requireSettlementRefusal(t, err)
	if got, err := os.ReadFile(path); err != nil || string(got) != "unexpected namespace" {
		t.Fatalf("invalid namespace was replaced: %q %v", got, err)
	}
}

func TestSettlementFenceRejectsAnEntryOfAnyKind(t *testing.T) {
	s, e := finishedOperationResult(t)
	path := publishSettlementFence(t, s, e.ID, nil)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := s.Apply(t.Context(), e.ID)
	requireSettlementRefusal(t, err)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("unknown fence entry was changed: %v %v", info, err)
	}
}
