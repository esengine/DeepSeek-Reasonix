package agent

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestForkHeadStartsANewHeadAndKeepsTheOldChain(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1", "q2", "a2")
	msgs := s.Snapshot()
	forkAt := msgs[2].ID // a1
	head, err := s.ForkHead(path, forkAt, HeadKindFork, "alt")
	if err != nil || head == "" || head == SessionMainHead {
		t.Fatalf("ForkHead = %q err=%v", head, err)
	}
	if got := strings.Join(dagContents(s.Snapshot()), ","); got != "sys,q1,a1" {
		t.Fatalf("in-memory transcript after fork = %s", got)
	}
	if ref, ok := s.Head(); !ok || ref.HeadID != head || ref.LeafID != forkAt {
		t.Fatalf("head ref after fork = %+v ok=%v", ref, ok)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("no-op save after fork: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2-alt"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	if got := strings.Join(dagChain(st, SessionMainHead), ","); got != "sys,q1,a1,q2,a2" {
		t.Fatalf("main chain after fork = %s", got)
	}
	if got := strings.Join(dagChain(st, head), ","); got != "sys,q1,a1,q2-alt" {
		t.Fatalf("fork chain = %s", got)
	}
	heads, err := ListSessionHeads(path)
	if err != nil || len(heads) != 2 || heads[1].ID != head || heads[1].Kind != HeadKindFork || heads[1].Name != "alt" || heads[1].ForkFrom != forkAt || !heads[1].Selected {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref, _ := reloaded.Head(); ref.HeadID != head {
		t.Fatalf("reload must land on the selected fork, got %+v", ref)
	}
	idx, err := ReadSessionHeadIndex(path)
	if err != nil || idx == nil || !idx.Current(path) || idx.SelectedHead != head || len(idx.Heads) != 2 {
		t.Fatalf("head index after fork = %+v err=%v", idx, err)
	}
	meta, _, _ := LoadBranchMeta(path)
	if meta.HeadID != head || meta.HeadCount != 2 {
		t.Fatalf("meta mirror after fork = head %q count %d", meta.HeadID, meta.HeadCount)
	}
}

func TestSwitchHeadMovesTheSessionBackAndForth(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	fork, err := s.ForkHead(path, s.Snapshot()[1].ID, HeadKindRewind, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a1-rewound"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := s.SwitchHead(path, SessionMainHead); err != nil {
		t.Fatalf("SwitchHead main: %v", err)
	}
	if got := strings.Join(dagContents(s.Snapshot()), ","); got != "sys,q1,a1" {
		t.Fatalf("transcript on main = %s", got)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2-main"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	st := dagReplay(t, path)
	if got := strings.Join(dagChain(st, SessionMainHead), ","); got != "sys,q1,a1,q2-main" {
		t.Fatalf("main chain = %s", got)
	}
	if got := strings.Join(dagChain(st, fork), ","); got != "sys,q1,a1-rewound" {
		t.Fatalf("rewind chain = %s", got)
	}
	if st.selectedHead() != SessionMainHead {
		t.Fatalf("selected = %q, want main after switch", st.selectedHead())
	}
	if err := s.SwitchHead(path, "nope"); !errors.Is(err, ErrSessionHeadUnknown) {
		t.Fatalf("unknown head err = %v", err)
	}
	if err := s.SwitchHead(path, SessionMainHead); err != nil {
		t.Fatalf("switching to the current head must be a no-op: %v", err)
	}
}

func TestHeadMarkersOnDiskSelectRetireRename(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	fork, err := s.ForkHead(path, s.Snapshot()[1].ID, HeadKindFork, "side")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameSessionHead(path, fork, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := RetireSessionHead(path, fork); err == nil {
		t.Fatal("retiring the selected head must be refused")
	}
	if err := SelectSessionHead(path, SessionMainHead); err != nil {
		t.Fatal(err)
	}
	if err := RetireSessionHead(path, fork); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if err := SelectSessionHead(path, fork); err == nil {
		t.Fatal("a retired head must not become the selection")
	}
	heads, err := ListSessionHeads(path)
	if err != nil || len(heads) != 2 {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
	if !heads[1].Retired || heads[1].Name != "renamed" || heads[1].Selected || !heads[0].Selected {
		t.Fatalf("head rows = %+v", heads)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref, _ := reloaded.Head(); ref.HeadID != SessionMainHead {
		t.Fatalf("reload after select = %+v", ref)
	}
	if err := RetireSessionHead(path, "missing"); !errors.Is(err, ErrSessionHeadUnknown) {
		t.Fatalf("unknown head err = %v", err)
	}
	meta, _, _ := LoadBranchMeta(path)
	if meta.HeadID != SessionMainHead || meta.HeadCount != 2 {
		t.Fatalf("meta mirror = head %q count %d", meta.HeadID, meta.HeadCount)
	}
}

func TestHeadOperationsRefuseSchemaOneSessions(t *testing.T) {
	useSchemaOneLog(t)
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1")
	if _, err := s.ForkHead(path, "", HeadKindFork, ""); !errors.Is(err, ErrSessionNotDAG) {
		t.Fatalf("ForkHead on schema 1 err = %v", err)
	}
	if err := SelectSessionHead(path, SessionMainHead); !errors.Is(err, ErrSessionNotDAG) {
		t.Fatalf("SelectSessionHead on schema 1 err = %v", err)
	}
}

func TestHeadListMarksCoveredHeads(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	fork, err := s.ForkHead(path, s.Snapshot()[2].ID, HeadKindFork, "")
	if err != nil {
		t.Fatal(err)
	}
	covered := func() map[string]bool {
		t.Helper()
		heads, err := ListSessionHeads(path)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, h := range heads {
			out[h.ID] = h.Covered
		}
		return out
	}
	if got := covered(); !got[SessionMainHead] || got[fork] {
		t.Fatalf("tip fork selected: covered = %v, want the parent covered and the selection never flagged", got)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2-alt"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := covered(); !got[SessionMainHead] || got[fork] {
		t.Fatalf("after the fork grew: covered = %v", got)
	}
	if err := SelectSessionHead(path, SessionMainHead); err != nil {
		t.Fatal(err)
	}
	if got := covered(); got[SessionMainHead] || got[fork] {
		t.Fatalf("main selected: covered = %v, want the diverged fork kept", got)
	}
}
